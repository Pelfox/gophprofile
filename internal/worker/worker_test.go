package worker

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"slices"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/pelfox/gophprofile/internal/storage"
	"github.com/pelfox/gophprofile/pkg"
	amqp "github.com/rabbitmq/amqp091-go"
	"github.com/rs/zerolog"
)

type fakeStorage struct {
	retrieved map[string][]byte

	retrievedKeys []string
	stored        []storage.StoreInput
	deleted       []string

	storeErr    error
	retrieveErr error
	deleteErr   error
}

func (s *fakeStorage) Store(
	ctx context.Context,
	input storage.StoreInput,
) (string, error) {
	s.stored = append(s.stored, storage.StoreInput{
		Key:         input.Key,
		Body:        append([]byte(nil), input.Body...),
		ContentType: input.ContentType,
		UserID:      input.UserID,
		AvatarID:    input.AvatarID,
	})
	if s.storeErr != nil {
		return "", s.storeErr
	}

	return "https://cdn.example.test/" + input.Key, nil
}

func (s *fakeStorage) Retrieve(ctx context.Context, key string) ([]byte, error) {
	s.retrievedKeys = append(s.retrievedKeys, key)
	if s.retrieveErr != nil {
		return nil, s.retrieveErr
	}

	payload, ok := s.retrieved[key]
	if !ok {
		return nil, errors.New("object not found")
	}

	return append([]byte(nil), payload...), nil
}

func (s *fakeStorage) Delete(ctx context.Context, keys []string) error {
	s.deleted = append(s.deleted, keys...)
	if s.deleteErr != nil {
		return s.deleteErr
	}

	return nil
}

type fakeQueue struct {
	resizeJobs     []pkg.MessageResizeRequest
	deleteJobs     []pkg.MessageDeleteRequest
	resizeDoneJobs []pkg.MessageResizeDone

	resizeErr     error
	deleteErr     error
	resizeDoneErr error
}

type blockingDeleteStorage struct {
	fakeStorage

	started  chan struct{}
	release  chan struct{}
	canceled chan struct{}
	calls    atomic.Int32
}

func (s *blockingDeleteStorage) Delete(ctx context.Context, _ []string) error {
	if s.calls.Add(1) == 1 {
		close(s.started)
	}

	select {
	case <-s.release:
		return nil
	case <-ctx.Done():
		select {
		case s.canceled <- struct{}{}:
		default:
		}
		return ctx.Err()
	}
}

type nackRecord struct {
	tag     uint64
	requeue bool
}

type recordingAcknowledger struct {
	acked  chan uint64
	nacked chan nackRecord
}

func newRecordingAcknowledger() *recordingAcknowledger {
	return &recordingAcknowledger{
		acked:  make(chan uint64, 2),
		nacked: make(chan nackRecord, 2),
	}
}

func (a *recordingAcknowledger) Ack(tag uint64, _ bool) error {
	a.acked <- tag
	return nil
}

func (a *recordingAcknowledger) Nack(
	tag uint64,
	_ bool,
	requeue bool,
) error {
	a.nacked <- nackRecord{tag: tag, requeue: requeue}
	return nil
}

func (a *recordingAcknowledger) Reject(tag uint64, requeue bool) error {
	a.nacked <- nackRecord{tag: tag, requeue: requeue}
	return nil
}

func (q *fakeQueue) RequestResize(
	ctx context.Context,
	message pkg.MessageResizeRequest,
) error {
	q.resizeJobs = append(q.resizeJobs, message)
	return q.resizeErr
}

func (q *fakeQueue) RequestDelete(
	ctx context.Context,
	message pkg.MessageDeleteRequest,
) error {
	q.deleteJobs = append(q.deleteJobs, message)
	return q.deleteErr
}

func (q *fakeQueue) CompleteResize(
	ctx context.Context,
	message pkg.MessageResizeDone,
) error {
	q.resizeDoneJobs = append(q.resizeDoneJobs, message)
	return q.resizeDoneErr
}

func (q *fakeQueue) Close() error {
	return nil
}

func TestConsumeDeliveriesFinishesCurrentJobOnShutdown(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	processingCtx, stopProcessing := newGracefulProcessingContext(
		ctx,
		zerolog.Nop(),
		time.Second,
	)
	defer stopProcessing()

	storageProvider := &blockingDeleteStorage{
		started:  make(chan struct{}),
		release:  make(chan struct{}),
		canceled: make(chan struct{}, 1),
	}
	processor := &processor{
		logger:  zerolog.Nop(),
		queue:   &fakeQueue{},
		storage: storageProvider,
	}
	acknowledger := newRecordingAcknowledger()
	resizeDeliveries := make(chan amqp.Delivery)
	deleteDeliveries := make(chan amqp.Delivery, 2)
	deleteDeliveries <- testDeleteDelivery(t, acknowledger, 1)

	result := make(chan error, 1)
	go func() {
		result <- consumeDeliveries(
			ctx,
			processingCtx,
			zerolog.Nop(),
			resizeDeliveries,
			deleteDeliveries,
			processor,
		)
	}()

	waitForSignal(t, storageProvider.started, "current job to start")
	cancel()
	deleteDeliveries <- testDeleteDelivery(t, acknowledger, 2)

	select {
	case <-storageProvider.canceled:
		t.Fatal("current job context was canceled before the drain timeout")
	case <-time.After(25 * time.Millisecond):
	}

	close(storageProvider.release)
	select {
	case tag := <-acknowledger.acked:
		if tag != 1 {
			t.Fatalf("expected delivery 1 to be acknowledged, got %d", tag)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for current job acknowledgement")
	}

	if err := waitForResult(t, result); err != nil {
		t.Fatalf("consumeDeliveries returned error: %v", err)
	}
	if got := storageProvider.calls.Load(); got != 1 {
		t.Fatalf("expected one processed job, got %d", got)
	}
	if got := len(deleteDeliveries); got != 1 {
		t.Fatalf("expected the next job to remain unprocessed, got queue length %d", got)
	}
}

func TestConsumeDeliveriesRequeuesCurrentJobAfterDrainTimeout(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	processingCtx, stopProcessing := newGracefulProcessingContext(
		ctx,
		zerolog.Nop(),
		10*time.Millisecond,
	)
	defer stopProcessing()

	storageProvider := &blockingDeleteStorage{
		started:  make(chan struct{}),
		release:  make(chan struct{}),
		canceled: make(chan struct{}, 1),
	}
	processor := &processor{
		logger:  zerolog.Nop(),
		queue:   &fakeQueue{},
		storage: storageProvider,
	}
	acknowledger := newRecordingAcknowledger()
	deleteDeliveries := make(chan amqp.Delivery, 1)
	deleteDeliveries <- testDeleteDelivery(t, acknowledger, 1)

	result := make(chan error, 1)
	go func() {
		result <- consumeDeliveries(
			ctx,
			processingCtx,
			zerolog.Nop(),
			make(chan amqp.Delivery),
			deleteDeliveries,
			processor,
		)
	}()

	waitForSignal(t, storageProvider.started, "current job to start")
	cancel()
	waitForSignal(t, storageProvider.canceled, "current job context cancellation")

	select {
	case record := <-acknowledger.nacked:
		if record.tag != 1 || !record.requeue {
			t.Fatalf("expected delivery 1 to be requeued, got %#v", record)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for current job requeue")
	}

	if err := waitForResult(t, result); err != nil {
		t.Fatalf("consumeDeliveries returned error: %v", err)
	}
}

func TestConsumeDeliveriesUpdatesHeartbeatWhileIdle(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	heartbeat := make(chan time.Time, 1)
	updated := make(chan struct{}, 1)
	result := make(chan error, 1)

	go func() {
		result <- consumeDeliveriesWithHeartbeat(
			ctx,
			ctx,
			zerolog.Nop(),
			make(chan amqp.Delivery),
			make(chan amqp.Delivery),
			&processor{},
			heartbeat,
			func() error {
				updated <- struct{}{}
				return nil
			},
		)
	}()

	heartbeat <- time.Now()
	waitForSignal(t, updated, "worker heartbeat update")
	cancel()

	if err := waitForResult(t, result); err != nil {
		t.Fatalf("consumeDeliveriesWithHeartbeat returned error: %v", err)
	}
}

func TestConsumeDeliveriesDoesNotUpdateHeartbeatDuringJob(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	storageProvider := &blockingDeleteStorage{
		started:  make(chan struct{}),
		release:  make(chan struct{}),
		canceled: make(chan struct{}, 1),
	}
	released := false
	defer func() {
		if !released {
			close(storageProvider.release)
		}
	}()

	heartbeat := make(chan time.Time, 1)
	updated := make(chan struct{}, 1)
	deleteDeliveries := make(chan amqp.Delivery, 1)
	deleteDeliveries <- testDeleteDelivery(t, newRecordingAcknowledger(), 1)
	result := make(chan error, 1)

	go func() {
		result <- consumeDeliveriesWithHeartbeat(
			ctx,
			context.Background(),
			zerolog.Nop(),
			make(chan amqp.Delivery),
			deleteDeliveries,
			&processor{
				logger:  zerolog.Nop(),
				queue:   &fakeQueue{},
				storage: storageProvider,
			},
			heartbeat,
			func() error {
				updated <- struct{}{}
				return nil
			},
		)
	}()

	waitForSignal(t, storageProvider.started, "current job to start")
	heartbeat <- time.Now()
	select {
	case <-updated:
		t.Fatal("heartbeat was updated while the current job was blocked")
	case <-time.After(25 * time.Millisecond):
	}

	cancel()
	close(storageProvider.release)
	released = true
	if err := waitForResult(t, result); err != nil {
		t.Fatalf("consumeDeliveriesWithHeartbeat returned error: %v", err)
	}
}

func TestProcessorProcessResizeCreatesThumbnailsAndPublishesCompletion(
	t *testing.T,
) {
	ctx := context.Background()
	request := resizeRequest()

	storageProvider := &fakeStorage{
		retrieved: map[string][]byte{
			request.FileName: pngPayload(t, 640, 480),
		},
	}
	queueProvider := &fakeQueue{}
	processor := newTestProcessor(queueProvider, storageProvider)

	if err := processor.processResize(ctx, jsonPayload(t, request)); err != nil {
		t.Fatalf("processResize returned error: %v", err)
	}

	if !slices.Equal(storageProvider.retrievedKeys, []string{request.FileName}) {
		t.Fatalf(
			"expected retrieved keys %v, got %v",
			[]string{request.FileName},
			storageProvider.retrievedKeys,
		)
	}

	expectedThumbnails := []struct {
		key  string
		size int
	}{
		{key: "avatars/source/100x100.jpg", size: 100},
		{key: "avatars/source/300x300.jpg", size: 300},
	}
	if len(storageProvider.stored) != len(expectedThumbnails) {
		t.Fatalf(
			"expected %d stored thumbnails, got %d",
			len(expectedThumbnails),
			len(storageProvider.stored),
		)
	}
	for i, expected := range expectedThumbnails {
		input := storageProvider.stored[i]
		if input.Key != expected.key {
			t.Fatalf("expected thumbnail key %s, got %s", expected.key, input.Key)
		}
		if input.ContentType != thumbnailContentType {
			t.Fatalf(
				"expected content type %s, got %s",
				thumbnailContentType,
				input.ContentType,
			)
		}
		if input.UserID != request.UserID {
			t.Fatalf("expected user ID %s, got %s", request.UserID, input.UserID)
		}
		if input.AvatarID != request.ID {
			t.Fatalf("expected avatar ID %s, got %s", request.ID, input.AvatarID)
		}
		assertJPEGSize(t, input.Body, expected.size)
	}

	expectedMessage := pkg.MessageResizeDone{
		ID: request.ID,
		ThumbnailS3Keys: pkg.ThumbnailS3Keys{
			Size100x100: "avatars/source/100x100.jpg",
			Size300x300: "avatars/source/300x300.jpg",
		},
	}
	if len(queueProvider.resizeDoneJobs) != 1 {
		t.Fatalf(
			"expected 1 completion message, got %d",
			len(queueProvider.resizeDoneJobs),
		)
	}
	if queueProvider.resizeDoneJobs[0] != expectedMessage {
		t.Fatalf(
			"expected completion message %#v, got %#v",
			expectedMessage,
			queueProvider.resizeDoneJobs[0],
		)
	}
}

func TestProcessorProcessResizeRejectsInvalidMessages(t *testing.T) {
	tests := []struct {
		name string
		body []byte
	}{
		{
			name: "invalid json",
			body: []byte("{"),
		},
		{
			name: "missing avatar ID",
			body: resizePayload(t, func(message *pkg.MessageResizeRequest) {
				message.ID = uuid.Nil
			}),
		},
		{
			name: "missing user ID",
			body: resizePayload(t, func(message *pkg.MessageResizeRequest) {
				message.UserID = uuid.Nil
			}),
		},
		{
			name: "missing file name",
			body: resizePayload(t, func(message *pkg.MessageResizeRequest) {
				message.FileName = ""
			}),
		},
		{
			name: "missing storage key",
			body: resizePayload(t, func(message *pkg.MessageResizeRequest) {
				message.Key = ""
			}),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			storageProvider := &fakeStorage{}
			queueProvider := &fakeQueue{}
			processor := newTestProcessor(queueProvider, storageProvider)

			err := processor.processResize(context.Background(), tt.body)
			if err == nil {
				t.Fatal("expected processResize to return error")
			}
			if len(storageProvider.retrievedKeys) != 0 {
				t.Fatalf(
					"storage should not be called, got retrieves %v",
					storageProvider.retrievedKeys,
				)
			}
			if len(storageProvider.stored) != 0 {
				t.Fatalf(
					"storage should not store thumbnails, got %d stores",
					len(storageProvider.stored),
				)
			}
			if len(queueProvider.resizeDoneJobs) != 0 {
				t.Fatalf(
					"queue should not publish completion, got %d messages",
					len(queueProvider.resizeDoneJobs),
				)
			}
		})
	}
}

func TestProcessorProcessResizeReturnsStoreErrorWithoutPublishingCompletion(
	t *testing.T,
) {
	ctx := context.Background()
	request := resizeRequest()
	storeErr := errors.New("store failed")

	storageProvider := &fakeStorage{
		retrieved: map[string][]byte{
			request.FileName: pngPayload(t, 640, 480),
		},
		storeErr: storeErr,
	}
	queueProvider := &fakeQueue{}
	processor := newTestProcessor(queueProvider, storageProvider)

	err := processor.processResize(ctx, jsonPayload(t, request))
	if !errors.Is(err, storeErr) {
		t.Fatalf("expected store error, got %v", err)
	}
	if len(storageProvider.stored) != 1 {
		t.Fatalf(
			"expected processing to stop after first failed store, got %d stores",
			len(storageProvider.stored),
		)
	}
	if len(queueProvider.resizeDoneJobs) != 0 {
		t.Fatalf(
			"queue should not publish completion, got %d messages",
			len(queueProvider.resizeDoneJobs),
		)
	}
}

func TestProcessorProcessResizeReturnsCompletionPublishError(t *testing.T) {
	ctx := context.Background()
	request := resizeRequest()
	publishErr := errors.New("publish failed")

	storageProvider := &fakeStorage{
		retrieved: map[string][]byte{
			request.FileName: pngPayload(t, 640, 480),
		},
	}
	queueProvider := &fakeQueue{resizeDoneErr: publishErr}
	processor := newTestProcessor(queueProvider, storageProvider)

	err := processor.processResize(ctx, jsonPayload(t, request))
	if !errors.Is(err, publishErr) {
		t.Fatalf("expected publish error, got %v", err)
	}
	if len(storageProvider.stored) != 2 {
		t.Fatalf("expected 2 stored thumbnails, got %d", len(storageProvider.stored))
	}
	if len(queueProvider.resizeDoneJobs) != 1 {
		t.Fatalf(
			"expected 1 completion publish attempt, got %d",
			len(queueProvider.resizeDoneJobs),
		)
	}
}

func TestProcessorProcessDeleteDeletesStorageKeys(t *testing.T) {
	storageProvider := &fakeStorage{}
	queueProvider := &fakeQueue{}
	processor := newTestProcessor(queueProvider, storageProvider)

	request := pkg.MessageDeleteRequest{
		ID: uuid.MustParse("22222222-2222-2222-2222-222222222222"),
		Keys: []string{
			"avatars/source/original.png",
			"avatars/source/100x100.jpg",
			"avatars/source/300x300.jpg",
		},
	}

	if err := processor.processDelete(
		context.Background(),
		jsonPayload(t, request),
	); err != nil {
		t.Fatalf("processDelete returned error: %v", err)
	}
	if !slices.Equal(storageProvider.deleted, request.Keys) {
		t.Fatalf("expected deleted keys %v, got %v", request.Keys, storageProvider.deleted)
	}
}

func TestProcessorProcessDeleteRejectsInvalidMessages(t *testing.T) {
	tests := []struct {
		name string
		body []byte
	}{
		{
			name: "invalid json",
			body: []byte("{"),
		},
		{
			name: "missing avatar ID",
			body: jsonPayload(t, pkg.MessageDeleteRequest{
				Keys: []string{"avatars/source/original.png"},
			}),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			storageProvider := &fakeStorage{}
			queueProvider := &fakeQueue{}
			processor := newTestProcessor(queueProvider, storageProvider)

			err := processor.processDelete(context.Background(), tt.body)
			if err == nil {
				t.Fatal("expected processDelete to return error")
			}
			if len(storageProvider.deleted) != 0 {
				t.Fatalf("storage should not be called, got deleted keys %v", storageProvider.deleted)
			}
		})
	}
}

func TestProcessorProcessDeleteReturnsStorageError(t *testing.T) {
	deleteErr := errors.New("delete failed")

	storageProvider := &fakeStorage{deleteErr: deleteErr}
	queueProvider := &fakeQueue{}
	processor := newTestProcessor(queueProvider, storageProvider)

	request := pkg.MessageDeleteRequest{
		ID:   uuid.MustParse("22222222-2222-2222-2222-222222222222"),
		Keys: []string{"avatars/source/original.png"},
	}

	err := processor.processDelete(context.Background(), jsonPayload(t, request))
	if !errors.Is(err, deleteErr) {
		t.Fatalf("expected delete error, got %v", err)
	}
	if !slices.Equal(storageProvider.deleted, request.Keys) {
		t.Fatalf("expected deleted keys %v, got %v", request.Keys, storageProvider.deleted)
	}
}

func TestCenterSquare(t *testing.T) {
	tests := []struct {
		name   string
		bounds image.Rectangle
		want   image.Rectangle
	}{
		{
			name:   "square",
			bounds: image.Rect(10, 20, 110, 120),
			want:   image.Rect(10, 20, 110, 120),
		},
		{
			name:   "landscape",
			bounds: image.Rect(10, 20, 210, 120),
			want:   image.Rect(60, 20, 160, 120),
		},
		{
			name:   "portrait",
			bounds: image.Rect(10, 20, 110, 220),
			want:   image.Rect(10, 70, 110, 170),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := centerSquare(tt.bounds)
			if got != tt.want {
				t.Fatalf("expected center square %v, got %v", tt.want, got)
			}
		})
	}
}

func newTestProcessor(queueProvider *fakeQueue, storageProvider *fakeStorage) *processor {
	return &processor{
		logger:  zerolog.Nop(),
		queue:   queueProvider,
		storage: storageProvider,
	}
}

func testDeleteDelivery(
	t *testing.T,
	acknowledger amqp.Acknowledger,
	tag uint64,
) amqp.Delivery {
	t.Helper()

	return amqp.Delivery{
		Acknowledger: acknowledger,
		DeliveryTag:  tag,
		Body: jsonPayload(t, pkg.MessageDeleteRequest{
			ID:   uuid.MustParse("22222222-2222-2222-2222-222222222222"),
			Keys: []string{"avatars/source/original.png"},
		}),
	}
}

func waitForSignal(t *testing.T, signal <-chan struct{}, description string) {
	t.Helper()

	select {
	case <-signal:
	case <-time.After(time.Second):
		t.Fatalf("timed out waiting for %s", description)
	}
}

func waitForResult(t *testing.T, result <-chan error) error {
	t.Helper()

	select {
	case err := <-result:
		return err
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for worker to stop")
		return nil
	}
}

func resizeRequest() pkg.MessageResizeRequest {
	return pkg.MessageResizeRequest{
		ID:       uuid.MustParse("22222222-2222-2222-2222-222222222222"),
		UserID:   uuid.MustParse("11111111-1111-1111-1111-111111111111"),
		FileName: "avatars/source/original.png",
		Key:      "avatars/source",
	}
}

func resizePayload(
	t *testing.T,
	mutate func(message *pkg.MessageResizeRequest),
) []byte {
	t.Helper()

	message := resizeRequest()
	mutate(&message)
	return jsonPayload(t, message)
}

func jsonPayload(t *testing.T, value any) []byte {
	t.Helper()

	payload, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("failed to marshal payload: %v", err)
	}

	return payload
}

func pngPayload(t *testing.T, width int, height int) []byte {
	t.Helper()

	img := image.NewRGBA(image.Rect(0, 0, width, height))
	for y := range height {
		for x := range width {
			img.Set(x, y, color.RGBA{
				R: uint8(x % 255),
				G: uint8(y % 255),
				B: uint8((x + y) % 255),
				A: 255,
			})
		}
	}

	var payload bytes.Buffer
	if err := png.Encode(&payload, img); err != nil {
		t.Fatalf("failed to encode test PNG: %v", err)
	}

	return payload.Bytes()
}

func assertJPEGSize(t *testing.T, payload []byte, size int) {
	t.Helper()

	img, err := jpeg.Decode(bytes.NewReader(payload))
	if err != nil {
		t.Fatalf("failed to decode thumbnail JPEG: %v", err)
	}
	if img.Bounds().Dx() != size || img.Bounds().Dy() != size {
		t.Fatalf(
			"expected JPEG size %dx%d, got %dx%d",
			size,
			size,
			img.Bounds().Dx(),
			img.Bounds().Dy(),
		)
	}
}

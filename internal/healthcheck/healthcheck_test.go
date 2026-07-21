package healthcheck

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

type stubDatabase struct {
	err error
}

func (s stubDatabase) Ping(context.Context) error {
	return s.err
}

type stubRabbitMQ struct {
	closed bool
}

func (s stubRabbitMQ) IsClosed() bool {
	return s.closed
}

func TestLiveness(t *testing.T) {
	handler := NewHandler(
		stubDatabase{err: errors.New("database is down")},
		stubRabbitMQ{closed: true},
	)
	recorder := httptest.NewRecorder()

	handler.Liveness(recorder, httptest.NewRequest(http.MethodGet, "/health/live", nil))

	if recorder.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d", http.StatusOK, recorder.Code)
	}
}

func TestReadiness(t *testing.T) {
	tests := []struct {
		name         string
		databaseErr  error
		rabbitClosed bool
		wantStatus   int
		wantResponse response
	}{
		{
			name:       "dependencies are available",
			wantStatus: http.StatusOK,
			wantResponse: response{
				Status: "ready",
				Checks: map[string]string{
					"database": "ok",
					"rabbitmq": "ok",
				},
			},
		},
		{
			name:         "database is unavailable",
			databaseErr:  errors.New("database is down"),
			rabbitClosed: false,
			wantStatus:   http.StatusServiceUnavailable,
			wantResponse: response{
				Status: "unavailable",
				Checks: map[string]string{
					"database": "unavailable",
					"rabbitmq": "ok",
				},
			},
		},
		{
			name:         "RabbitMQ is unavailable",
			rabbitClosed: true,
			wantStatus:   http.StatusServiceUnavailable,
			wantResponse: response{
				Status: "unavailable",
				Checks: map[string]string{
					"database": "ok",
					"rabbitmq": "unavailable",
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			handler := NewHandler(
				stubDatabase{err: tt.databaseErr},
				stubRabbitMQ{closed: tt.rabbitClosed},
			)
			recorder := httptest.NewRecorder()

			handler.Readiness(
				recorder,
				httptest.NewRequest(http.MethodGet, "/health/ready", nil),
			)

			if recorder.Code != tt.wantStatus {
				t.Fatalf("expected status %d, got %d", tt.wantStatus, recorder.Code)
			}

			var got response
			if err := json.NewDecoder(recorder.Body).Decode(&got); err != nil {
				t.Fatalf("decode response: %v", err)
			}
			if got.Status != tt.wantResponse.Status {
				t.Errorf("expected status body %q, got %q", tt.wantResponse.Status, got.Status)
			}
			for dependency, want := range tt.wantResponse.Checks {
				if got.Checks[dependency] != want {
					t.Errorf(
						"expected %s check %q, got %q",
						dependency,
						want,
						got.Checks[dependency],
					)
				}
			}
		})
	}
}

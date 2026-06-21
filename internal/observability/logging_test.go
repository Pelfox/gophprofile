package observability

import (
	"bytes"
	"context"
	"encoding/json"
	"testing"

	"go.opentelemetry.io/otel/trace"
)

func TestTraceContextHookAddsTraceFields(t *testing.T) {
	traceID := trace.TraceID{0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08, 0x09, 0x0a, 0x0b, 0x0c, 0x0d, 0x0e, 0x0f, 0x10}
	spanID := trace.SpanID{0x11, 0x12, 0x13, 0x14, 0x15, 0x16, 0x17, 0x18}
	ctx := trace.ContextWithSpanContext(
		context.Background(),
		trace.NewSpanContext(trace.SpanContextConfig{
			TraceID: traceID,
			SpanID:  spanID,
		}),
	)

	var output bytes.Buffer
	logger := NewLogger(&output)
	logger.Info().Ctx(ctx).Msg("correlated log")

	var payload map[string]any
	if err := json.Unmarshal(output.Bytes(), &payload); err != nil {
		t.Fatalf("failed to decode log payload: %v", err)
	}
	if payload[TraceIDLogField] != traceID.String() {
		t.Fatalf("expected trace ID %s, got %v", traceID.String(), payload[TraceIDLogField])
	}
	if payload[SpanIDLogField] != spanID.String() {
		t.Fatalf("expected span ID %s, got %v", spanID.String(), payload[SpanIDLogField])
	}
}

package observability

import (
	"io"

	"github.com/rs/zerolog"
	"go.opentelemetry.io/otel/trace"
)

const (
	// TraceIDLogField is the structured log field used to correlate logs with traces.
	TraceIDLogField = "trace_id"
	// SpanIDLogField is the structured log field used to correlate logs with spans.
	SpanIDLogField = "span_id"
)

// TraceContextHook adds OpenTelemetry trace identifiers to zerolog events.
type TraceContextHook struct{}

// Run enriches log events that were created with Event.Ctx(ctx).
func (TraceContextHook) Run(event *zerolog.Event, _ zerolog.Level, _ string) {
	spanContext := trace.SpanContextFromContext(event.GetCtx())
	if !spanContext.IsValid() {
		return
	}

	event.Str(TraceIDLogField, spanContext.TraceID().String())
	if spanContext.HasSpanID() {
		event.Str(SpanIDLogField, spanContext.SpanID().String())
	}
}

// NewLogger creates the shared JSON logger configured for trace correlation.
func NewLogger(output io.Writer) zerolog.Logger {
	zerolog.TimeFieldFormat = zerolog.TimeFormatUnix
	return zerolog.New(output).
		With().
		Timestamp().
		Logger().
		Hook(TraceContextHook{})
}

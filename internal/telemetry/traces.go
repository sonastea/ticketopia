package telemetry

import (
	"context"
	"time"

	"github.com/rs/zerolog"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/codes"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/trace"
)

// Tracer is the tracer for Ticketopia's operations.
func Tracer() trace.Tracer { return otel.Tracer(Scope) }

// EndSpan marks a failed operation without exporting potentially sensitive
// error text. Pass the returned error at the end of the operation.
func EndSpan(span trace.Span, err error) {
	if err != nil {
		span.SetStatus(codes.Error, "")
	}
	span.End()
}

// LogTrace adds IDs only when this span is already slow or marked as an error.
// A fast, successful span may not be exported; don't link routine logs to it.
func LogTrace(event *zerolog.Event, ctx context.Context) *zerolog.Event {
	span, ok := trace.SpanFromContext(ctx).(sdktrace.ReadOnlySpan)
	if !ok {
		return event
	}
	end := span.EndTime()
	if end.IsZero() {
		end = time.Now()
	}
	if !diagnosticSpan(span.Status().Code, end.Sub(span.StartTime())) {
		return event
	}
	sc := span.SpanContext()
	return event.Str("trace_id", sc.TraceID().String()).Str("span_id", sc.SpanID().String())
}

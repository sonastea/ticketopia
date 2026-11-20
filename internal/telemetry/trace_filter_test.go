package telemetry

import (
	"testing"
	"time"

	"go.opentelemetry.io/otel/codes"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace"
)

func TestTraceFilterKeepsFailureInOversizedTrace(t *testing.T) {
	recorder := tracetest.NewSpanRecorder()
	provider := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(newTraceFilter(recorder)))
	tracer := provider.Tracer(Scope)
	start := time.Now()
	ctx, root := tracer.Start(t.Context(), "request", trace.WithTimestamp(start))
	for range maxTraceChildSpans {
		_, child := tracer.Start(ctx, "ordinary", trace.WithTimestamp(start))
		child.End(trace.WithTimestamp(start.Add(time.Millisecond)))
	}
	_, failure := tracer.Start(ctx, "failure", trace.WithTimestamp(start))
	failure.SetStatus(codes.Error, "")
	failure.End(trace.WithTimestamp(start.Add(time.Millisecond)))
	root.End(trace.WithTimestamp(start.Add(2 * time.Millisecond)))

	spans := recorder.Ended()
	if len(spans) != 1+maxTraceChildSpans {
		t.Fatalf("expected bounded trace with root, got %d spans", len(spans))
	}
	for _, span := range spans {
		if span.SpanContext().SpanID() == failure.SpanContext().SpanID() {
			return
		}
	}
	t.Fatal("discarded the failure that made the trace worth exporting")
}

func TestTraceFilterDoesNotExportRoutineOverload(t *testing.T) {
	recorder := tracetest.NewSpanRecorder()
	provider := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(newTraceFilter(recorder)))
	tracer := provider.Tracer(Scope)
	start := time.Now()
	var busy []trace.Span
	for range maxInFlightTraces {
		_, root := tracer.Start(t.Context(), "busy", trace.WithTimestamp(start))
		busy = append(busy, root)
	}
	ctx, root := tracer.Start(t.Context(), "overflow", trace.WithTimestamp(start))
	_, routine := tracer.Start(ctx, "routine", trace.WithTimestamp(start))
	routine.End(trace.WithTimestamp(start.Add(time.Millisecond)))
	_, failure := tracer.Start(ctx, "failure", trace.WithTimestamp(start))
	failure.SetStatus(codes.Error, "")
	failure.End(trace.WithTimestamp(start.Add(time.Millisecond)))
	root.End(trace.WithTimestamp(start.Add(2 * time.Millisecond)))
	for _, span := range busy {
		span.End(trace.WithTimestamp(start.Add(2 * time.Millisecond)))
	}

	spans := recorder.Ended()
	if len(spans) != 1 || spans[0].SpanContext().SpanID() != failure.SpanContext().SpanID() {
		t.Fatalf("expected only the failure under overload, exported %d spans", len(spans))
	}
}

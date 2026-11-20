package telemetry

import (
	"context"
	"sync"
	"time"

	"go.opentelemetry.io/otel/codes"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/trace"
)

const (
	slowTrace          = 2 * time.Second
	maxInFlightTraces  = 1024
	maxTraceChildSpans = 16
)

func diagnosticSpan(status codes.Code, duration time.Duration) bool {
	return status == codes.Error || duration >= slowTrace
}

type pendingTrace struct {
	children   []sdktrace.ReadOnlySpan
	diagnostic bool
}

// traceFilter decides before the export queue, once the local root completes.
// Ticketopia's child operations finish before their parent. Keep their context
// together so a tool failure can retain its otherwise successful HTTP request.
type traceFilter struct {
	next sdktrace.SpanProcessor
	mu   sync.Mutex
	// nil after shutdown.
	pending map[trace.TraceID]pendingTrace
}

func newTraceFilter(next sdktrace.SpanProcessor) *traceFilter {
	return &traceFilter{next: next, pending: make(map[trace.TraceID]pendingTrace)}
}

func (f *traceFilter) OnStart(_ context.Context, span sdktrace.ReadWriteSpan) {
	if span.Parent().IsValid() && !span.Parent().IsRemote() {
		return
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.pending != nil && len(f.pending) < maxInFlightTraces {
		f.pending[span.SpanContext().TraceID()] = pendingTrace{}
	}
}

func (f *traceFilter) OnEnd(span sdktrace.ReadOnlySpan) {
	id := span.SpanContext().TraceID()
	localRoot := !span.Parent().IsValid() || span.Parent().IsRemote()
	diagnostic := diagnosticSpan(span.Status().Code, span.EndTime().Sub(span.StartTime()))

	f.mu.Lock()
	if f.pending == nil {
		f.mu.Unlock()
		return
	}
	group, tracked := f.pending[id]
	if tracked && !localRoot {
		if len(group.children) < maxTraceChildSpans {
			group.children = append(group.children, span)
		} else if diagnostic && !group.diagnostic {
			// Keep at least the first reason to retain an oversized trace.
			group.children[len(group.children)-1] = span
		}
		group.diagnostic = group.diagnostic || diagnostic
		f.pending[id] = group
		f.mu.Unlock()
		return
	}
	if localRoot {
		delete(f.pending, id)
	}
	f.mu.Unlock()

	// Still keep interesting individual spans when the in-flight buffer is full
	// or a child outlives its root. Neither case admits fast, successful spans.
	if !diagnostic && !group.diagnostic {
		return
	}
	f.next.OnEnd(span)
	for _, child := range group.children {
		f.next.OnEnd(child)
	}
}

func (f *traceFilter) ForceFlush(ctx context.Context) error { return f.next.ForceFlush(ctx) }

func (f *traceFilter) Shutdown(ctx context.Context) error {
	f.mu.Lock()
	f.pending = nil
	f.mu.Unlock()
	return f.next.Shutdown(ctx)
}

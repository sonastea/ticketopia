package api

import (
	"errors"
	"net/http"
	"strings"
	"testing"

	"github.com/labstack/echo/v4"
	"go.opentelemetry.io/otel"
	otelattribute "go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace"
	"go.opentelemetry.io/otel/trace/noop"
)

func TestRequestTracesUseRoutesAndRecordFailures(t *testing.T) {
	recorder := tracetest.NewSpanRecorder()
	otel.SetTracerProvider(sdktrace.NewTracerProvider(sdktrace.WithSampler(sdktrace.AlwaysSample()), sdktrace.WithSpanProcessor(recorder)))
	t.Cleanup(func() { otel.SetTracerProvider(noop.NewTracerProvider()) })

	e, _, logs := requestLogRoutes(t)
	e.GET("/failures/:event_id", func(c echo.Context) error { return errors.New("private error details") })
	e.GET(publicActivityStream, func(c echo.Context) error { return c.NoContent(http.StatusOK) })
	for _, path := range []string{"/failures/private-event-id?token=private-token", "/panics", "/healthz", publicActivityStream} {
		get(e, path)
	}
	spans := recorder.Ended()
	if len(spans) != 2 {
		t.Fatalf("expected only failure and panic spans, got %d", len(spans))
	}
	failed, panicked := spans[0], spans[1]
	if failed.Name() != "GET /failures/:event_id" || failed.Status().Code != codes.Error || failed.Status().Description != "" || len(failed.Events()) != 0 {
		t.Fatalf("unexpected failed span: %s, %v, %v", failed.Name(), failed.Status(), failed.Events())
	}
	attr := otelattribute.NewSet(failed.Attributes()...)
	route, _ := attr.Value("http.route")
	status, _ := attr.Value("http.response.status_code")
	if route.AsString() != "/failures/:event_id" || status.AsInt64() != 500 || strings.Contains(attr.Encoded(otelattribute.DefaultEncoder()), "private") {
		t.Errorf("unexpected attributes: %v", failed.Attributes())
	}
	if panicked.Status().Code != codes.Error {
		t.Errorf("panic span not marked as error: %v", panicked.Status())
	}
	lines := logLines(t, logs)
	if len(lines) != 2 || lines[0]["trace_id"] != failed.SpanContext().TraceID().String() || lines[1]["span_id"] != panicked.SpanContext().SpanID().String() {
		t.Errorf("error logs lack trace correlation: %v", lines)
	}
}

func TestMCPToolErrorIsAChildSpanEvenWhenHTTPReturnsOK(t *testing.T) {
	recorder := tracetest.NewSpanRecorder()
	otel.SetTracerProvider(sdktrace.NewTracerProvider(sdktrace.WithSampler(sdktrace.AlwaysSample()), sdktrace.WithSpanProcessor(recorder)))
	t.Cleanup(func() { otel.SetTracerProvider(noop.NewTracerProvider()) })

	response := mcpRequest(t, mcpRoutesFor(t, enabledMCP(60)), "")
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"isError":true`) {
		t.Fatalf("expected an MCP tool error with HTTP 200: %d %s", response.Code, response.Body.String())
	}
	spans := recorder.Ended()
	if len(spans) != 2 {
		t.Fatalf("expected tool and HTTP spans, got %d", len(spans))
	}
	tool, request := spans[0], spans[1]
	if request.Name() != "POST /mcp" || request.SpanKind() != trace.SpanKindServer || request.Status().Code == codes.Error {
		t.Errorf("unexpected HTTP span: %s, %s, %v", request.Name(), request.SpanKind(), request.Status())
	}
	if tool.Name() != "execute_tool find_events" || tool.Status().Code != codes.Error ||
		tool.Parent().SpanID() != request.SpanContext().SpanID() || tool.SpanContext().TraceID() != request.SpanContext().TraceID() {
		t.Errorf("tool failure isn't attached to HTTP trace: %s, %v", tool.Name(), tool.Status())
	}
}

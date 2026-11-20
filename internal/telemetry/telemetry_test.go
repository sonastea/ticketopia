package telemetry_test

import (
	"bytes"
	"database/sql"
	"io"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"github.com/sonastea/ticketopia/internal/appmcp"
	"github.com/sonastea/ticketopia/internal/telemetry"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
	colmetrics "go.opentelemetry.io/proto/otlp/collector/metrics/v1"
	coltrace "go.opentelemetry.io/proto/otlp/collector/trace/v1"
	tracepb "go.opentelemetry.io/proto/otlp/trace/v1"
	"google.golang.org/protobuf/proto"
)

var otelVars = []string{"OTEL_SDK_DISABLED", "OTEL_METRICS_EXPORTER", "OTEL_EXPORTER_OTLP_ENDPOINT", "OTEL_EXPORTER_OTLP_METRICS_ENDPOINT",
	"OTEL_EXPORTER_OTLP_PROTOCOL", "OTEL_EXPORTER_OTLP_METRICS_PROTOCOL", "OTEL_SERVICE_NAME", "OTEL_RESOURCE_ATTRIBUTES",
	"OTEL_TRACES_EXPORTER", "OTEL_EXPORTER_OTLP_TRACES_ENDPOINT", "OTEL_EXPORTER_OTLP_TRACES_PROTOCOL",
	"OTEL_TRACES_SAMPLER", "OTEL_TRACES_SAMPLER_ARG"}

func clearEnv(t *testing.T) {
	for _, name := range otelVars {
		t.Setenv(name, "")
	}
}

func TestConfigFromEnv(t *testing.T) {
	for name, c := range map[string]struct {
		env     map[string]string
		enabled bool
		invalid bool
	}{
		"nothing set":         {nil, false, false},
		"base endpoint":       {map[string]string{"OTEL_EXPORTER_OTLP_ENDPOINT": "http://collector:4318"}, true, false},
		"metrics endpoint":    {map[string]string{"OTEL_EXPORTER_OTLP_METRICS_ENDPOINT": "http://collector:4318/v1/metrics"}, true, false},
		"sdk disabled":        {map[string]string{"OTEL_EXPORTER_OTLP_ENDPOINT": "http://c:4318", "OTEL_SDK_DISABLED": "true"}, false, false},
		"exporter none":       {map[string]string{"OTEL_EXPORTER_OTLP_ENDPOINT": "http://c:4318", "OTEL_METRICS_EXPORTER": "none"}, false, false},
		"explicit http":       {map[string]string{"OTEL_EXPORTER_OTLP_ENDPOINT": "http://c:4318", "OTEL_EXPORTER_OTLP_PROTOCOL": "http/protobuf"}, true, false},
		"grpc":                {map[string]string{"OTEL_EXPORTER_OTLP_ENDPOINT": "http://c:4317", "OTEL_EXPORTER_OTLP_PROTOCOL": "grpc"}, false, true},
		"metrics grpc":        {map[string]string{"OTEL_EXPORTER_OTLP_ENDPOINT": "http://c:4317", "OTEL_EXPORTER_OTLP_METRICS_PROTOCOL": "grpc"}, false, true},
		"signal wins":         {map[string]string{"OTEL_EXPORTER_OTLP_ENDPOINT": "http://c:4318", "OTEL_EXPORTER_OTLP_METRICS_PROTOCOL": "http/protobuf", "OTEL_EXPORTER_OTLP_PROTOCOL": "grpc"}, true, false},
		"prometheus exporter": {map[string]string{"OTEL_EXPORTER_OTLP_ENDPOINT": "http://c:4318", "OTEL_METRICS_EXPORTER": "prometheus"}, false, true},
		"bad disabled":        {map[string]string{"OTEL_SDK_DISABLED": "maybe"}, false, true},
	} {
		t.Run(name, func(t *testing.T) {
			clearEnv(t)
			t.Setenv("OTEL_TRACES_EXPORTER", "none")
			for key, value := range c.env {
				t.Setenv(key, value)
			}
			got, err := telemetry.ConfigFromEnv()
			if (err != nil) != c.invalid || (got.MetricsEndpoint != "") != c.enabled {
				t.Fatalf("%+v %v", got, err)
			}
		})
	}
}

func TestTraceConfig(t *testing.T) {
	clearEnv(t)
	const base = "http://collector:4318"
	const traces = base + "/custom-traces"
	t.Setenv("OTEL_EXPORTER_OTLP_TRACES_ENDPOINT", traces)
	if got, err := telemetry.ConfigFromEnv(); err != nil || got.TracesEndpoint != traces || got.MetricsEndpoint != "" {
		t.Fatalf("trace-only config: %+v %v", got, err)
	}
	t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", base)
	if got, err := telemetry.ConfigFromEnv(); err != nil || got.TracesEndpoint != traces || got.MetricsEndpoint != base {
		t.Fatalf("trace-specific override: %+v %v", got, err)
	}
	t.Setenv("OTEL_EXPORTER_OTLP_TRACES_PROTOCOL", "grpc")
	if _, err := telemetry.ConfigFromEnv(); err == nil {
		t.Fatal("accepted unsupported trace protocol")
	}
	t.Setenv("OTEL_TRACES_EXPORTER", "none")
	if got, err := telemetry.ConfigFromEnv(); err != nil || got.TracesEndpoint != "" || got.MetricsEndpoint != base {
		t.Fatalf("trace opt-out: %+v %v", got, err)
	}
}

// Metrics and diagnostic traces share a resource and flush on shutdown.
func TestSetupExportsOverOTLPHTTP(t *testing.T) {
	var mu sync.Mutex
	var requests []*colmetrics.ExportMetricsServiceRequest
	var traceRequests []*coltrace.ExportTraceServiceRequest
	receiver := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		if r.Header.Get("Content-Type") != "application/x-protobuf" {
			t.Errorf("export %s %s", r.URL.Path, r.Header.Get("Content-Type"))
		}
		mu.Lock()
		switch r.URL.Path {
		case "/v1/metrics":
			var request colmetrics.ExportMetricsServiceRequest
			if err := proto.Unmarshal(body, &request); err != nil {
				t.Error(err)
			}
			requests = append(requests, &request)
		case "/v1/traces":
			var request coltrace.ExportTraceServiceRequest
			if err := proto.Unmarshal(body, &request); err != nil {
				t.Error(err)
			}
			traceRequests = append(traceRequests, &request)
		default:
			t.Errorf("unexpected OTLP path %s", r.URL.Path)
		}
		mu.Unlock()
		w.Header().Set("Content-Type", "application/x-protobuf")
		_, _ = w.Write(nil)
	}))
	defer receiver.Close()
	clearEnv(t)
	t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", receiver.URL)
	t.Setenv("OTEL_RESOURCE_ATTRIBUTES", "deployment.environment.name=test")

	// Created before Setup, as package-level instruments are.
	appmcp.NewMetrics().Refuse(appmcp.RefusedOrigin)
	config, err := telemetry.ConfigFromEnv()
	if err != nil {
		t.Fatalf("%+v %v", config, err)
	}
	stop, err := telemetry.Setup(t.Context(), config, "abc123", zerolog.Nop())
	if err != nil {
		t.Fatal(err)
	}
	if err := telemetry.ObserveDB(telemetry.Meter(), func() sql.DBStats { return sql.DBStats{InUse: 2, Idle: 3, MaxOpenConnections: 10} }); err != nil {
		t.Fatal(err)
	}
	appmcp.NewMetrics().Refuse(appmcp.RefusedBatch)
	tracer := telemetry.Tracer()
	now := time.Now()
	routineCtx, routine := tracer.Start(t.Context(), "routine", trace.WithTimestamp(now))
	_, routineChild := tracer.Start(routineCtx, "routine.child", trace.WithTimestamp(now))
	routineChild.End(trace.WithTimestamp(now.Add(time.Millisecond)))
	routine.End(trace.WithTimestamp(now.Add(2 * time.Millisecond)))

	_, slow := tracer.Start(t.Context(), "slow", trace.WithTimestamp(now))
	slow.End(trace.WithTimestamp(now.Add(2 * time.Second)))

	failingCtx, parent := tracer.Start(t.Context(), "request", trace.WithTimestamp(now))
	_, sibling := tracer.Start(failingCtx, "context", trace.WithTimestamp(now))
	sibling.End(trace.WithTimestamp(now.Add(time.Millisecond)))
	func() {
		defer func() { _ = recover() }()
		_, span := tracer.Start(failingCtx, "failed.child", trace.WithTimestamp(now))
		span.SetStatus(codes.Error, "")
		defer span.End(trace.WithTimestamp(now.Add(2 * time.Millisecond)))
		panic("private panic details")
	}()
	parent.End(trace.WithTimestamp(now.Add(3 * time.Millisecond)))
	if err := stop(t.Context()); err != nil {
		t.Fatal(err)
	}

	mu.Lock()
	defer mu.Unlock()
	if len(requests) == 0 {
		t.Fatal("nothing exported")
	}
	resource := map[string]string{}
	var names []string
	for _, rm := range requests[len(requests)-1].ResourceMetrics {
		for _, kv := range rm.Resource.Attributes {
			resource[kv.Key] = kv.Value.GetStringValue()
		}
		for _, sm := range rm.ScopeMetrics {
			for _, m := range sm.Metrics {
				names = append(names, m.Name)
				if m.Name == "ticketopia.mcp.refusals" {
					// Only the refusal recorded after Setup is exported: the
					// earlier one predates any provider.
					var reasons []string
					for _, p := range m.GetSum().DataPoints {
						for _, kv := range p.Attributes {
							reasons = append(reasons, kv.Value.GetStringValue())
						}
					}
					if !slices.Equal(reasons, []string{appmcp.RefusedBatch}) {
						t.Errorf("refusal reasons %v", reasons)
					}
				}
			}
		}
	}
	if resource["service.name"] != "ticketopia" || resource["service.version"] != "abc123" || resource["service.instance.id"] == "" ||
		resource["deployment.environment.name"] != "test" {
		t.Errorf("resource %v", resource)
	}
	for _, want := range []string{"ticketopia.mcp.refusals", "ticketopia.process.goroutines", "ticketopia.process.heap_objects",
		"ticketopia.db.connections", "ticketopia.db.connections.max"} {
		if !slices.Contains(names, want) {
			t.Errorf("missing %s in %s", want, strings.Join(names, ", "))
		}
	}
	spans := map[string]*tracepb.Span{}
	for _, request := range traceRequests {
		for _, rs := range request.ResourceSpans {
			if !proto.Equal(rs.Resource, requests[len(requests)-1].ResourceMetrics[0].Resource) {
				t.Errorf("trace and metrics resources differ: %v", rs.Resource)
			}
			for _, ss := range rs.ScopeSpans {
				for _, span := range ss.Spans {
					spans[span.Name] = span
				}
			}
		}
	}
	if len(spans) != 4 || spans["slow"] == nil || spans["request"] == nil || spans["context"] == nil || spans["failed.child"] == nil {
		t.Fatalf("expected only slow and failing traces with their context, got %v", spans)
	}
	for _, name := range []string{"context", "failed.child"} {
		if !bytes.Equal(spans[name].ParentSpanId, spans["request"].SpanId) || !bytes.Equal(spans[name].TraceId, spans["request"].TraceId) {
			t.Errorf("%s was detached from the request span", name)
		}
	}
	if events := spans["failed.child"].Events; len(events) != 0 {
		t.Errorf("SDK exported panic details: %v", events)
	}
}

func TestSetupDisabledIsANoOp(t *testing.T) {
	stop, err := telemetry.Setup(t.Context(), telemetry.Config{}, "x", zerolog.Nop())
	if err != nil || stop(t.Context()) != nil {
		t.Fatal(err)
	}
}

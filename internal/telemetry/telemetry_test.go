package telemetry_test

import (
	"database/sql"
	"io"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/rs/zerolog"
	"github.com/sonastea/ticketopia/internal/appmcp"
	"github.com/sonastea/ticketopia/internal/telemetry"
	colmetrics "go.opentelemetry.io/proto/otlp/collector/metrics/v1"
	"google.golang.org/protobuf/proto"
)

var otelVars = []string{"OTEL_SDK_DISABLED", "OTEL_METRICS_EXPORTER", "OTEL_EXPORTER_OTLP_ENDPOINT", "OTEL_EXPORTER_OTLP_METRICS_ENDPOINT",
	"OTEL_EXPORTER_OTLP_PROTOCOL", "OTEL_EXPORTER_OTLP_METRICS_PROTOCOL", "OTEL_SERVICE_NAME", "OTEL_RESOURCE_ATTRIBUTES"}

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
			for key, value := range c.env {
				t.Setenv(key, value)
			}
			got, err := telemetry.ConfigFromEnv()
			if (err != nil) != c.invalid || got.Enabled != c.enabled {
				t.Fatalf("%+v %v", got, err)
			}
		})
	}
}

// Metrics reach an OTLP/HTTP receiver at the standard path, named for the
// service and revision, including instruments created before Setup.
func TestSetupExportsOverOTLPHTTP(t *testing.T) {
	var mu sync.Mutex
	var requests []*colmetrics.ExportMetricsServiceRequest
	receiver := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var request colmetrics.ExportMetricsServiceRequest
		if r.URL.Path != "/v1/metrics" || r.Header.Get("Content-Type") != "application/x-protobuf" || proto.Unmarshal(body, &request) != nil {
			t.Errorf("export %s %s", r.URL.Path, r.Header.Get("Content-Type"))
		}
		mu.Lock()
		requests = append(requests, &request)
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
	if err != nil || !config.Enabled {
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
}

func TestSetupDisabledIsANoOp(t *testing.T) {
	stop, err := telemetry.Setup(t.Context(), telemetry.Config{}, "x", zerolog.Nop())
	if err != nil || stop(t.Context()) != nil {
		t.Fatal(err)
	}
}

package ticketopia

import (
	"context"
	"testing"

	"github.com/sonastea/ticketopia/internal/activity"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
)

type idleActivity struct{}

func (idleActivity) Snapshot(context.Context) (activity.Snapshot, error) {
	return activity.Snapshot{}, nil
}
func (idleActivity) Counts(context.Context, []string) (activity.Counts, error) {
	return activity.Counts{}, nil
}
func (idleActivity) Cleanup(context.Context) error { return nil }

// collect returns each exported metric's data points as attribute → value.
func collect(t *testing.T, reader *sdkmetric.ManualReader) map[string]map[string]int64 {
	t.Helper()
	var rm metricdata.ResourceMetrics
	if err := reader.Collect(t.Context(), &rm); err != nil {
		t.Fatal(err)
	}
	result := map[string]map[string]int64{}
	for _, sm := range rm.ScopeMetrics {
		for _, m := range sm.Metrics {
			points := map[string]int64{}
			add := func(dp metricdata.DataPoint[int64]) {
				key := ""
				for _, kv := range dp.Attributes.ToSlice() {
					key = kv.Value.Emit()
				}
				points[key] = dp.Value
			}
			switch data := m.Data.(type) {
			case metricdata.Gauge[int64]:
				for _, dp := range data.DataPoints {
					add(dp)
				}
			case metricdata.Sum[int64]:
				for _, dp := range data.DataPoints {
					add(dp)
				}
			}
			result[m.Name] = points
		}
	}
	return result
}

func TestObserveActivityAndCatalog(t *testing.T) {
	reader := sdkmetric.NewManualReader()
	meter := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader)).Meter("test")
	service := activity.New(t.Context(), idleActivity{}, 4)
	defer service.Close()
	service.RecordHTTP(429)
	service.RecordHTTP(429)
	if err := observeActivity(meter, service); err != nil {
		t.Fatal(err)
	}
	var catalog catalogStatus
	if err := catalog.observe(meter); err != nil {
		t.Fatal(err)
	}
	// Before the first status query, the catalog reports nothing.
	got := collect(t, reader)
	if len(got["ticketopia.catalog.status"]) != 0 || got["ticketopia.activity.refusals"]["429"] != 2 ||
		got["ticketopia.activity.streams"][""] != 0 || len(got["ticketopia.activity.failures"]) != 6 {
		t.Fatalf("first collection %v", got)
	}
	for _, name := range []string{"ticketopia.activity.pending", "ticketopia.activity.sql_jobs", "ticketopia.activity.cache",
		"ticketopia.activity.accounted_bytes", "ticketopia.activity.cache_lookups", "ticketopia.activity.overwritten_updates",
		"ticketopia.activity.write_errors", "ticketopia.activity.sql_statements"} {
		if _, ok := got[name]; !ok {
			t.Errorf("missing %s", name)
		}
	}
	catalog.set(map[string]int64{"stale_days": 3, "quota_used": 120})
	got = collect(t, reader)
	if got["ticketopia.catalog.status"]["stale_days"] != 3 || got["ticketopia.catalog.status"]["quota_used"] != 120 {
		t.Fatalf("catalog %v", got["ticketopia.catalog.status"])
	}
}

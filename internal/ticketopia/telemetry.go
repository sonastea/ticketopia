package ticketopia

import (
	"context"
	"sync/atomic"

	"github.com/sonastea/ticketopia/internal/activity"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

// observeActivity exports the live activity service's Stats, read once per
// collection: the same figures the "Live activity metrics" log carries.
// Durations are exported as a histogram by the activity package itself.
func observeActivity(meter metric.Meter, s *activity.Service) error {
	gauge := func(name, unit, description string) metric.Int64ObservableGauge {
		g, _ := meter.Int64ObservableGauge(name, metric.WithUnit(unit), metric.WithDescription(description))
		return g
	}
	counter := func(name, unit, description string) metric.Int64ObservableCounter {
		c, _ := meter.Int64ObservableCounter(name, metric.WithUnit(unit), metric.WithDescription(description))
		return c
	}
	streams := gauge("ticketopia.activity.streams", "{stream}", "Open live activity streams.")
	pending := gauge("ticketopia.activity.pending", "{request}", "Stream starts waiting for a projection.")
	jobs := gauge("ticketopia.activity.sql_jobs", "{job}", "Live activity SQL jobs in progress.")
	cache := gauge("ticketopia.activity.cache", "{item}", "Live activity cache size by kind (candidates, cohorts, owners).")
	accounted := gauge("ticketopia.activity.accounted_bytes", "By", "Bytes accounted to live activity caches (not process RSS).")
	statements := counter("ticketopia.activity.sql_statements", "{statement}", "Live activity SQL statements.")
	lookups := counter("ticketopia.activity.cache_lookups", "{lookup}", "Live activity cache lookups by result (hit, miss).")
	refused := counter("ticketopia.activity.refusals", "{request}", "Live activity requests refused by HTTP status (429, 503).")
	overwritten := counter("ticketopia.activity.overwritten_updates", "{update}", "Updates replaced before a slow stream read them.")
	writeErrors := counter("ticketopia.activity.write_errors", "{error}", "Stream write errors.")
	failures := counter("ticketopia.activity.failures", "{failure}", "Live activity failures by reason.")
	attr := func(key, value string) metric.ObserveOption {
		return metric.WithAttributes(attribute.String(key, value))
	}
	_, err := meter.RegisterCallback(func(_ context.Context, o metric.Observer) error {
		st := s.Stats()
		o.ObserveInt64(streams, int64(st.Streams))
		o.ObserveInt64(pending, int64(st.Pending))
		o.ObserveInt64(jobs, int64(st.SQLJobs))
		o.ObserveInt64(cache, st.Candidates, attr("kind", "candidates"))
		o.ObserveInt64(cache, st.Cohorts, attr("kind", "cohorts"))
		o.ObserveInt64(cache, st.Owners, attr("kind", "owners"))
		o.ObserveInt64(accounted, st.Bytes)
		o.ObserveInt64(statements, int64(st.SQLStatements))
		o.ObserveInt64(lookups, int64(st.CacheHits), attr("result", "hit"))
		o.ObserveInt64(lookups, int64(st.CacheMisses), attr("result", "miss"))
		o.ObserveInt64(refused, int64(st.HTTP429), attr("status", "429"))
		o.ObserveInt64(refused, int64(st.HTTP503), attr("status", "503"))
		o.ObserveInt64(overwritten, int64(st.Overwritten))
		o.ObserveInt64(writeErrors, int64(st.WriteErrors))
		for reason, n := range st.Failures {
			o.ObserveInt64(failures, int64(n), attr("reason", reason))
		}
		return nil
	}, streams, pending, jobs, cache, accounted, statements, lookups, refused, overwritten, writeErrors, failures)
	return err
}

// catalogStatus holds the latest event catalog operational status. The
// queries behind it run every 15 minutes, so collection reports the last
// result instead of querying MariaDB on every export.
type catalogStatus struct {
	latest atomic.Pointer[map[string]int64]
}

func (c *catalogStatus) set(stats map[string]int64) { c.latest.Store(&stats) }

func (c *catalogStatus) observe(meter metric.Meter) error {
	gauge, err := meter.Int64ObservableGauge("ticketopia.catalog.status", metric.WithUnit("1"),
		metric.WithDescription("Event catalog operational status by measure, as last logged (refreshed every 15 minutes)."))
	if err != nil {
		return err
	}
	_, err = meter.RegisterCallback(func(_ context.Context, o metric.Observer) error {
		if latest := c.latest.Load(); latest != nil {
			for measure, n := range *latest {
				o.ObserveInt64(gauge, n, metric.WithAttributes(attribute.String("measure", measure)))
			}
		}
		return nil
	}, gauge)
	return err
}

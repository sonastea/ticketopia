package activity

import (
	rtmetrics "runtime/metrics"
	"sync/atomic"
	"time"
)

// Fixed dimensions keep operational telemetry independent of recipient identity,
// filter contents and event/actor data. Durations are cumulative, disjoint buckets
// (<=1/10/50/250/1000/5000 ms, then +Inf), not sampled percentiles.
type timing struct {
	count   atomic.Uint64
	nanos   atomic.Uint64
	buckets [7]atomic.Uint64
}

type TimingStats struct {
	Count   uint64    `json:"count"`
	Seconds float64   `json:"seconds"`
	Buckets [7]uint64 `json:"buckets"`
}

func (t *timing) observe(start time.Time) {
	d := time.Since(start)
	t.count.Add(1)
	t.nanos.Add(uint64(d))
	i := 0
	for _, bound := range [...]time.Duration{time.Millisecond, 10 * time.Millisecond, 50 * time.Millisecond, 250 * time.Millisecond, time.Second, 5 * time.Second} {
		if d <= bound {
			break
		}
		i++
	}
	t.buckets[i].Add(1)
}
func (t *timing) stats() TimingStats {
	s := TimingStats{Count: t.count.Load(), Seconds: float64(t.nanos.Load()) / float64(time.Second)}
	for i := range s.Buckets {
		s.Buckets[i] = t.buckets[i].Load()
	}
	return s
}

type metrics struct {
	projection, selection, membership, counts, location, cleanup, delivery                   timing
	hits, misses, rejected, unavailable, overwritten, writeErrors                            atomic.Uint64
	candidateLimit, memoryLimit, queueLimit, streamLimit, projectionFailed, membershipFailed atomic.Uint64
	candidates, cohorts, owners, bytes                                                       atomic.Int64
}

// Stats is safe during SQL reads and shutdown. Cache gauges describe the last
// completed assembly; Bytes includes the reserved in-progress generation and
// fixed assembly allowance. It is accounting, not process RSS.
type Stats struct {
	AsOf          time.Time              `json:"as_of"`
	SQLStatements uint64                 `json:"sql_statements"`
	Streams       int                    `json:"streams"`
	Pending       int                    `json:"pending"`
	SQLJobs       int                    `json:"sql_jobs"`
	Candidates    int64                  `json:"candidates"`
	Cohorts       int64                  `json:"cohorts"`
	Owners        int64                  `json:"owners"`
	Bytes         int64                  `json:"accounted_bytes"`
	CacheHits     uint64                 `json:"cache_hits"`
	CacheMisses   uint64                 `json:"cache_misses"`
	HTTP429       uint64                 `json:"http_429"`
	HTTP503       uint64                 `json:"http_503"`
	Overwritten   uint64                 `json:"overwritten_updates"`
	WriteErrors   uint64                 `json:"write_errors"`
	Failures      map[string]uint64      `json:"failures"`
	Operations    map[string]TimingStats `json:"operations"`
}

func (s *Service) Stats() Stats {
	var statements uint64
	if repo, ok := s.repo.(interface{ QueryCount() uint64 }); ok {
		statements = repo.QueryCount()
	}
	s.mu.Lock()
	streams := len(s.subs)
	s.mu.Unlock()
	m := &s.metrics
	return Stats{AsOf: time.Now().UTC(), SQLStatements: statements, Streams: streams, Pending: len(s.pending), SQLJobs: len(s.jobs),
		Candidates: m.candidates.Load(), Cohorts: m.cohorts.Load(), Owners: m.owners.Load(), Bytes: m.bytes.Load(),
		CacheHits: m.hits.Load(), CacheMisses: m.misses.Load(), HTTP429: m.rejected.Load(), HTTP503: m.unavailable.Load(),
		Overwritten: m.overwritten.Load(), WriteErrors: m.writeErrors.Load(),
		Failures:   map[string]uint64{"candidate_limit": m.candidateLimit.Load(), "memory_limit": m.memoryLimit.Load(), "queue_limit": m.queueLimit.Load(), "stream_limit": m.streamLimit.Load(), "projection_failed": m.projectionFailed.Load(), "membership_failed": m.membershipFailed.Load()},
		Operations: map[string]TimingStats{"projection": m.projection.stats(), "selection": m.selection.stats(), "membership": m.membership.stats(), "counts": m.counts.stats(), "location": m.location.stats(), "cleanup": m.cleanup.stats(), "delivery": m.delivery.stats()},
	}
}

// ProcessStats exposes process-wide growth signals for soak checks and rollout
// observation without a debug endpoint: goroutines, live heap objects and the
// shared SQL pool's open connections. Like Stats, it carries no identities.
type ProcessStats struct {
	Goroutines         uint64 `json:"goroutines"`
	HeapBytes          uint64 `json:"heap_bytes"`
	SQLOpenConnections int    `json:"sql_open_connections"`
}

func (s *Service) ProcessStats() ProcessStats {
	samples := []rtmetrics.Sample{{Name: "/sched/goroutines:goroutines"}, {Name: "/memory/classes/heap/objects:bytes"}}
	rtmetrics.Read(samples)
	p := ProcessStats{Goroutines: samples[0].Value.Uint64(), HeapBytes: samples[1].Value.Uint64()}
	if repo, ok := s.repo.(interface{ OpenConnections() int }); ok {
		p.SQLOpenConnections = repo.OpenConnections()
	}
	return p
}

// Called under the projection gate, including on failed assembly and idle expiry.
func (s *Service) accountMetrics() {
	n, bytes := 0, s.viewBytes+s.membershipBytes()+(8<<20)
	if s.index != nil {
		n = len(s.index.data.Items)
		bytes += s.index.bytes * 2
	}
	s.metrics.candidates.Store(int64(n))
	s.metrics.cohorts.Store(int64(len(s.views)))
	s.metrics.owners.Store(int64(len(s.members)))
	s.metrics.bytes.Store(int64(bytes))
}

func (s *Service) RecordHTTP(status int) {
	switch status {
	case 429:
		s.metrics.rejected.Add(1)
	case 503:
		s.metrics.unavailable.Add(1)
	}
}

// Delivery measures server write/flush time, not commit-to-browser latency. The
// latter requires correlated workload observations and is reported by load tests.
func (s *Service) RecordDelivery(start time.Time, err error) {
	s.metrics.delivery.observe(start)
	if err != nil {
		s.metrics.writeErrors.Add(1)
	}
}

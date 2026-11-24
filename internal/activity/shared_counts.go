package activity

import (
	"sync"
	"time"
)

// DefaultSharedCounts is how long the server shares one event's totals across
// count requests and live streams. Every open page refreshes its counts about
// every 15 seconds; without sharing, each refresh counts every recommendation
// and interest for up to 101 events, so database work grows with visitors.
const DefaultSharedCounts = 10 * time.Second

// sharedCountEntries bounds remembered totals (a few dozen bytes each).
const sharedCountEntries = 20000

// countGeneration is implemented by repositories that count their own
// recommendation and interest writes in this process. A total read before a
// write is never served after it, so a person sees their own change at once;
// other replicas' writes appear within the window.
type countGeneration interface {
	Generation() uint64
}

type sharedCounts struct {
	mu      sync.Mutex
	ttl     time.Duration
	entries map[string]sharedCount
}

type sharedCount struct {
	value       Count
	asOf, until time.Time
	gen         uint64
}

// ShareCounts serves each event's totals for up to ttl, across Counts callers
// and live streams; zero (the default) reads every time. The server enables it
// with DefaultSharedCounts.
func (s *Service) ShareCounts(ttl time.Duration) {
	s.counts.mu.Lock()
	defer s.counts.mu.Unlock()
	s.counts.ttl = max(ttl, 0)
	clear(s.counts.entries)
}

func (s *Service) countGeneration() uint64 {
	if g, ok := s.repo.(countGeneration); ok {
		return g.Generation()
	}
	return 0
}

// lookup returns remembered totals and the IDs still to read, deduplicated.
func (c *sharedCounts) lookup(ids []string, now time.Time, gen uint64) (map[string]sharedCount, []string, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.ttl <= 0 {
		return nil, nil, false
	}
	hits := map[string]sharedCount{}
	var misses []string
	seen := map[string]bool{}
	for _, id := range ids {
		if seen[id] {
			continue
		}
		seen[id] = true
		if e, ok := c.entries[id]; ok && now.Before(e.until) && e.gen == gen {
			hits[id] = e
		} else {
			misses = append(misses, id)
		}
	}
	return hits, misses, true
}

func (c *sharedCounts) store(read Counts, now time.Time, gen uint64) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.ttl <= 0 {
		return
	}
	if c.entries == nil {
		c.entries = map[string]sharedCount{}
	}
	if len(c.entries)+len(read.Items) > sharedCountEntries {
		for id, e := range c.entries {
			if !now.Before(e.until) {
				delete(c.entries, id)
			}
		}
		if len(c.entries)+len(read.Items) > sharedCountEntries {
			clear(c.entries)
		}
	}
	for _, v := range read.Items {
		c.entries[v.EventID] = sharedCount{v, read.AsOf, now.Add(c.ttl), gen}
	}
}

// assemble answers ids in order from fresh and remembered totals. AsOf is the
// oldest time any returned total was read.
func assemble(ids []string, read Counts, hits map[string]sharedCount) Counts {
	fresh := map[string]Count{}
	for _, v := range read.Items {
		fresh[v.EventID] = v
	}
	out := Counts{AsOf: read.AsOf, Items: make([]Count, 0, len(ids))}
	for _, id := range ids {
		if v, ok := fresh[id]; ok {
			out.Items = append(out.Items, v)
			continue
		}
		e, ok := hits[id]
		if !ok {
			out.Items = append(out.Items, Count{EventID: id})
			continue
		}
		out.Items = append(out.Items, e.value)
		if out.AsOf.IsZero() || e.asOf.Before(out.AsOf) {
			out.AsOf = e.asOf
		}
	}
	return out
}

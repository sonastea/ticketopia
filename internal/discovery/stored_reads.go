package discovery

import (
	"context"
	"fmt"
	"slices"
	"sync"
	"time"
)

const (
	// DefaultSharedReads is how long visitors share one stored catalog read.
	// Stored results change when collection writes them (hours apart); a
	// write in this process ends the window at once, so it costs no visible
	// freshness while turning a busy search into one query per window.
	DefaultSharedReads = 10 * time.Second
	// storedReadEntries bounds remembered reads; each holds one page of events.
	storedReadEntries = 256
)

// CatalogGeneration is implemented by catalogs that count their own writes in
// this process. A shared read taken before a write is never served after it.
type CatalogGeneration interface {
	Generation() uint64
}

// storedReads coalesces concurrent identical catalog reads and remembers each
// for ttl. Reads are shared, so results are read-only; callers copy slices
// before modifying them, as with cached provider pages.
type storedReads struct {
	ttl     time.Duration
	mu      sync.Mutex
	entries map[string]storedRead
	gen     uint64
}

type storedRead struct {
	read       CatalogRead
	until      time.Time
	catalogGen uint64
}

func storedReadKey(q Query) string {
	key := q.Values().Encode()
	if c := q.Cursor; c != nil {
		key += fmt.Sprintf("|%q|%q|%q|%q|%d", c.Date, c.Time, c.Name, c.ID, c.Total)
	}
	return key
}

func (c *storedReads) get(key string, now time.Time, catalogGen uint64) (CatalogRead, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	entry, ok := c.entries[key]
	if !ok || !now.Before(entry.until) || entry.catalogGen != catalogGen {
		return CatalogRead{}, false
	}
	return entry.read, true
}

func (c *storedReads) generation() uint64 {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.gen
}

// put keeps a read only if no refresh finished since it began, so a read
// taken before new results were stored never outlives them.
func (c *storedReads) put(key string, read CatalogRead, gen, catalogGen uint64, now time.Time) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if gen != c.gen {
		return
	}
	if c.entries == nil {
		c.entries = make(map[string]storedRead)
	}
	if _, ok := c.entries[key]; !ok && len(c.entries) >= storedReadEntries {
		for k, entry := range c.entries {
			if !now.Before(entry.until) {
				delete(c.entries, k)
			}
		}
		// Arbitrary eviction suffices: a dropped read is only queried again.
		for k := range c.entries {
			if len(c.entries) < storedReadEntries {
				break
			}
			delete(c.entries, k)
		}
	}
	c.entries[key] = storedRead{read, now.Add(c.ttl), catalogGen}
}

// clear forgets every read after collection stores new results.
func (c *storedReads) clear() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.gen++
	clear(c.entries)
}

func (s *Service) catalogGeneration() uint64 {
	if g, ok := s.catalog.(CatalogGeneration); ok {
		return g.Generation()
	}
	return 0
}

// searchCatalog serves a recent shared read or makes one, coalescing callers.
// The query runs detached from any one caller, bounded by the repository's own
// timeout, so a visitor leaving does not fail the others waiting on it.
func (s *Service) searchCatalog(ctx context.Context, q Query) (CatalogRead, error) {
	if s.stored.ttl <= 0 {
		return s.catalog.Search(ctx, q, s.freshFor)
	}
	key := storedReadKey(q)
	if read, ok := s.stored.get(key, s.now(), s.catalogGeneration()); ok {
		return shareRead(read), nil
	}
	result := s.flights.DoChan("stored:"+key, func() (any, error) {
		return s.readCatalog(context.WithoutCancel(ctx), key, q)
	})
	select {
	case <-ctx.Done():
		return CatalogRead{}, ctx.Err()
	case r := <-result:
		if r.Err != nil {
			return CatalogRead{}, r.Err
		}
		return shareRead(r.Val.(CatalogRead)), nil
	}
}

// readCatalog queries the catalog and, when sharing reads, remembers a
// successful one.
func (s *Service) readCatalog(ctx context.Context, key string, q Query) (CatalogRead, error) {
	if s.stored.ttl <= 0 {
		return s.catalog.Search(ctx, q, s.freshFor)
	}
	gen, catalogGen := s.stored.generation(), s.catalogGeneration()
	read, err := s.catalog.Search(ctx, q, s.freshFor)
	if err == nil {
		s.stored.put(key, read, gen, catalogGen, s.now())
	}
	return read, err
}

// shareRead clips the shared items so a caller's append copies them.
func shareRead(read CatalogRead) CatalogRead {
	read.List.Items = slices.Clip(read.List.Items)
	return read
}

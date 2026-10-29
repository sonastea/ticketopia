package discovery

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"sync"
	"time"

	"github.com/sonastea/ticketopia/internal/kv"
	"github.com/sonastea/ticketopia/internal/models"
)

type cachePolicy struct {
	fresh, retain time.Duration
}

var (
	eventPolicy = cachePolicy{time.Hour, 24 * time.Hour}
	genrePolicy = cachePolicy{24 * time.Hour, 7 * 24 * time.Hour}
)

type cacheRecord[T any] struct {
	Data       T         `json:"data"`
	FetchedAt  time.Time `json:"fetched_at"`
	FreshUntil time.Time `json:"fresh_until"`
	StaleUntil time.Time `json:"stale_until"`
	NotFound   bool      `json:"not_found,omitempty"`
}

type cacheResult[T any] struct {
	data T
	meta models.Freshness
}

func cached[T any](ctx context.Context, s *Service, key string, policy cachePolicy, fetch func(context.Context) (T, error)) (T, models.Freshness, error) {
	var zero T
	if err := ctx.Err(); err != nil {
		return zero, models.Freshness{}, err
	}
	if record, ok := readRecord[T](ctx, s, key); ok && s.now().Before(record.FreshUntil) {
		if record.NotFound {
			return zero, models.Freshness{}, ErrNotFound
		}
		return record.Data, models.Freshness{DataAsOf: record.FetchedAt, FreshUntil: record.FreshUntil}, nil
	}
	// A cancelled browser request must not cancel the one refresh shared by other
	// clients. The service lifecycle and a hard timeout still bound that work.
	result := s.flights.DoChan(key, func() (any, error) {
		fetchCtx, cancel := context.WithTimeout(s.ctx, 10*time.Second)
		defer cancel()
		record, found := readRecord[T](fetchCtx, s, key)
		if found && s.now().Before(record.FreshUntil) {
			if record.NotFound {
				return nil, ErrNotFound
			}
			return cacheResult[T]{record.Data, models.Freshness{DataAsOf: record.FetchedAt, FreshUntil: record.FreshUntil}}, nil
		}
		data, err := fetch(fetchCtx)
		if err == nil {
			now := s.now()
			writeRecord(fetchCtx, s, key, cacheRecord[T]{
				Data: data, FetchedAt: now, FreshUntil: now.Add(policy.fresh), StaleUntil: now.Add(policy.retain),
			})
			return cacheResult[T]{data, models.Freshness{DataAsOf: now, FreshUntil: now.Add(policy.fresh)}}, nil
		}
		if errors.Is(err, ErrNotFound) {
			now := s.now()
			writeRecord(fetchCtx, s, key, cacheRecord[T]{
				NotFound: true, FetchedAt: now, FreshUntil: now.Add(5 * time.Minute), StaleUntil: now.Add(5 * time.Minute),
			})
			return nil, err
		}
		var unavailable *UnavailableError
		if found && !record.NotFound && s.now().Before(record.StaleUntil) && (errors.As(err, &unavailable) || errors.Is(err, context.DeadlineExceeded)) {
			return cacheResult[T]{record.Data, models.Freshness{DataAsOf: record.FetchedAt, Stale: true}}, nil
		}
		return nil, err
	})
	select {
	case <-ctx.Done():
		return zero, models.Freshness{}, ctx.Err()
	case result := <-result:
		if result.Err != nil {
			return zero, models.Freshness{}, result.Err
		}
		value := result.Val.(cacheResult[T])
		return value.data, value.meta, nil
	}
}

func readRecord[T any](ctx context.Context, s *Service, key string) (cacheRecord[T], bool) {
	data, err := s.cache.Get(ctx, key)
	if err != nil {
		if errors.Is(err, kv.ErrNotFound) {
			s.decoded.drop(key)
		} else if ctx.Err() == nil {
			s.logger.Warn().Err(err).Msg("Could not read discovery cache")
		}
		return cacheRecord[T]{}, false
	}
	record, ok := s.decoded.get(key, data).(cacheRecord[T])
	if !ok {
		if json.Unmarshal(data, &record) != nil || record.FetchedAt.IsZero() || record.FreshUntil.Before(record.FetchedAt) || record.StaleUntil.Before(record.FreshUntil) {
			s.logger.Warn().Msg("Invalid discovery cache entry; fetching fresh data")
			return record, false
		}
		s.decoded.put(key, data, record)
	}
	return record, s.now().Before(record.StaleUntil)
}

// decodedBudget bounds the stored bytes behind remembered records; decoded
// values are a few times larger.
const decodedBudget = 4 << 20

// decodedCache remembers each key's last decoded record while the store still
// returns the same bytes, so warm hits skip JSON decoding. The store stays the
// source of truth for expiry and invalidation: a write from any replica
// changes the bytes and is decoded afresh. Records are shared, so discovery
// results are read-only; callers copy slices before modifying them.
type decodedCache struct {
	mu      sync.Mutex
	entries map[string]decodedEntry
	size    int
}

type decodedEntry struct {
	raw    []byte
	record any
}

func (c *decodedCache) get(key string, raw []byte) any {
	c.mu.Lock()
	entry, ok := c.entries[key]
	c.mu.Unlock()
	// Stores return immutable bytes, so comparing outside the lock is safe.
	if ok && bytes.Equal(entry.raw, raw) {
		return entry.record
	}
	return nil
}

func (c *decodedCache) put(key string, raw []byte, record any) {
	if len(raw) > decodedBudget/16 {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.entries == nil {
		c.entries = make(map[string]decodedEntry)
	}
	c.size -= len(c.entries[key].raw)
	delete(c.entries, key)
	// Arbitrary eviction suffices: a dropped record is only decoded again.
	for k, entry := range c.entries {
		if c.size+len(raw) <= decodedBudget {
			break
		}
		delete(c.entries, k)
		c.size -= len(entry.raw)
	}
	c.entries[key] = decodedEntry{raw, record}
	c.size += len(raw)
}

func (c *decodedCache) drop(key string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if entry, ok := c.entries[key]; ok {
		delete(c.entries, key)
		c.size -= len(entry.raw)
	}
}

func writeRecord[T any](ctx context.Context, s *Service, key string, record cacheRecord[T]) {
	data, err := json.Marshal(record)
	if err == nil {
		err = s.cache.Set(ctx, key, data, record.StaleUntil.Sub(s.now()))
	}
	if err != nil {
		s.logger.Warn().Err(err).Msg("Could not write discovery cache")
	}
}

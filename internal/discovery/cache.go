package discovery

import (
	"context"
	"encoding/json"
	"errors"
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
		return record.Data, models.Freshness{DataAsOf: record.FetchedAt}, nil
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
			return cacheResult[T]{record.Data, models.Freshness{DataAsOf: record.FetchedAt}}, nil
		}
		data, err := fetch(fetchCtx)
		if err == nil {
			now := s.now()
			writeRecord(fetchCtx, s, key, cacheRecord[T]{
				Data: data, FetchedAt: now, FreshUntil: now.Add(policy.fresh), StaleUntil: now.Add(policy.retain),
			})
			return cacheResult[T]{data, models.Freshness{DataAsOf: now}}, nil
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
	var record cacheRecord[T]
	data, err := s.cache.Get(ctx, key)
	if err != nil {
		if !errors.Is(err, kv.ErrNotFound) && ctx.Err() == nil {
			s.logger.Warn().Err(err).Msg("Could not read discovery cache")
		}
		return record, false
	}
	if json.Unmarshal(data, &record) != nil || record.FetchedAt.IsZero() || record.FreshUntil.Before(record.FetchedAt) || record.StaleUntil.Before(record.FreshUntil) {
		s.logger.Warn().Msg("Invalid discovery cache entry; fetching fresh data")
		return record, false
	}
	return record, s.now().Before(record.StaleUntil)
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

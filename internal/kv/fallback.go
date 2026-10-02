package kv

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/rs/zerolog"
)

type fallbackStore struct {
	primary Store
	local   Store
	logger  zerolog.Logger
	mu      sync.Mutex
	retryAt time.Time
}

// WithFallback mirrors writes locally and uses that cache when the primary is
// unavailable. Failed backends are retried after a short cooldown.
func WithFallback(primary, local Store, logger zerolog.Logger) Store {
	return &fallbackStore{primary: primary, local: local, logger: logger}
}

func (s *fallbackStore) Get(ctx context.Context, key string) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if s.available() {
		opCtx, cancel := context.WithTimeout(ctx, operationTimeout)
		value, err := s.primary.Get(opCtx, key)
		cancel()
		if err == nil {
			return value, nil
		}
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if !errors.Is(err, ErrNotFound) {
			s.failed(err)
		}
	}
	return s.local.Get(ctx, key)
}

func (s *fallbackStore) Set(ctx context.Context, key string, value []byte, ttl time.Duration) error {
	if err := s.local.Set(ctx, key, value, ttl); err != nil {
		return err
	}
	if s.available() {
		opCtx, cancel := context.WithTimeout(ctx, operationTimeout)
		err := s.primary.Set(opCtx, key, value, ttl)
		cancel()
		if err := ctx.Err(); err != nil {
			return err
		}
		if err != nil {
			s.failed(err)
		}
	}
	return nil
}

func (s *fallbackStore) Close() error {
	return errors.Join(s.primary.Close(), s.local.Close())
}

func (s *fallbackStore) available() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return !time.Now().Before(s.retryAt)
}

func (s *fallbackStore) failed(err error) {
	s.mu.Lock()
	if time.Now().Before(s.retryAt) {
		s.mu.Unlock()
		return
	}
	s.retryAt = time.Now().Add(30 * time.Second)
	s.mu.Unlock()
	s.logger.Warn().Err(err).Msg("KV backend unavailable; using in-memory cache and retrying in 30 seconds")
}

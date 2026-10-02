package kv

import (
	"bytes"
	"context"
	"sync"
	"time"
)

type memoryEntry struct {
	value     []byte
	expiresAt time.Time
}

type memoryStore struct {
	mu      sync.Mutex
	entries map[string]memoryEntry
	done    chan struct{}
	closed  bool
}

func NewMemory() Store {
	store := &memoryStore{
		entries: make(map[string]memoryEntry),
		done:    make(chan struct{}),
	}
	go store.cleanup()
	return store
}

func (s *memoryStore) Get(ctx context.Context, key string) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return nil, ErrClosed
	}

	entry, ok := s.entries[key]
	if !ok || (!entry.expiresAt.IsZero() && !time.Now().Before(entry.expiresAt)) {
		delete(s.entries, key)
		return nil, ErrNotFound
	}
	return bytes.Clone(entry.value), nil
}

func (s *memoryStore) Set(ctx context.Context, key string, value []byte, ttl time.Duration) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return ErrClosed
	}

	entry := memoryEntry{value: bytes.Clone(value)}
	if ttl > 0 {
		entry.expiresAt = time.Now().Add(ttl)
	}
	s.entries[key] = entry
	return nil
}

func (s *memoryStore) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.closed {
		s.closed = true
		close(s.done)
		clear(s.entries)
	}
	return nil
}

func (s *memoryStore) cleanup() {
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()
	for {
		select {
		case <-s.done:
			return
		case now := <-ticker.C:
			s.mu.Lock()
			for key, entry := range s.entries {
				if !entry.expiresAt.IsZero() && !now.Before(entry.expiresAt) {
					delete(s.entries, key)
				}
			}
			s.mu.Unlock()
		}
	}
}

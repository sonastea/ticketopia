package kv

import (
	"context"
	"errors"
	"time"
)

var (
	ErrNotFound = errors.New("kv: key not found")
	ErrClosed   = errors.New("kv: store is closed")
)

const operationTimeout = time.Second

// Store is a concurrency-safe cache of byte values. Get returns ErrNotFound for
// missing or expired keys. Set replaces a value and its TTL; a non-positive TTL
// means the value does not expire. Callers may safely modify their byte slices.
type Store interface {
	Get(ctx context.Context, key string) ([]byte, error)
	Set(ctx context.Context, key string, value []byte, ttl time.Duration) error
	Close() error
}

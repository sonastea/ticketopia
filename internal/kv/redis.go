package kv

import (
	"context"
	"errors"
	"time"

	"github.com/redis/go-redis/v9"
)

type redisStore struct {
	client *redis.Client
}

// NewRedis connects to Redis or a Redis-compatible server, such as Valkey or DragonflyDB.
func NewRedis(ctx context.Context, rawURL string) (Store, error) {
	opts, err := redis.ParseURL(rawURL)
	if err != nil {
		return nil, err
	}
	opts.Protocol = 2
	opts.DisableIdentity = true
	opts.ContextTimeoutEnabled = true
	opts.DialTimeout = operationTimeout
	opts.ReadTimeout = operationTimeout
	opts.WriteTimeout = operationTimeout
	opts.PoolTimeout = operationTimeout
	opts.MaxRetries = -1
	opts.DialerRetries = 1

	client := redis.NewClient(opts)
	ctx, cancel := context.WithTimeout(ctx, operationTimeout)
	defer cancel()
	if err := client.Ping(ctx).Err(); err != nil {
		client.Close()
		return nil, err
	}
	return &redisStore{client: client}, nil
}

func (s *redisStore) Get(ctx context.Context, key string) ([]byte, error) {
	value, err := s.client.Get(ctx, key).Bytes()
	if errors.Is(err, redis.Nil) {
		return nil, ErrNotFound
	}
	return value, err
}

func (s *redisStore) Set(ctx context.Context, key string, value []byte, ttl time.Duration) error {
	if ttl < 0 {
		ttl = 0
	}
	return s.client.Set(ctx, key, value, ttl).Err()
}

func (s *redisStore) Close() error {
	return s.client.Close()
}

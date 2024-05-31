package infra

import (
	"context"
	"os"

	"github.com/redis/go-redis/v9"
)

func newRedisClient(ctx context.Context) (*redis.Client, error) {
	redisOpts, err := redis.ParseURL(os.Getenv("REDIS_URL"))
	if err != nil {
		return nil, err
	}

	redis := redis.NewClient(redisOpts)
	if err := redis.Ping(ctx).Err(); err != nil {
		return nil, err
	}

	return redis, nil
}

func NewRedisClient(ctx context.Context) (*redis.Client, error) {
	return newRedisClient(ctx)
}

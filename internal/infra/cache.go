package infra

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/rs/zerolog"
	"github.com/sonastea/ticketopia/internal/kv"
)

// NewCache defaults to memory and falls back to it if a configured backend
// cannot be opened. KV_URL takes precedence over the legacy REDIS_URL.
func NewCache(ctx context.Context, logger zerolog.Logger) kv.Store {
	local := kv.NewMemory()
	rawURL := strings.TrimSpace(os.Getenv("KV_URL"))
	if rawURL == "" {
		rawURL = strings.TrimSpace(os.Getenv("REDIS_URL"))
	}
	if rawURL == "" {
		logger.Info().Str("backend", "memory").Msg("Cache initialized")
		return local
	}

	parsedURL, err := url.Parse(rawURL)
	if err == nil && parsedURL.Scheme == "memory" {
		logger.Info().Str("backend", "memory").Msg("Cache initialized")
		return local
	}
	var primary kv.Store
	if err == nil {
		connectCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
		primary, err = openCache(connectCtx, parsedURL)
		cancel()
	}
	if err != nil {
		logger.Warn().Err(err).Msg("Could not connect to KV backend; using in-memory cache")
		return local
	}

	logger.Info().Str("backend", parsedURL.Scheme).Msg("Cache initialized with in-memory fallback")
	return kv.WithFallback(primary, local, logger)
}

func openCache(ctx context.Context, parsedURL *url.URL) (kv.Store, error) {
	// Valkey and DragonflyDB speak the Redis protocol, including redis(s) URLs.
	endpoint := *parsedURL
	switch endpoint.Scheme {
	case "redis", "rediss":
		return kv.NewRedis(ctx, endpoint.String())
	case "valkey", "dragonfly", "dragonflydb":
		endpoint.Scheme = "redis"
		return kv.NewRedis(ctx, endpoint.String())
	case "valkeys", "dragonflys", "dragonflydbs":
		endpoint.Scheme = "rediss"
		return kv.NewRedis(ctx, endpoint.String())
	case "nats", "tls":
		bucket := strings.TrimSpace(os.Getenv("KV_NATS_BUCKET"))
		if bucket == "" {
			bucket = "ticketopia_cache"
		}
		return kv.NewNATS(ctx, endpoint.String(), bucket)
	default:
		return nil, fmt.Errorf("unsupported KV URL scheme %q", endpoint.Scheme)
	}
}

# KV cache changes

## Unreleased

### 2026-10-02 — Backend-neutral cache with in-memory fallback

#### Added

- A backend-neutral KV cache with in-memory, Redis, Valkey, DragonflyDB, and NATS
  JetStream support.
- `KV_URL` for backend selection and `KV_NATS_BUCKET` for the NATS bucket name.
- Shared discovery caching with filter-aware/versioned keys, concurrent-miss
  deduplication, list-to-detail reuse, metadata-specific retention, negative
  caching, and stale results during provider outages.
- Cached IP-to-city hints with 24-hour successful and 15-minute unsuccessful
  retention, concurrent lookup deduplication, and shared city-level event reuse.

#### Changed

- Default to in-memory caching and fall back locally when a shared backend is
  unavailable. Runtime failures use a 30-second retry cooldown.
- Fetch fresh events when cached data cannot be read or decoded.
- Make `.env` optional, retain `REDIS_URL` compatibility, and close cache resources
  during shutdown.
- Keep original collection freshness on retained reads and bound external usage
  with request pacing, a daily budget, and provider-aware cooldowns.

See [cache configuration](../cache.md) for current setup and behavior.

# Cache changes

## Unreleased

### 2026-10-29 — No validator work for uncacheable responses

- Private and `no-store` JSON and calendar responses no longer hash their body for
  an ETag they never send. A 20-event JSON page fell from about 108 µs to 87 µs
  (24 -> 20 allocs); bytes, headers and public validators are unchanged.

See [public HTTP caching](../cache.md#public-http-caching).

### 2026-10-29 — Content-versioned browser assets

- Link the stylesheet and scripts as `/assets/<file>?v=<content hash>` and serve
  the current version as a year-long immutable response. Other asset requests
  revalidate against a version ETag, so a deploy can no longer be hidden behind
  Cloudflare's default four-hour cache.

See [browser assets](../cache.md#browser-assets).

### 2026-10-29 — Decoded warm hits and shared catalog refreshes

- Reuse each process's decoded discovery record while the store returns the same
  bytes, and stop copying in-memory reads. A warm 20-event page hit fell from
  about 312 µs/194 KB to under 1 µs on the memory store (about 10 µs when a
  network backend returns fresh bytes); see `BenchmarkCachedHit`.
- Start one in-process refresh goroutine per stored Discover scope; concurrent
  visitors share it instead of each parking a goroutine until it finishes.

See [source cache](../cache.md#source-cache).

### 2026-10-15 — Public HTTP caching and event revalidation

- Add deterministic complete-representation weak ETags and grammar-aware
  conditional GET to taxonomy, the embedded contract, event search/detail/history.
- Cap taxonomy HTTP freshness at 600 seconds and its discovery-owned deadline;
  revalidate event responses on every reuse. Stale/error/credential-bearing reads
  are never publicly cached or turned into 304 responses.
- Preserve validation, refresh/coverage work, JSON encoding, private policies and
  Vary. Update the OpenAPI contract and add controlled-clock/HTTP regressions.
- Extend the isolated MariaDB workload with 200/304 bytes, allocations, timings
  and repository-call measurements; retain the existing measured schema-15 work.

See [HTTP policies](../cache.md#public-http-caching) and
[measurements](../public-cache-measurements.md).

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

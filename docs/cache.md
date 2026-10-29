# Cache configuration

## Public HTTP caching

Only these anonymous successful GET responses opt into shared HTTP caching:

| Endpoint | Cache-Control |
| --- | --- |
| `/api/v1/categories`, `/api/v1/genres` | `public, max-age=N, must-revalidate` |
| `/api/v1/openapi.yaml` | `public, max-age=600, must-revalidate` |
| `/api/v1/events`, `/api/v1/events/:event_id`, `/api/v1/events/:event_id/history` | `public, no-cache` |

Taxonomy `N = floor(max(0, min(600 seconds, freshUntil - now)))`. Discovery
supplies the actual source deadline; its 24-hour policy remains centralized.
Expired/unknown freshness and stale fallback use `no-store`, without a validator
or 304. History reads durable observations directly and have no stale fallback.
Errors use `no-store` and retain their existing status and `Retry-After`.
Requests containing Authorization (even empty/invalid) or the configured session
cookie use `private, no-store`, bypassing conditional handling. Existing private
pages and personalized API policies remain in force.

Eligible responses carry `ETag: W/"v1-<sha256>"`. The hash covers representation
type/version and the complete serialized body, including events, history,
`data_as_of`, `stale`, coverage, total, cursor and limited-result indicators.
Identical representations produce identical validators across replicas; the
embedded OpenAPI validator is precomputed. JSON encoding is unchanged.

`If-None-Match` supports weak/strong comparison, lists, repeated fields, opaque
tags containing commas, and `*`. Malformed fields are nonmatches. Validation and
service reads happen first: errors always win. Matching eligible responses return
bodyless 304 with the current ETag, Cache-Control and applicable Vary values.
ETags are the sole validator; Last-Modified is not used. Query strings remain
part of cache identity. Existing Vary values are preserved; these handlers do
not compress or add Accept-Encoding negotiation.

Event storage requires revalidation on **every reuse**, including omitted date
defaults at the UTC day boundary. A 304 reduces response bytes, not database
queries: provider refreshes, coverage checks and normal service/database work
still happen. There is no additional application response cache. See the
[HTTP measurements](public-cache-measurements.md) for timing, allocations and
repository invocation counts.

## Source cache

Ticketopia uses an in-process, concurrency-safe KV cache by default. No cache
server or `.env` file is required for the cache. Discovery reads use these policies:

| Data | Fresh for | Retained from collection time |
| --- | --- | --- |
| Event searches and details | 1 hour | 24 hours |
| Category/genre/subgenre catalogs (including legacy music genres) | 24 hours | 7 days |
| Missing event (404) | 5 minutes | 5 minutes |

Fresh reads make no Ticketmaster requests. Expired freshness triggers one shared
refresh per key and process. On temporary failure, retained data is returned with
its original collection time and `stale: true`; after the retention deadline it
is unavailable. An authoritative 404 replaces an old event with a five-minute
negative entry. Empty search results are cached normally. Failed/malformed
provider responses never replace successful data. Corrupt cache entries are misses.

Keys are versioned and based on normalized filters, date range, page, and page
size, with explicit category scope and English metadata. All-category keys are
distinct from pre-expansion music-only keys. API keys are excluded. Equivalent query
ordering, whitespace, country-code case, and numeric defaults share keys. Search
results populate detail entries from embedded metadata. The HTML and JSON API
share these entries. Cancellation of one caller does not cancel a shared refresh.
Expired in-memory entries are cleaned up automatically.

Each process also remembers the decoded record behind recently read entries
(bounded by 4 MiB of stored bytes) and reuses it while the store returns
identical bytes, so warm hits skip JSON decoding. The store remains authoritative:
writes from any replica, expiry, eviction and corruption are seen on the next
read. Decoded records are shared between requests, so discovery results are
read-only; code that changes a result copies the affected slice first. The
in-memory store likewise returns its stored bytes without copying.

[IP-based city hints](location.md#ip-lookup-and-caching) use the same KV store:
24 hours for successful lookups and 15 minutes for unsuccessful lookups, with
per-process concurrent-request deduplication. They expire without a stale window.
Event search keys contain the resolved city and filters, so visitors in the same
city share event results without adding per-visitor event-cache entries.

See [discovery](discovery.md#external-request-budget) for timeouts, cooldowns,
per-process budget limits, and multi-instance coordination boundaries. These are
replaceable read caches; [MariaDB persistence](design/database.md) is planned for
durable history, user activity, and jobs.

Set `KV_URL` to use a shared cache:

| Backend | Example `KV_URL` |
| --- | --- |
| In-memory (explicit override) | `memory://` |
| Redis | `redis://localhost:6379/0` |
| Valkey | `valkey://localhost:6379/0` |
| DragonflyDB | `dragonfly://localhost:6379/0` |
| NATS JetStream KV | `nats://localhost:4222` |

Redis, Valkey, and DragonflyDB all support `redis://` and `rediss://` (TLS).
The `valkeys://`, `dragonflys://`, `dragonflydb://`, and `dragonflydbs://` aliases
are also accepted. Use `tls://` for a TLS-enabled NATS server. URL credentials are
passed to the client.

`KV_URL` takes precedence over `REDIS_URL`, which remains supported for existing
configurations. If neither is set, or the configured backend cannot be opened,
Ticketopia starts with the in-memory cache. After a startup fallback, restart the
application to reconnect to the shared cache.

When a connected backend fails during requests, writes continue to the local
cache and reads fall back to it. Remote operations have a one-second timeout;
after a failure, the backend is retried on the next operation after 30 seconds.
In-memory data is local to each process and is lost on restart.

## NATS

NATS requires version 2.11+ with JetStream enabled (`nats-server -js`). Ticketopia
creates a dedicated `ticketopia_cache` bucket automatically; set `KV_NATS_BUCKET`
to choose another name. The connection needs permission to access/create the
bucket and read/write its keys. Existing buckets must use history 1, no bucket-wide
TTL, and per-key TTL support (`LimitMarkerTTL`); mirrored/sourced buckets are not
used. Per-entry expiration is managed by JetStream. Keys are encoded so application
keys such as `events:1` work with NATS's key restrictions.

The application depends only on `internal/kv.Store` (`Get`, `Set` with TTL, and
`Close`), so additional backends can be implemented without changing event handlers.

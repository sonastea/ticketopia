# Cache configuration

Ticketopia uses an in-process, concurrency-safe KV cache by default. No cache
server or `.env` file is required. Event results expire after one hour, and expired
in-memory entries are cleaned up automatically.

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

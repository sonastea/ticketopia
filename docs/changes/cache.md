# KV cache changes

## Unreleased

### 2026-10-02 — Backend-neutral cache with in-memory fallback

#### Added

- A backend-neutral KV cache with in-memory, Redis, Valkey, DragonflyDB, and NATS
  JetStream support.
- `KV_URL` for backend selection and `KV_NATS_BUCKET` for the NATS bucket name.

#### Changed

- Default to in-memory caching and fall back locally when a shared backend is
  unavailable. Runtime failures use a 30-second retry cooldown.
- Fetch fresh events when cached data cannot be read or decoded.
- Make `.env` optional, retain `REDIS_URL` compatibility, and close cache resources
  during shutdown.

See [cache configuration](../cache.md) for current setup and behavior.

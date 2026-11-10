# Agent tools (MCP) changes

## Unreleased

### 2026-11-09 — k6 load baseline

- Add `make load`: a k6 mix of page views, `/api/v1` calls and public and
  signed-in `/mcp` tool calls against a `discover-last-known` review server,
  with per-scenario latency, error, tool-error and dropped-iteration thresholds
  and refusals counted as 429 or 503. It refuses non-loopback targets. See
  [load testing](../load-testing.md), including the first local baseline.

### 2026-11-09 — Signed-in saves and reminders with API tokens

- Add `my_saved_events`, `save_event`, `unsave_event`, `my_reminders`,
  `set_reminder` and `stop_reminder`, acting only for the account behind an
  `Authorization: Bearer` API token, through the same services and error
  wording as `/api/v1/me`. Without a token they return sign-in guidance.
- Verify tokens at the boundary inside the concurrency bound (401 with
  `WWW-Authenticate` when invalid, 503 when accounts are unavailable), limit
  each account like each IP, never accept session cookies, and annotate reads
  and destructive writes.
- Review-fixture accounts carry API tokens in the manifest; a MariaDB test
  checks per-account isolation, idempotent saves, reminder rules and refusal of
  cookies and invalid tokens.

### 2026-11-09 — Read-only public event tools at /mcp

- Serve `find_events`, `list_categories` and `event_details` over stateless
  Streamable HTTP at `/mcp`, off unless `MCP_ENABLED=true`. Tools reuse
  Discover's query parsing, the shared discovery service and the event-detail
  durable fallback, and report errors in the JSON API's wording.
- Limit each trusted client IP per replica (`MCP_RATE_PER_MINUTE`, default 60,
  429 with `Retry-After`, at most 4096 tracked IPs) and concurrent requests
  (`MCP_MAX_IN_FLIGHT`, default 8, immediate 503). Reject JSON-RPC batches, and
  browser origins outside `MCP_ALLOWED_ORIGINS` (default `AUTH_BASE_URL`) or
  loopback; the request `Host` is not trusted. Cap bodies at 64 KiB, mark responses `no-store`, and log every
  tool call, including rejected arguments, without arguments.
- Publish validated output schemas, keep provider retry delays in tool errors,
  and treat an explicit `limit` of 0 as invalid, as `/api/v1` does.
- Review servers also serve `/mcp` over fixture data. Tests cover the tools, the
  HTTP boundary, a `tools/call` without `initialize`, and stored-Discover
  results with per-event freshness against MariaDB.
- See [agent tools](../app-mcp.md) and the [design](../design/app-mcp.md).

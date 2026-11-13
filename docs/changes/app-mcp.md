# Agent tools (MCP) changes

## Unreleased

### 2026-11-13 — Accept ChatGPT's client metadata

- Client ID Metadata Documents that list `token_endpoint_auth_methods_supported`
  (MCP SEP-3149) are accepted when the list includes `none`, whatever the legacy
  `token_endpoint_auth_method` preference says. ChatGPT's document prefers
  `private_key_jwt` but lists `none`, and was refused; it now signs in as a
  public client. Verified against the live ChatGPT, OpenCode and Claude Code
  documents on a local server; see the [guide](../app-mcp.md#oauth-sign-in).

### 2026-11-12 — OAuth sign-in with Client ID Metadata Documents

- Add an OAuth 2.1 authorization server (`MCP_OAUTH_ENABLED`, on by default
  with accounts) so agents sign in through the browser: protected resource and
  authorization server metadata, `/oauth/authorize` with S256 PKCE, RFC 9207
  `iss`, and `/oauth/token` with rotating refresh tokens; replaying a rotated
  refresh token revokes the connection. Clients identify
  with Client ID Metadata Documents, fetched only from public HTTPS addresses;
  DCR and client secrets are not supported.
- A consent page names the app and its metadata host and lets people withhold
  changes (`read` versus `write` scopes). With accounts, `/mcp` now requires
  sign-in for every request by default (`MCP_REQUIRE_SIGN_IN`), answering
  requests without a token with a 401 challenge, so agent traffic is attributed
  and limited per account; with sign-in optional, only signed-in tool calls are
  challenged. Changes through read-only connections get 403
  `insufficient_scope`, and tools also refuse writes without the scope.
- Remote `make load-test` runs send `TOKEN` on public tool calls too; review
  servers keep sign-in optional so the local baseline stays anonymous.
- Connections are listed under **Connected apps** on the account page and can be
  disconnected; **Sign out everywhere** disconnects them all, serialized with
  code redemption. Schema 21 adds `oauth_codes`, `oauth_grants` and
  `oauth_refresh_history` with runtime grants in every provisioning path.
- Tests cover the service rules, document fetching and SSRF refusal, a real MCP
  SDK client signing in and stepping up from read-only, and code/refresh single
  use, replay revocation and redemption racing sign-out across three replicas
  against MariaDB. Real hosted clients still need a public deployment to verify;
  see the [feature backlog](../goals/feature-backlog.md).

### 2026-11-11 — Per-tool metrics and busy wording

- Log an `MCP metrics` record each minute with changes: per-tool calls,
  outcomes, signed-in share and duration buckets, plus boundary refusals by
  reason, with fixed dimensions (unregistered tool names count as `unknown`).
- `set_reminder` on an unsaved event says to save it first, and overload is
  reported as Ticketopia being busy, not as a provider failure.

### 2026-11-09 — k6 load baseline

- Add `make load-test`: a k6 mix of page views, `/api/v1` calls and public and
  signed-in `/mcp` tool calls against a `discover-last-known` review server,
  with per-scenario latency, error, tool-error and dropped-iteration thresholds
  and refusals counted as 429 or 503. It refuses non-loopback targets. See
  [load testing](../load-testing.md), including the first local baseline.
- `make simulate-users` (k6) simulates signed-in members browsing,
  saving, marking interest, recommending, following and discussing through the
  browser's form posts with think time, against a new `populated` review scenario
  of 40 members with sessions in the manifest.
- `target=` (`TARGET`) aims the same script at a dev or production server:
  read-only cached searches and details, signed-in reads only with `TOKEN`,
  rates ramped under the default `/mcp` limit, setup checks, and automatic
  abort on errors or slow responses.

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

# Agent tools (MCP) changes

## Unreleased

### 2026-11-13 — Token revocation endpoint (RFC 7009)

- Agents disconnect themselves at `POST /oauth/revoke`, advertised as
  `revocation_endpoint` in server metadata: either of a connection's current
  tokens with the client's `client_id` ends the whole connection, as
  **Disconnect** on the account page does. Unknown, expired, rotated or other
  clients' tokens get the same empty 200 and change nothing. It shares the token
  endpoint's public-client, form-body, CORS and per-IP rules; storage failures
  get 503 with `Retry-After`. No schema change. Tests cover the service rules,
  the HTTP endpoint and cross-replica revocation against MariaDB; see the
  [guide](../app-mcp.md#oauth-sign-in).

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

Older entries: [agent tools archive](archive/app-mcp/).

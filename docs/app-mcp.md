# Agent tools (MCP)

Ticketopia serves event tools, and once signed in (OAuth or an API token) the
caller's own saves and reminders, to people's AI agents and editors over the
[Model Context Protocol](https://modelcontextprotocol.io/) at **`/mcp`**. It is
off unless `MCP_ENABLED=true`. The [design](design/app-mcp.md) describes later
steps (a separate deployment); this page covers what exists.
[Load testing](load-testing.md) covers `make load-test` and baselines.

## Tools

| Tool | Behavior |
|---|---|
| `find_events` | Search by city, country, dates, category, keyword, sort and limit, with `cursor` paging. Same validation, defaults, caching and results as Discover and `GET /api/v1/events`. |
| `list_categories` | Categories and their IDs for `find_events` `category_id`, as `GET /api/v1/categories`. |
| `event_details` | One event by ID, as `GET /api/v1/events/{event_id}`, including the durable last-known fallback. |

Signed-in tools act only on the account behind the request's token. The
saved-event tools are listed when accounts and MariaDB are enabled; the reminder
tools also need [email reminders](reminders.md) (`REMINDERS_ENABLED=true`):

| Tool | Behavior |
|---|---|
| `my_saved_events` | The caller's saved events, newest first, with `limit`/`cursor` paging, as `GET /api/v1/me/saved-events`. |
| `save_event` / `unsave_event` | Save or remove one event, idempotently, returning the resulting state. Removing also stops its reminder. |
| `my_reminders` | The caller's on-sale reminders and their delivery states, as `GET /api/v1/me/reminders`. |
| `set_reminder` / `stop_reminder` | Set (1–10080 minutes before the public sale) or stop a reminder; the event must be saved first, and delivery follows the account's email preferences. |

Read tools are annotated read-only. `unsave_event`, `stop_reminder` and
`set_reminder` (changing a reminder replaces its timing) are marked destructive;
`save_event` is additive. All writes are idempotent.

`find_events` dates are `YYYY-MM-DD` and match each event's local date, as on
Discover. `start_date` defaults to today in UTC and `end_date` to 90 days later
(at most 366 days). `limit` is 1–100, default 20. For
the next page, pass `next_cursor` as `cursor` with exactly the same other
arguments; a cursor is bound to its filters.

Results keep the API's freshness metadata: list `meta` and, for stored Discover
lists, per-event `meta` with `stale` for last-known events. Each tool publishes
an output schema, and results are validated against it. Errors are tool errors
with the API's wording (for example `limit: must be an integer from 1 to 100`),
plus `Retry after N seconds.` when the provider is cooling down, so agents can
correct or pace their requests. Signed-in tool errors use the account API's
wording.

## Signing in

With accounts enabled, **every `/mcp` request needs sign-in by default**, public
tools included, so all agent traffic is attributed to an account and limited
per account; requests without a token get a **401** challenge that starts
OAuth sign-in. `MCP_REQUIRE_SIGN_IN=false` lets public tools (and
`initialize`/`tools/list`) work anonymously again, limited per IP. Without
accounts nobody can sign in, so only the public tools are served, anonymously.

Sign in with either:

- **OAuth** (on by default with accounts; `MCP_OAUTH_ENABLED=false` turns it
  off): the agent opens a
  browser, the person signs in with Google if needed and approves the connection,
  and the agent receives tokens. This is how Claude, ChatGPT, VS Code and other
  apps without custom headers connect. See [OAuth sign-in](#oauth-sign-in).
- **An API token**: create one under **API access** on your account page (`/me`;
  see [accounts](accounts.md#sessions-and-api-clients)) and configure your client
  to send `Authorization: Bearer <token>`. API tokens carry full read and write
  access. Revoking the token cuts the agent off.

A token that is sent must be valid: invalid, expired or revoked tokens get
**401** with `WWW-Authenticate: Bearer`. Browser session cookies are never
accepted on `/mcp`, so a page cannot act with someone's sign-in. With sign-in
optional and OAuth off, a signed-in tool called without a token returns a tool
error explaining how to connect.

### OAuth sign-in

Ticketopia is its own OAuth 2.1 authorization server, with `AUTH_BASE_URL` as
the issuer and `AUTH_BASE_URL/mcp` as the only resource tokens are issued for.

- **Discovery.** `/.well-known/oauth-protected-resource/mcp` (and the root
  `/.well-known/oauth-protected-resource`) describe `/mcp` (RFC 9728);
  `/.well-known/oauth-authorization-server` describes the server (RFC 8414). Both
  allow any origin, as do the token and revocation endpoints, which never use
  cookies.
- **Challenges.** A request without a token gets **401** with
  `resource_metadata` and `scope="read write"` in `WWW-Authenticate`, so clients
  start sign-in: every request while sign-in is required, otherwise only
  signed-in tool calls. Changes through a read-only connection get **403**
  `error="insufficient_scope"`, so clients can ask again.
- **Clients** identify themselves with a
  [Client ID Metadata Document](https://modelcontextprotocol.io/specification/2026-07-28/basic/authorization/client-registration):
  an HTTPS `client_id` URL serving JSON with the same `client_id`, a
  `client_name` and `redirect_uris`. Dynamic Client Registration and client
  secrets are not supported. Clients sign in as public clients (`none`): a
  document's `token_endpoint_auth_methods_supported` must include `none` when
  it is present (MCP SEP-3149; ChatGPT lists `none` and `private_key_jwt`),
  and otherwise its legacy `token_endpoint_auth_method` must be `none` or
  absent. Documents are fetched over HTTPS on port 443 from
  public addresses only (no redirects, 5 KB, 5 seconds) and cached per replica
  for their `max-age` (1 minute to 24 hours, default 10 minutes).
- **Redirects** must match a registered URI exactly, except loopback `http`
  URIs, which may use any port (RFC 8252). Registered URIs must be `https`,
  loopback `http` or an app scheme such as `cursor://`. A request with an
  untrusted `client_id` or `redirect_uri` shows an error page instead of
  redirecting.
- **Authorization** at `/oauth/authorize` requires `response_type=code` and PKCE
  with `S256`; `resource`, when sent, must be `AUTH_BASE_URL/mcp`. Signed-out
  people sign in first and return to the request. The consent page names the app
  and the host serving its metadata, lists what it can do, and offers **Also
  allow changes** when `write` is requested; leaving it off makes a read-only
  connection. Responses carry `iss` (RFC 9207). The page cannot be framed.
- **Scopes**: `read` (public tools, saved events, reminders) and `write` (save,
  unsave, set and stop reminders). An omitted scope requests both.
- **Tokens** from `/oauth/token` (form-encoded, public clients only): access
  tokens (`tko_at_…`) last one hour; refresh tokens (`tko_rt_…`), issued when
  the client's document lists the `refresh_token` grant, rotate on every use.
  Presenting a rotated refresh token again revokes the whole connection: it
  leaked, or the client raced its own refreshes, and the two can't be told
  apart, so the person reconnects either way. A refresh may narrow
  but never widen scopes, and a rejected one changes nothing. A connection
  expires after 30 days without a refresh. Codes last five minutes and work
  once, even if the exchange fails; `redirect_uri` may be omitted (PKCE binds
  the code) but must match when sent. Only hashes are stored. Access tokens work
  only on `/mcp`, never on `/api/v1`.
- **Connections.** Each approval creates a connection, listed under **Connected
  apps** on the account page with its access; **Disconnect** stops its tokens
  immediately. An account keeps at most ten; approving another removes the
  oldest. **Sign out everywhere** also disconnects every app, including any whose
  code exchange is in progress.
- **Revocation** (RFC 7009): an agent disconnects itself by posting `token`
  (either current token) and its `client_id` to `/oauth/revoke`, the
  `revocation_endpoint` in server metadata, form-encoded and without client
  authentication. Either token ends the whole connection, as **Disconnect**
  does; so does a refresh token already rotated away, which means the client
  kept a stale token or it leaked, as with a replay at the token endpoint. A
  replaced access token is simply unknown. `token_type_hint` is accepted but
  unneeded, since the prefix identifies the token. The answer is an empty
  **200** whether or not anything was revoked: unknown, expired and other
  clients' tokens change nothing. A missing `token` gets `invalid_request`, a
  missing `client_id` or any `Authorization` header `invalid_client`, and a
  storage failure **503** with `Retry-After`. Each answered request logs an
  `OAuth token revocation` record with its `outcome` (`revoked`,
  `revoked_rotated`, `unknown` or `other_client`, a client bug or a token in
  the wrong hands) and the claimed client's metadata host, never the token or
  account.
- The token and revocation endpoints share one allowance per client IP, like
  `/mcp` (`MCP_RATE_PER_MINUTE`); authorization pages share the sign-in limit of 20
  attempts per IP per ten minutes.

OAuth needs schema version 21 (`oauth_codes`, `oauth_grants`,
`oauth_refresh_history`); run migrations
and reconcile runtime grants before starting the new version, as for any
schema change.

## Protocol and limits

- Streamable HTTP in **stateless** mode with JSON responses: each `POST /mcp`
  stands alone, so any replica can answer and no session is kept. A
  `tools/call` needs no prior `initialize` request.
- Each trusted client IP, and each signed-in account, may make
  `MCP_RATE_PER_MINUTE` requests per minute per replica (default 60, bursts up
  to a minute's allowance). Excess requests get
  **429** with `Retry-After`. Client IPs follow `TRUSTED_PROXY_CIDRS`. At most
  4096 client IPs are tracked per replica; while full, new IPs get 429.
- One request is one call: JSON-RPC batches get **400**.
- At most `MCP_MAX_IN_FLIGHT` requests run at once per replica (default 8, below
  the default database pool of 10); excess gets **503** with `Retry-After: 1`
  immediately rather than queuing.
- Requests carrying an `Origin` header must come from an allowed origin or a
  loopback origin (local tools such as the MCP Inspector); others get **403**.
  `MCP_ALLOWED_ORIGINS` (comma-separated) sets the allowed origins and defaults
  to `AUTH_BASE_URL`, which applies even when accounts are disabled. The request's own `Host` is not trusted, since DNS rebinding controls
  it. Clients without an `Origin` header (agents, servers) are unaffected, and a
  reverse proxy may connect over loopback with the public `Host`. CORS stays
  disabled.
- Requests above 64 KiB get **413**. Responses are `Cache-Control: no-store`.
- Every `tools/call` is logged with its tool, outcome (`ok`, `tool_error`,
  `error`) and duration, never its arguments, including calls rejected for
  invalid arguments. Names that match no tool are logged as `unknown`.
- Each replica logs an `MCP metrics` record every minute in which anything
  changed: per tool, cumulative `calls`, `ok`, `tool_errors`, `errors`,
  `signed_in`, `seconds` and seven disjoint duration buckets (≤1 / ≤10 / ≤50 /
  ≤250 / ≤1000 / ≤5000 ms and more); and `refusals` at the boundary by reason
  (`origin`, `ip_rate`, `account_rate`, `busy`, `unauthorized`,
  `accounts_unavailable`, `batch`, `too_large`, and with OAuth `sign_in_required`
  and `insufficient_scope`). Dimensions are fixed: no
  arguments, accounts or IPs. Use interval deltas, as for
  [live-activity telemetry](live-activity-rollout.md#operational-telemetry).
  With an OTLP endpoint configured, the same counts, a duration histogram and
  revocation outcomes are also exported as metrics; see
  [observability](observability.md#metrics).
- When Ticketopia itself is busy (for example its database does not answer in
  time), tools report "Ticketopia is busy right now… Retry after 5 seconds."
  rather than a provider failure.
- `/mcp` is JSON-RPC, not REST, so it is not part of the
  [OpenAPI contract](openapi.md).

## Connect a client

Enable it locally with `MCP_ENABLED=true` in `.env` and run the app (`air`), then
add the remote server to your client:

```sh
opencode mcp add ticketopia --url http://localhost:8080/mcp \
  --header 'Authorization=Bearer {env:TICKETOPIA_TOKEN}'
claude mcp add --transport http ticketopia http://localhost:8080/mcp \
  --header "Authorization: Bearer $TICKETOPIA_TOKEN"
npx @modelcontextprotocol/inspector   # then connect to http://localhost:8080/mcp
```

Keep the token in an environment variable rather than in committed
configuration. Omitting the header works only for public tools, and only with
`MCP_REQUIRE_SIGN_IN=false` or without accounts.

With OAuth on (the default with accounts), omit the header instead: clients that
support MCP OAuth with Client ID Metadata Documents (Claude Code, OpenCode,
VS Code, Claude and ChatGPT connectors) open the browser to sign in when they
connect (or, with sign-in optional, when a signed-in tool first needs it). The
client app signs in, not the model, so OpenCode or Claude Code connects the same
way with any model (GLM, GPT, Claude). In OpenCode, add the server without a
header (`opencode mcp add ticketopia --url …/mcp`) and sign in from `/mcps`.
Hosted apps (Claude on the web, ChatGPT) can only reach a public HTTPS
deployment, not `localhost`. Clients that only support Dynamic Client
Registration cannot sign in; give them an API token header instead.

The [review server](testing.md#signed-in-manual-review-server) also serves
`/mcp` (manifest `paths.mcp`) over its seeded fixture data, without a provider
key, with a high rate limit for local tooling. It enables OAuth with a fixture
app (`paths.oauth_authorize` opens its consent page) but keeps sign-in optional,
so load scripts can call public tools anonymously. Each fixture account has an API
token in the manifest (`accounts.<name>.api_token`) for the signed-in tools.

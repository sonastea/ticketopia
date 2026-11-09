# Agent tools (MCP)

Ticketopia serves read-only event tools to people's AI agents and editors over the
[Model Context Protocol](https://modelcontextprotocol.io/) at **`/mcp`**. It is
off unless `MCP_ENABLED=true`. The [design](design/app-mcp.md) describes later
steps (signed-in tools, OAuth, load testing); this page covers what exists.

## Tools

| Tool | Behavior |
|---|---|
| `find_events` | Search by city, country, dates, category, keyword, sort and limit, with `cursor` paging. Same validation, defaults, caching and results as Discover and `GET /api/v1/events`. |
| `list_categories` | Categories and their IDs for `find_events` `category_id`, as `GET /api/v1/categories`. |
| `event_details` | One event by ID, as `GET /api/v1/events/{event_id}`, including the durable last-known fallback. |

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
correct or pace their requests. No tool reads or changes account data, and none
needs sign-in.

## Protocol and limits

- Streamable HTTP in **stateless** mode with JSON responses: each `POST /mcp`
  stands alone, so any replica can answer and no session is kept. A
  `tools/call` needs no prior `initialize` request.
- Each trusted client IP may make `MCP_RATE_PER_MINUTE` requests per minute per
  replica (default 60, bursts up to a minute's allowance). Excess requests get
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
  invalid arguments.
- `/mcp` is JSON-RPC, not REST, so it is not part of the
  [OpenAPI contract](openapi.md).

## Connect a client

Enable it locally with `MCP_ENABLED=true` in `.env` and run the app (`air`), then
add the remote server to your client:

```sh
opencode mcp add ticketopia --url http://localhost:8080/mcp
claude mcp add --transport http ticketopia http://localhost:8080/mcp
npx @modelcontextprotocol/inspector   # then connect to http://localhost:8080/mcp
```

The [review server](testing.md#signed-in-manual-review-server) also serves
`/mcp` (manifest `paths.mcp`) over its seeded fixture data, without a provider
key, with a high rate limit for local tooling.

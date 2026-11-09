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

Results keep the API's freshness metadata: list `meta` and, for stored Discover
lists, per-event `meta` with `stale` for last-known events. Errors are tool
errors with the API's wording (for example `limit: must be an integer from 1 to
100`), so agents can correct their request. No tool reads or changes account
data, and none needs sign-in.

## Protocol and limits

- Streamable HTTP in **stateless** mode with JSON responses: each `POST /mcp`
  stands alone, so any replica can answer and no session is kept. A
  `tools/call` needs no prior `initialize` request.
- Each trusted client IP may make `MCP_RATE_PER_MINUTE` requests per minute per
  replica (default 60, bursts up to a minute's allowance). Excess requests get
  **429** with `Retry-After`. Client IPs follow `TRUSTED_PROXY_CIDRS`.
- Browser requests from other origins get **403**; CORS stays disabled.
  Requests above 64 KiB are rejected. Responses are `Cache-Control: no-store`.
- Each call logs its tool, outcome and duration, never its arguments.
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

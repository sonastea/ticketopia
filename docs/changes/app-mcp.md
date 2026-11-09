# Agent tools (MCP) changes

## Unreleased

### 2026-11-09 — Read-only public event tools at /mcp

- Serve `find_events`, `list_categories` and `event_details` over stateless
  Streamable HTTP at `/mcp`, off unless `MCP_ENABLED=true`. Tools reuse
  Discover's query parsing, the shared discovery service and the event-detail
  durable fallback, and report errors in the JSON API's wording.
- Limit each trusted client IP per replica (`MCP_RATE_PER_MINUTE`, default 60,
  429 with `Retry-After`), reject cross-origin browser requests, cap request
  bodies at 64 KiB, mark responses `no-store`, and log calls without arguments.
- Review servers also serve `/mcp` over fixture data. Tests cover the tools, the
  HTTP boundary, a `tools/call` without `initialize`, and stored-Discover
  results with per-event freshness against MariaDB.
- See [agent tools](../app-mcp.md) and the [design](../design/app-mcp.md).

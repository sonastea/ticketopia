# App MCP endpoint

Status: **Steps 1–3 implemented** (public tools behind `MCP_ENABLED`, signed-in
saves and reminders with API-token bearer auth, and a local k6 baseline; see
[agent tools](../app-mcp.md) and [load testing](../load-testing.md)). **Step 4
implemented** as OAuth with Client ID Metadata Documents and read/write scopes
(on by default with accounts, as is requiring sign-in for every request);
Dynamic Client Registration was left out (see the
[feature backlog](../goals/feature-backlog.md)), and connecting real hosted
clients awaits a public deployment. Step 5 remains proposed. It describes an MCP
server for Ticketopia users' own agents. The separate, implemented
[review MCP server](../testing.md#review-fixtures-over-mcp) is a local
development tool over throwaway fixture data and is unaffected by this plan.

## Goal

Let a signed-in user's agent (OpenCode, Claude, Cursor, and similar) find
events, read details, and manage that user's saves and reminders through the
same rules as the web app and `/api/v1`. MCP becomes a third surface beside
HTML and JSON, not a new backend.

Non-goals: moderation, discussions or recommendations in the first release;
any capability the web app lacks; agent access to other users' private data.

## Placement

- Serve **`/mcp`** from the existing Go app with the official Go SDK
  (`github.com/modelcontextprotocol/go-sdk`, already a module dependency) over
  Streamable HTTP. Tools call the same services as the HTML and JSON handlers
  (discovery, saved events, reminders, accounts), never the HTTP API.
- Run the handler in **stateless** mode (`StreamableHTTPOptions.Stateless`), so
  any replica can answer any request without session affinity. Prefer plain
  JSON responses (`JSONResponse`) unless a tool needs streaming.
- Ship behind a deployment setting (for example `MCP_ENABLED`, default off) so
  it can be enabled per environment and rolled back without a deploy.
- Name the server `ticketopia`; `ticketopia-review` remains the development tool.

## Tools

Model user intents, not endpoints, and keep the set small (each tool costs
agent context):

| Tool | Access | Behavior |
|---|---|---|
| `find_events` | public | city/country, dates, category, keyword; same validation, limits and caching as Discover |
| `list_categories` | public | category IDs and names for `find_events` (added in step 1) |
| `event_details` | public | one event with freshness and history summary |
| `my_saved_events` | signed in | the caller's saves, paginated |
| `save_event` / `unsave_event` | signed in | same rules and idempotency as the saved-events API |
| `my_reminders` | signed in | the caller's on-sale reminders and their states |
| `set_reminder` / `stop_reminder` | signed in | same lead limits and readiness rules as the web app |

Responses carry the existing freshness metadata (`meta`, including per-event
last-known labels), so agents can state when data is not live. Private data is
returned only to its owner; public tools never reveal private saves, interest
or reminders. Write tools are idempotent and return the resulting state.

## Authentication

1. **First release: existing API tokens.** Accept `Authorization: Bearer` with
   the account API tokens users already create under **API access** (30-day,
   hashed, revocable; see [accounts](../accounts.md#sessions-and-api-clients)).
   Public tools also work without a token. Clients that support custom headers
   can connect immediately.
2. **Then: OAuth 2.1 as the MCP authorization spec expects.** Publish protected
   resource metadata for `/mcp`, issue tokens through an authorization-code +
   PKCE flow that signs in with the existing Google login, and store them as
   another credential kind with the same limits and revocation. Clients such as
   OpenCode then sign in through their normal OAuth prompt.

Consider separate read and write scopes so a user can grant an agent read-only
access.

## Limits and isolation

Agents call tools in loops, so `/mcp` gets its own protections:

- **Rate limits** per token (and per IP for public tools), separate from page
  and API limits, returning a tool error the agent can act on.
- **A concurrency cap** per replica for tool calls, so agent bursts cannot take
  the database connections web users need. Pool size per replica (`DB_MAX_OPEN`,
  default 10) times replicas must stay below MariaDB's connection limit.
- **Bounded work:** paginated results, size caps, request timeouts, and no tool
  that bypasses the shared discovery cache or Ticketmaster request budget.

## Deployment and scaling

1. **One binary, one deployment (start here).** `/mcp` scales with the app under
   the existing autoscaling. Traffic is I/O-bound like the web app, and the
   stateless handler needs no affinity.
2. **One binary, two deployments (if needed).** If agent traffic behaves
   differently, deploy the same image twice, with one instance serving only
   `/mcp` (route-based), and scale each independently. No second codebase.
3. **Separate service** only for a different runtime (edge), team or security
   boundary. An edge proxy would handle OAuth and rate limiting and forward to
   the origin; it cannot hold MariaDB connections.

Check proxy and ingress timeouts for `/mcp` as for live-activity SSE.

## Observability

Count tool calls, latency and errors per tool and account, separately from page
views. Never log tool arguments containing private data or tokens. These metrics
show when to move from deployment option 1 to option 2.

Implemented as a per-replica `MCP metrics` log (see
[agent tools](../app-mcp.md#protocol-and-limits)): per tool and boundary refusal,
with a signed-in count instead of per-account dimensions, so telemetry carries
no account identity; per-account limits still apply at the boundary.

## Testing and load

**Correctness.** Test tools with the SDK's in-memory transport, as the review
MCP tests do, plus MariaDB integration tests built on
[`internal/reviewfixture`](../testing.md#signed-in-manual-review-server)
for privacy (owner versus other account), validation, idempotency and limits.

**Load: k6, not LLMs.** Load needs high, repeatable request rates with
latency thresholds. k6 scripts can mix page views, `/api/v1` calls and `/mcp`
JSON-RPC POSTs (stateless mode makes each a plain request; no `initialize` is
needed first, as tested), with arrival-rate scenarios and
thresholds such as p95 latency, error rate and database pool saturation.

**Realism: LLM agents as users.** Agents driving the web app (browser tools)
or `/mcp` are slow, costly and nondeterministic, so a handful of them suits
behavior and exploratory testing, not throughput. Use their sessions to find
realistic flows and mixes (search, open, save, set a reminder), then encode
those as weighted k6 scenarios. Agents find what users do; k6 measures what
the system sustains.

**Environment rules:**

- Never drive provider requests with load: the Ticketmaster budget is not
  coordinated across replicas and would be exhausted. Push limits with the
  provider off or a loopback stand-in like `reviewfixture`'s
  `discover-last-known` upstream, on an isolated database and a staging
  deployment shaped like production (replica count, pool sizes).
- Against a dev or production server, run only the gentle, read-only, mostly
  cached remote profile of [load testing](../load-testing.md#remote-servers),
  which ramps, stays under the default `/mcp` limit and aborts itself.
- Follow the existing [live-activity soak](../live-activity-rollout.md) gate
  practice: fixed durations, recorded samples, and pass/fail thresholds.

## Delivery steps

1. Read-only public tools behind `MCP_ENABLED`, with rate limits and metrics.
2. Signed-in tools with API-token bearer auth; privacy and limit tests.
3. k6 baseline for web, API and `/mcp` traffic mixes; agent-session exploration.
4. OAuth flow and read/write scopes (implemented with CIMD; DCR deferred).
5. Revisit deployment option 2 using the metrics.

## Open questions

- Which client applications must work first? Claude Code, VS Code, Claude and
  ChatGPT support CIMD; Cursor documents DCR or a pre-registered client only.
- Rate-limit values per token and per IP, and whether paid or trusted users
  need higher ones.
- Whether `find_events` should default to the account's remembered city when
  signed in, as the web app does.

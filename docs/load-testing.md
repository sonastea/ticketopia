# Load testing

`make load` runs a [k6](https://grafana.com/docs/k6/) traffic mix of page views,
`/api/v1` calls and [agent tools](app-mcp.md) (`/mcp`) against a local review
server, with latency and error thresholds. It is a local baseline for spotting
regressions and limits, not a capacity gate: k6, the app and MariaDB share one
machine. The design is in [app MCP: testing and load](design/app-mcp.md#testing-and-load).

## Running it

```sh
make review-server scenario=discover-last-known   # terminal 1, until Ctrl-C
make load                                         # terminal 2: 1 minute at rate 1
make load duration=5m rate=3                      # longer, three times the arrivals
```

Install k6 first (`brew install k6`, or without installing,
`make load K6="mise exec k6@2.3.0 -- k6"`). The script reads the review server's
`manifest.json` from `REVIEW_SERVER_STATE_DIR` (default
`$TMPDIR/ticketopia-review`); for a server started through the review MCP, set
`REVIEW_SERVER_STATE_DIR=$TMPDIR/ticketopia-review-mcp`. It needs the
`discover-last-known` scenario, whose stored lists and loopback upstream serve
searches without a provider key, and refuses non-loopback targets. Never point
load at production, a shared deployment or real Ticketmaster. The k6 summary is
also exported to `.local/load/traffic-mix-<UTC>.json`.

## Traffic mix

Each scenario has a constant arrival rate (iterations per second, times
`rate`, with VU budgets scaled alike); the agent-tool flows follow an agent session's sequence (search, open
an event, save it, set a reminder):

| Scenario | Rate | Requests per iteration |
|---|---|---|
| `web` | 10/s | Discover page, event page |
| `api` | 10/s | `GET /api/v1/events`, one event, the owner's saved events (API token) |
| `mcp_public` | 10/s | `find_events`, then `event_details` for a result |
| `mcp_signed_in` | 2/s | `my_saved_events` (owner); `save_event`, `set_reminder`, `my_reminders` (other) |

MCP requests are stateless `tools/call` POSTs without `initialize`; a call
passes only with a JSON-RPC result whose structured content has the expected
shape (for example, search results with items). Signed-in writes repeat one save
and reminder, which are idempotent, so concurrent iterations cannot conflict.
Redirects are not followed.

Thresholds: per-scenario p95 under 300 ms and p99 under 1 s, under 1% failed
requests, over 99% passed checks, no tool errors (`mcp_tool_errors`) and no
dropped iterations. `/mcp` refusals are counted by cause over `/mcp` requests:
`mcp_limited_429` (rate limits) and `mcp_busy_503` (the concurrency cap).

Review servers allow 6000 `/mcp` requests a minute per client IP, with bursts up
to a minute's allowance, and 64 concurrent ones. All k6 traffic comes from one
IP, so sustained `/mcp` load above 100 requests a second is eventually limited;
real clients are limited separately. The bucket takes a minute to refill, so
wait a minute between runs, or a run inherits the previous one's limiting.

## Results

### 2026-11-09 — Local baseline (Apple M1, 8 cores, 16 GB)

Review server `discover-last-known` on local MariaDB (pool 10), k6 2.3.0 on the
same machine.

| Run | Requests/s | p95 web / api / mcp public / mcp signed-in | `/mcp` refused |
|---|---|---|---|
| `rate=1`, 1m (default) | 78 | 19 / 11 / 19 / 14 ms | none; all thresholds pass |
| `rate=3`, 30s | 234 | 15 / 8 / 13 / 10 ms | none |
| `rate=5`, 30s | 390 | 19 / 12 / 15 / 13 ms | none (within the burst) |
| `rate=8`, 45s | 624 | 32 / 21 / 23 / 20 ms | none; all thresholds pass |
| `rate=15`, 45s | 1166 | 934 / 615 / 503 / 306 ms | 44% 429, 21% busy 503; 81 dropped iterations |

Latency stays low through `rate=8` (`/mcp` at 224 requests a second, within its
burst). At `rate=15` (about 600 VUs) the shared machine saturates: page and API
p95 rise thirtyfold, `/mcp` exceeds both the per-IP limit and the concurrency
cap, and every failed request is an `/mcp` refusal; web and API requests slowed
but did not fail. These runs are short; sustained `/mcp` load above 100 requests
a second from one IP is limited once the 6000-request burst is spent (after
about 48 seconds at `rate=8`).

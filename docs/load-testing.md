# Load testing

`make load-test` runs a [k6](https://grafana.com/docs/k6/) traffic mix of page
views, `/api/v1` calls and [agent tools](app-mcp.md) (`/mcp`), with latency and
error thresholds. By default it targets a local review server, as a baseline
for spotting regressions and limits rather than a capacity gate: k6, the app
and MariaDB share one machine. With `target=` the same script makes a gentle,
read-only run against a [dev or production server](#remote-servers). The design is in [app MCP: testing and load](design/app-mcp.md#testing-and-load).

## Running it

```sh
make review-server scenario=discover-last-known   # terminal 1, until Ctrl-C
make load-test                                    # terminal 2: 1 minute at rate 1
make load-test duration=5m rate=3                 # longer, three times the arrivals
```

Install k6 first (`brew install k6`, or without installing,
`make load-test K6="mise exec k6@2.3.0 -- k6"`). The script reads the review server's
`manifest.json` from `REVIEW_SERVER_STATE_DIR` (default
`$TMPDIR/ticketopia-review`); for a server started through the review MCP, set
`REVIEW_SERVER_STATE_DIR=$TMPDIR/ticketopia-review-mcp`. It needs the
`discover-last-known` scenario, whose stored lists and loopback upstream serve
searches without a provider key, and refuses a manifest with a non-loopback
target. The k6 summary is also exported to `.local/load/traffic-mix-<UTC>.json`.

## Remote servers

Setting `TARGET` (`target=` with make) aims the script at another server, such
as a dev server or production, from your computer or a VM. Only environment
variables change; nothing in the script does:

```sh
make load-test target=http://localhost:8080             # a dev server, public traffic only
TOKEN=<api token> make load-test target=https://ticketopia.example duration=5m

# On a VM: install k6 and copy scripts/load/traffic-mix.js, then
TARGET=https://ticketopia.example TOKEN=<api token> k6 run traffic-mix.js
```

Remote runs differ from local ones to stay safe on a real deployment:

- **Read-only, mostly cached.** Pages, API and tools all send one search
  (`CITY`/`COUNTRY`, default Chicago, US, with explicit dates for the default
  90 days) and open only up to `EVENTS` (default 5) of its results, so most
  responses come from the discovery cache. Provider requests are not zero:
  the first request for the search and each event, cache refreshes, and
  collection for a scope the server has not seen still use the Ticketmaster
  daily budget, so check it before long runs. No saves, reminders or other
  writes are made, and a signed-in-only run (`SCENARIOS=mcp_signed_in`)
  searches nothing.
- **Signed-in reads only with `TOKEN`**, an [API token](accounts.md#sessions-and-api-clients)
  from the account page; use a dedicated test account. `my_reminders` runs only
  when the server lists it (reminders enabled). Keep the token in the
  environment, not on the command line. It is sent only over HTTPS or to this
  machine, unless `ALLOW_HTTP_TOKEN=1` on a trusted network.
- **Gentle and ramped.** At `RATE=1`, per minute: 30 page, 30 API, 15 public
  tool and 4 signed-in tool iterations, so `/mcp` stays near 40 requests a
  minute, under the default `MCP_RATE_PER_MINUTE` of 60 per IP. Arrivals ramp
  from zero over `RAMP` (default 1m), then hold for `DURATION`. Raising `RATE`
  past about 1.5 measures that limit rather than capacity, unless it is raised
  for the test or load comes from several machines.
- **Stops itself.** The run aborts if over 1% of requests fail, over 1% of
  checks fail (including tool errors, which are HTTP 200), or a scenario's p95
  exceeds `P95` (default 1000 ms), evaluated after 30 seconds.
- **Checks first.** Setup fails before any load when the server is unreachable,
  `/mcp` is not enabled (run `SCENARIOS=web,api` there), the search finds no
  events, or `TOKEN` is rejected.

`TARGET` must be an origin (`https://host[:port]`); redirects are not followed,
so use the canonical scheme and host. Prefer a staging deployment shaped like
production for pushing toward limits; on production, run in quiet hours and
watch the [deployment's](deployment.md) database pool and provider budget.

| Variable | Default | Meaning |
|---|---|---|
| `TARGET` | unset (local review server) | Remote origin to test |
| `TOKEN` | unset | API token for signed-in reads (`ALLOW_HTTP_TOKEN=1` permits plain HTTP) |
| `DURATION` / `RATE` | `1m` / `1` | Hold time and arrival-rate multiplier (`duration=`/`rate=` with make) |
| `RAMP` | `1m` remote, none locally | Ramp-up time |
| `SCENARIOS` | all available | Subset of `web,api,mcp_public,mcp_signed_in` |
| `CITY` / `COUNTRY` / `EVENTS` | Chicago / US / 5 | Remote search and events opened |
| `P95` | 1000 remote, 300 local (ms) | p95 threshold; p99 is three times it |

## Traffic mix

Locally, each scenario has a constant arrival rate (iterations per second,
times `rate`, with VU budgets scaled alike); the agent-tool flows follow an agent session's sequence (search, open
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

Thresholds: per-scenario p95 under 300 ms and p99 under 900 ms, under 1% failed
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

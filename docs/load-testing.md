# Load testing

Two [k6](https://grafana.com/docs/k6/) tests, with different jobs:

| | `make load-test` (traffic load) | `make simulate-users` (simulated users) |
|---|---|---|
| Measures | Throughput and latency of read traffic: page views, `/api/v1` and [agent tools](app-mcp.md) (`/mcp`) | Signed-in people using the site: browsing, saving, interest, recommending, following and discussing |
| Model | Fixed arrival rates (requests keep coming however fast the server answers) | One VU per member, each acting with think time, like a session |
| Accounts | None for public traffic; the fixture's API tokens locally, or your `TOKEN` remotely, for signed-in reads | 40 members with browser sessions, from the review server |
| Writes | Locally, one idempotent save and reminder; remotely never | Yes: saves, posts, follows and more |
| Targets | Local review server (`scenario=discover-last-known`), or a [dev or production server](#remote-servers) with `target=` | Local review server only (`scenario=populated`) |
| Script | `scripts/load/traffic-mix.js` | `scripts/load/user-sessions.js` |

Both need the review server started first, in its own terminal, except a
`target=` run. Locally, k6, the app and MariaDB share one machine, so results
are baselines for spotting regressions and limits, not capacity gates. The
design is in [app MCP: testing and load](design/app-mcp.md#testing-and-load).

## Running the traffic load

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
  machine, unless `ALLOW_HTTP_TOKEN=1` on a trusted network. Servers with
  accounts require sign-in for every `/mcp` call by default, so remote public
  tool calls also carry `TOKEN` when it is set; without it they get 401 there.
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

In `make load-test`, locally, each scenario has a constant arrival rate (iterations per second,
times `rate`, with VU budgets scaled alike); the agent-tool flows follow an agent session's sequence (search, open
an event, save it, set a reminder):

| Scenario | Rate | Requests per iteration |
|---|---|---|
| `web` | 10/s | Discover page, event page |
| `api` | 10/s | `GET /api/v1/events`, one event, the owner's saved events (API token) |
| `mcp_public` | 10/s | `find_events`, then `event_details` for a result |
| `mcp_signed_in` | 2/s | `my_saved_events` (owner); `save_event`, `set_reminder`, `my_reminders` (other) |

Every request asks for compressed responses, as browsers do (k6 decompresses
them); otherwise pages arrive about 20 times larger and remote runs measure
bandwidth. MCP requests are stateless `tools/call` POSTs without `initialize`; a call
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

## Benchmarks

`make benchmark` measures each token-free scenario (`web`, `api`,
`mcp_public`) on its own: it holds the scenario at increasing request rates,
one short k6 run per step at full rate, stops that scenario at its first
failing step, and writes a self-contained HTML report with a chart and a table
per scenario (achieved requests/s, p50/p95/p99/max, failures by status, dropped
iterations). The report and every step's k6 summary and log go to
`.local/load/benchmark-<UTC>/`.

```sh
make benchmark                                      # a running review server
make benchmark target=https://ticketopia.example    # remote, read-only
STEPS=100,200,400 STEP=1m SCENARIOS=api make benchmark target=https://ticketopia.example
```

| Variable | Default | Meaning |
|---|---|---|
| `SCENARIOS` | `web,api,mcp_public` | Scenarios to benchmark, one at a time; join with `+` (for example `web+api+mcp_public`) to run them together, sharing each step's rate evenly |
| `STEPS` | `25,50,100,200,300,400,500,600,800,1000` | Held requests/s per step |
| `STEP` / `PAUSE` | `30s` / `30` | Each step's duration, and seconds between steps |

A step passes under the same thresholds as `make load-test` (remote runs abort a
failing step after about 30 seconds). `RPS`, which the benchmark sets, holds a
`make load-test` scenario at that many requests a second; `RAMP=0` starts at the
full rate. Against a remote server, raise `MCP_RATE_PER_MINUTE` for the window
before benchmarking `mcp_public`, or it measures the per-IP limit; on a review
server, steps above 100 requests a second exceed its limit once the burst is
spent.

### Comparing runs

`node scripts/load/compare.mjs <config.json> [out.html]` puts several benchmark
runs (different times of day, settings or hosts) in one self-contained report:
capacity cards and a bar chart, then per scenario a p95 chart (log scale) and a
step-by-step table for every run, a network baseline, test conditions, and
optional findings and caveats. The config is JSON:

```json
{
  "title": "Production benchmark — three times of day",
  "target": "https://ticketopia.example",
  "date": "10 November 2026",
  "summary": "One-paragraph headline.",
  "host": "Server and resource profile.",
  "client": "Load generator and network.",
  "runs": [
    {
      "label": "Early morning", "when": "4:49 AM", "settings": "Defaults",
      "probe": { "readyz": 90, "discover": null, "note": "time to first byte, ms" },
      "sources": { "web": ".local/load/benchmark-…", "api": ".local/load/benchmark-…" }
    }
  ],
  "findings": ["HTML allowed"],
  "caveats": ["HTML allowed"]
}
```

`sources` maps each scenario (`web`, `api`, `mcp_public`, `web+api+mcp_public`)
to the benchmark directory holding its steps, so one run can combine
directories. A step passes as in the benchmark; `stepSeconds` (default 30) and
`p95` (default 1000 ms) match the benchmark's settings.

## Simulated users

`make simulate-users` simulates signed-in people using the site: browsing,
saving, marking interest, recommending, following venues and threads, and
discussing. It runs only against a local review server with the `populated`
scenario, because it writes; everything it creates is dropped with the
server's schema.

```sh
make review-server scenario=populated           # terminal 1, until Ctrl-C
make simulate-users                             # 20 users, 3s think time, 30s ramp + 2m
make simulate-users users=40 think=0.5 duration=5m
```

Each k6 VU is one member (`users`, at most 40) with its own session.
Like the browser's scripts, it takes the session's CSRF token and sends
same-origin form posts with `Accept: application/json`, pausing `think`
seconds (varied ±50%) between steps. A visit opens Discover (and the batched
saved-state read) and an event, then takes two to five steps:

| Step | Share | Requests |
|---|---|---|
| Save or remove | 20% | `POST /saved/{id}` |
| Interest (public or private) or remove | 12% | `POST /interests/{id}` |
| Recommend with a reason | 8% | `POST /recommendations/{id}` |
| Follow or unfollow the venue | 7% | `POST /follows/venue/{id}` |
| Discuss | 28% | Read the event's threads, then start a thread, reply, mark helpful, or follow the thread |
| Account page | 10% | `/saved`, `/me/notifications`, `/follows` or `/community` |
| Discussions page | 15% | `/events/{id}/discussions` |

Each member keeps under the server's limit of 20 contributions per 10 minutes.
That limit spans runs, so runs within ten minutes of each other may see posts
refused; like a person, the member then stops posting for the window, and
the refusal counts as `posts_limited` rather than a failure. Only the limit's
own response (`rate_limited`) counts; any other refusal fails, and the run
fails if half of all post attempts are refused (`posts_refused`). An action
passes only when its response has the fields the browser reads (for example
`saved` and `event_id` for a save). Other failed actions are logged with the
member, status and message.

Thresholds: p95 under 500 ms for pages and 300 ms for actions and reads,
under 1% failed requests, over 99% passed checks and under half of post
attempts refused. The summary counts each
action (`user_actions`). Review servers run no background workers, so
notification digests and reminder emails are not exercised, nor are the live
activity streams (see the [live-activity soak](live-activity-rollout.md)).

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

### 2026-11-09 — Simulated users (same machine)

Fresh `populated` review server; `posts_limited` appears only after earlier runs.

| Run | Requests/s | Actions/s | p95 page / action / read | Result |
|---|---|---|---|---|
| default: 20 users, 3s think, 2m | 9 | 2.8 | 12 / 24 / 6 ms | all pass |
| 40 users, 0.5s think, 1m | 96 | 30 | 17 / 27 / 9 ms | all pass |
| 40 users, no think time, 1m | 622 | 193 | 132 / 109 / 74 ms | all pass, 0 failed |

Even without think time, 40 users acting continuously stay within the
thresholds. Where the ceiling lies, and whether the app or the shared machine
sets it, was not measured.

### 2026-11-10 — Production on the resource profile

`topia.sonastea.com` behind Cloudflare, on a Ryzen 9 5900X with Coolify and the
[resource profile](deployment.md#resource-profile) (app 1 CPU and 256 MiB,
MariaDB 2 CPUs, Redis 0.5 CPU). Remote mode from a Mac, 20-second ramp then one minute held. A first public-only
check (web, API and public tools) ran before compression was requested; the
step-ups used `SCENARIOS=web,api` with compressed responses.

| Run | Held rate | Run average | p95 web / api | Result |
|---|---|---|---|---|
| `rate=1`, public mix, uncompressed | 2/s | 2/s | 113 / 76 ms (tools 110 ms) | all pass |
| `rate=120` | 240/s | 209/s | 66 / 60 ms | all pass |
| `rate=240` | 480/s | 419/s | 192 / 122 ms | pass; 2 client connection timeouts |
| `rate=400` | ramping to 800/s | 463/s until abort | 3.8 / 3.1 s | aborted: 46% failed |

Latency includes the Cloudflare round trip (about 50 ms). The site held 480
requests a second for a minute and failed while ramping toward 800: requests
queued and timed out at the app, which answered its own 503s (then labelled `provider_unavailable`; since
contract v1.22.0 overload timeouts answer `temporarily_unavailable`). It recovered
within seconds once load stopped. Server resource use was not recorded, so the
exact ceiling between 480 and 800 is unmeasured.


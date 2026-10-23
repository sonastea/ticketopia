# Live activity: performance and rollout record

Part 5 is **in progress**, not a production-readiness or deployment sign-off.
Keep production's explicit `LIVE_ACTIVITY_ENABLED=false` until the outstanding
gates below pass and the coordinated migration/deployment is authorized.

**Latest result — 2026-10-22:** the [P1 rollout gates](#p1-rollout-gates--2026-10-21)
found and fixed calibration and latency defects. Distinct public filters,
personalization and the production-image deployment scenario now pass at 10,000
rich groups. Open: the [latest gate-of-record soak](#p13-encoding-projection-and-gate-rerun--2026-10-22)
still fails the write gate (pooled p95 1.108×; 95% CI 1.076–1.139 crosses 1.10, so
inconclusive, not a pass); browse passes (1.031×). The previous run failed with
statistical support (1.160×; CI 1.105–1.226). Two runs, one per build, do not
show that the change caused the difference. The
[request-level diagnosis](#p13-write-gate-request-level-diagnosis--2026-10-22)
fixed the synchronous membership-notification cost. The remaining shift is mostly
SQL time under concurrent read load on the local VM; it needs validation on
representative Linux infrastructure, and the gate stays failed until then. A local
[ablation smoke run](#ablation-smoke-run-local--2026-10-23) is weak evidence
against projection and count reads as the main cause. The
[Linux runbook](#representative-linux-run) is ready but has not run. The
actual-ingress check (P1.5) has tooling but has not been run, and the cold-start
target needs agreement. Physical-device and screen-reader verification also remain open.

## Operational telemetry

Every enabled replica emits a structured `Live activity metrics` log every
15 seconds while its worker is running. The `activity` object contains cumulative
counters and current/last-completed gauges:

- `streams`, `pending`, `sql_jobs`: current subscriptions, admitted reads and
  activity repository jobs (limits 256, 64 and 2 by default).
- `candidates`, `cohorts`, `owners`, `accounted_bytes`: completed candidate/cache
  accounting, including reserved generation assembly. This is not process RSS;
  collect container memory/CPU and MariaDB metrics alongside it.
- `cache_hits`, `cache_misses`, `sql_statements`: selection reuse and actual attempted
  activity SELECT/DML statements, excluding transaction-control statements.
- `http_429`, `http_503`, `overwritten_updates`, `write_errors`: transport rejection,
  latest-state buffer replacement and write/flush failures. Buffer replacement is
  expected for slow readers; it does not enqueue historical activity.
- `failures`: fixed reasons `candidate_limit`, `memory_limit`, `queue_limit`,
  `stream_limit`, `projection_failed`, `membership_failed`. Reason counters may
  overlap (for example candidate overflow also fails the projection).
- `operations`: projection (SQL plus index assembly), uncached selection/encoding,
  membership repository jobs, counts, location-key reads, cleanup, and delivery.
  Each has cumulative `count`, `seconds` and seven **disjoint** duration buckets:
  ≤1 / ≤10 / ≤50 / ≤250 / ≤1000 / ≤5000 ms and >5000 ms.

The same record carries a `process` object: `goroutines`, `heap_bytes` (live heap
objects) and `sql_open_connections` (the shared application pool). These are
identity-free growth signals for soaks and rollout observation, read without a
debug endpoint.

Use interval deltas, not cumulative averages, for alerting. Gauges update after
assembly or eviction; telemetry never waits for SQL. Metric dimensions are compiled
constants: no account IDs, names, cities, event IDs or filter hashes. There is no
new public metrics endpoint. Ship these logs through the deployment's existing
private log collector. Delivery timing measures server write/flush, **not**
commit-to-browser latency; measure that with workload observations.

Snapshot and stream starts still share 60/minute per trusted client IP; 429 carries
`Retry-After: 60`. Saturated read admission, stream capacity, candidate overflow or
memory failure returns unavailable/503, never an empty or broadened feed. Counts
also share bounded read admission and a five-second service deadline. Joining tabs
reuse fresh counts for existing IDs and batch only newly joining IDs; they do not
reset the normal 15-second refresh cadence.
The entire shared delivery cycle also has one five-second deadline, shared across
count batches and recipient work, so a large union cannot renew its time budget
once per query or connection.
Invalidated inactive accounts retain their pending membership rebuild on re-entry,
even when another account already consumed the coalesced invalidation notification.
Attempts that hit an SQL error back off per account for two seconds after assembly;
duplicate tabs cannot repeatedly retry a failed membership query. Work cut short by the
per-sweep cap or deadline resumes on the next attempt without backoff.
Obsolete private membership revisions count against the 128 unreferenced-cohort
allowance, including when replacements belong to active streams. They cannot
accumulate a fresh set of retired cohorts on every membership change.

Each cached view stores the earliest expiry among its selected groups, so warm
readers check expiry in constant time and reselect in memory at that boundary.
Expiry never makes the whole generation unavailable: with ten-minute contributions
a 10,000-group window expires a group roughly every 60 ms, which previously closed
every stream on the replica between projections. The worker reads the service's
current generation each tick rather than reusing its own last poll, so recovery
by another reader is never overwritten with a stale failure. Subscriptions can
wait up to 100 ms for brief selection contention before falling back to the shared
worker, avoiding an unnecessary one-second tick wait for an already-valid view.
Request cancellation also releases that pending subscription.

Candidate refreshes reuse category/artist/venue facets only when the authoritative
`(event_id, data_as_of)` revision still matches. Eligibility, counts, attribution,
title/location and metadata revisions are read again in the current transaction;
changed facets are batched in that same snapshot. A failed generation discards
reuse state. With unchanged metadata, projection uses two SELECT statements rather
than reloading up to 40 facet batches at 10,000 groups. Real MariaDB regression
checks verify query reuse, name clearing and metadata correction against a full
uncached reference.

## Reproducible local deployment fixture

Use the isolated local MariaDB development service. The opt-in test creates a
random schema/runtime user, runs **the production image** in three containers,
starts nginx, and removes its containers/schema/user and temporary configuration
on completion. It does not use a fake repository or in-process replica stand-ins.
Do not point the test administrator credentials at a shared database.

```sh
docker build -t ticketopia:activity-load .
MARIADB_TEST_ADDR=127.0.0.1:3307 \
  LIVE_ACTIVITY_LOAD_IMAGE=ticketopia:activity-load \
  LIVE_ACTIVITY_LOAD_GROUPS=1000 \
  go test ./internal/persistence -run '^TestMariaDBActivityDeploymentLoad$' \
  -count=1 -timeout=15m -v
```

Repeat with `LIVE_ACTIVITY_LOAD_GROUPS=100` and `10000`. Set
`LIVE_ACTIVITY_LOAD_RICH=true` for long multilingual titles, eight additional
artists and two contributions per group. A failed rich-window run is a failed
capacity gate, not permission to truncate the candidate window or raise limits.
The existing populated-window test separately verifies 10,001-group overflow.
`LIVE_ACTIVITY_LOAD_PROFILE=true` adds per-statement server timing (a temporary
global statement log on the isolated local server, restored on cleanup);
`LIVE_ACTIVITY_LOAD_PHASE=ordinary` stops after the ordinary-traffic gate for
profiling iterations and is never a complete scenario result.
`LIVE_ACTIVITY_LOAD_DISTINCT=true` substitutes 256 distinct private accounts
(one tab per account per replica) for the shared-account phase. Each has its own
stored preferences and credentials; additions/removals use production HTTP writes.

The fixture uses `ticketopia-db_default` / database host `mariadb` by default;
override `LIVE_ACTIVITY_LOAD_NETWORK` and `LIVE_ACTIVITY_LOAD_DB_HOST` for an
equivalent isolated local service with verified TLS. Each app has a 2 GiB memory
limit and one CPU; nginx 1.28 has 128 MiB, 0.5 CPU, 4,096 connections and an 8,192
file-descriptor limit. The proxy listener is loopback-only. Its synthetic
`X-Load-IP` policy is **test-only**, allowing distinct/shared IP workloads without
disabling the app limiter; production must derive trusted addresses from clients.
Public starts are bounded to 24 concurrent handshakes; the private phase opens
768 tabs sharing one account, with distinct synthetic client IPs. The test records
container samples, publication observations, membership propagation and replica
operational logs.

The scenario exercises 768 live streams, rejection at a full replica, 60/61
shared-IP starts, actual proxy heartbeat flushing, five publication/withdrawal
rounds, no-live versus loaded ordinary browse/write p95, cross-process private-interest
addition/removal, local invalidation with quiet reconnect, SQL permission
failure/recovery in its isolated schema, and
SIGTERM drain while another replica remains available. These are server/proxy
observations, not browser paint timings or a physical-device claim.

The P1 gates are separate opt-in tests on the same isolated local MariaDB service:

```sh
# P1.1 distinct public filters and P1.2 personalization (three in-process replicas)
MARIADB_TEST_ADDR=127.0.0.1:3307 LIVE_ACTIVITY_CAPACITY_GROUPS=100,1000,4000,10000 \
  LIVE_ACTIVITY_PERSONAL_GROUPS=1000,4000 go test ./internal/persistence \
  -run '^TestMariaDBActivity(PublicFilter|Personalization)Capacity$' -count=1 -timeout=60m -v
# P1.3 soak on the deployment fixture (at least 30m for a gate result)
MARIADB_TEST_ADDR=127.0.0.1:3307 LIVE_ACTIVITY_LOAD_IMAGE=ticketopia:activity-load \
  LIVE_ACTIVITY_SOAK=40m go test ./internal/persistence \
  -run '^TestMariaDBActivityDeploymentSoak$' -count=1 -timeout=150m -v
# P1.4 browser-visible latency (Chromium through Playwright)
MARIADB_TEST_ADDR=127.0.0.1:3307 PLAYWRIGHT_MODULE=/path/to/node_modules/playwright \
  LIVE_ACTIVITY_LATENCY_BROWSER_SCRIPT=../../scripts/live-activity-latency-browser.cjs \
  go test ./internal/persistence -run '^TestMariaDBLiveActivityLatencyBrowser$' -count=1 -v
# P1.5 actual ingress: sends real traffic; only an operated staging environment
LIVE_ACTIVITY_INGRESS_URL=https://staging.example LIVE_ACTIVITY_INGRESS_REPLICAS=3 \
  LIVE_ACTIVITY_INGRESS_DRAIN_WAIT=5m go test ./internal/persistence \
  -run '^TestLiveActivityIngress$' -count=1 -v
# Projection plans (ANALYZE FORMAT=JSON), current form vs diagnostic alternatives
MARIADB_TEST_ADDR=127.0.0.1:3307 LIVE_ACTIVITY_PLAN_GROUPS=1000,10000 \
  LIVE_ACTIVITY_PLAN_DIR=.local/plans go test ./internal/persistence \
  -run '^TestMariaDBActivityProjectionPlans$' -count=1 -timeout=40m -v
```

The plans test checks that each alternative returns identical rows, then records
five warm samples per form. `LIVE_ACTIVITY_PLAN_SHAPES` selects `history` (16
contributors, two contributions per group, plus 4,096 other accounts with 32,768
past recommendations) and/or `soak` (8 contributors and one contribution per
group; past recommendations are four times the window). Plans depend on shape.
Without the history, the optimizer chose different join orders.

**Ablation diagnostic.** `LIVE_ACTIVITY_ABLATION=true` measures which live-activity
SQL causes the write shift. It removes one source at a time from full load, rather
than running each source alone. All three replicas then run the same source built
with `-tags activityablation` (combinable with `LIVE_ACTIVITY_LOAD_TRACE`). Release
images contain none of it. Before each period, the harness writes a mounted mode
file and waits for every replica to log the new mode:

- `full`: all reads reach MariaDB, and the latest ~30 candidate generations and
  counts are recorded.
- `no-projection`: candidate reads replay recorded generations in order. Each is
  shifted so its age and remaining lifetime match the recording, with revisions
  unchanged. Generation turnover, selection, encoding and frames continue.
- `no-counts`: count reads, including fallback count GETs, return the last
  recorded counts.
- `replayed`: both.

Membership reads, cleanup, location keys and request-path SQL (credentials,
preferences) still reach MariaDB in every mode. Replayed content does not show new
publications, while the writer keeps writing them.

The ordinary phase becomes a fixed 32-period order (`soakAblationOrder`): four
rounds that pair full load with each arm (no-live, no-projection, no-counts,
replayed). Arm order rotates by round, and each arm's pairs alternate AB, BA, AB,
BA. Each arm reports loaded/arm p95 with the same paired bootstrap, plus
per-request component differences. A run is a diagnostic, never a gate result.
With four pairs per arm, an effect smaller than about 0.15 ms at p95 cannot be
told apart from noise, so read the means and p50 differences alongside. Allow
about one hour for the ablation phase after the sustained soak.

#### Representative Linux run

Run these on one Linux host (amd64 or arm64) with Docker Engine, Docker Compose,
Go 1.27+, Make and OpenSSL. The fixture expects the repository's local MariaDB:
Compose project `ticketopia-db`, container `ticketopia-db-mariadb-1`, network
`ticketopia-db_default`, address `127.0.0.1:3307`. The database shares the host
with the replicas but not a VM. Separate database hardware is a different
environment, and the statement profile only supports the local container. Run one
test at a time and keep the host otherwise idle. Report results, absolute
latencies, host CPU/storage and kernel alongside the ratios. A Linux pass shows
performance on that host, not the cause of the local failure.

```sh
make db-setup
docker build -t ticketopia:activity-load .
export MARIADB_TEST_ADDR=127.0.0.1:3307 LIVE_ACTIVITY_LOAD_IMAGE=ticketopia:activity-load
# 1. Attribution (diagnostic): ablation with trace and statement log, ~1h40m.
LIVE_ACTIVITY_SOAK=15m LIVE_ACTIVITY_ABLATION=true LIVE_ACTIVITY_LOAD_TRACE=true \
  LIVE_ACTIVITY_LOAD_PROFILE=true LIVE_ACTIVITY_SOAK_SAMPLES=$PWD/.local/linux-ablation.csv \
  go test ./internal/persistence -run '^TestMariaDBActivityDeploymentSoak$' \
  -count=1 -timeout=180m -v 2>&1 | tee .local/linux-ablation.log
# 2. Gate of record: unchanged and uninstrumented, run once, ~70m.
LIVE_ACTIVITY_SOAK=40m LIVE_ACTIVITY_SOAK_SAMPLES=$PWD/.local/linux-gate.csv \
  go test ./internal/persistence -run '^TestMariaDBActivityDeploymentSoak$' \
  -count=1 -timeout=150m -v 2>&1 | tee .local/linux-gate.log
```

Return both logs, the CSVs and their `-events.csv`/`-requests.csv` companions, plus
`uname -a`, `nproc`, `free -g`, `docker info` and the storage type. Traces are large
and optional.

`LIVE_ACTIVITY_CAPACITY_PROFILE=typical` selects 1–3 artist lineups, and
`LIVE_ACTIVITY_PERSONAL_BURST` (default 64, at most 256) sets how many accounts change
membership at once. The soak keeps
1,000 groups (`LIVE_ACTIVITY_LOAD_GROUPS`, at most 9,000) under continuous replacement,
excludes a five-minute warmup from growth checks, and measures the ordinary-traffic
gate in an interleaved ABBA×4 phase after the sustained soak (methodology in the
[soak result](#p13-40-minute-soak-with-interleaved-gate--2026-10-21)). Raw samples go
to `LIVE_ACTIVITY_SOAK_SAMPLES` (default `.local/activity-soak-<UTC>.csv`); rerun the
analysis offline with `LIVE_ACTIVITY_SOAK_ANALYZE=<csv> go test ./internal/persistence
-run '^TestActivitySoakSampleAnalysis$' -v`. A companion `<csv>-events.csv` holds
r0 count-frame arrivals and 5 s CPU rates for the Docker VM, the test process and,
on Colima, the host QEMU/Lima processes. Samples are saved even when the test
fails. Allow `-timeout=150m` for a 40-minute soak.

Request-level diagnostics are portable (Docker and the Go toolchain only).
nginx logs only the in-network probe's requests, with `$request_time` and
`$upstream_{connect,header,response}_time`, so the log costs the same in both
states. Each interleaved probe request is joined to its log line in
`<csv>-requests.csv`. `LIVE_ACTIVITY_LOAD_TRACE=true` runs r0 as the same source
built with `-tags activitytrace`; release builds have no trace control. That build
records a Go execution trace (`<csv>-trace-<period>.trace`) in pairs 2, 3, 6 and 7,
covering both pair orders, and leaves the other pairs to measure tracing overhead.
Production code marks each request as a trace task and marks the SQL transaction,
commit and synchronous membership notification as regions. These cost one atomic
check unless a trace is recording. After sampling, `go tool trace -d=parsed`
splits each matched handler into running, runnable (scheduler latency), network
(SQL round trips), mutex and GC time. Combine it with `LIVE_ACTIVITY_LOAD_PROFILE=true`
for server-side statement and commit times. Instrumented runs are diagnostics, not
gate results. The
latency fixture allows 600 starts per minute because every journey shares one
loopback address; production keeps 60.

## Results and outstanding gates

### Ablation smoke run, local — 2026-10-23

**Purpose: check the [ablation harness](#reproducible-local-deployment-fixture), not
a result to act on alone.** This was a 5-minute sustained phase followed by all 32
ablation periods, on the same M1/Colima environment. Every mode switch was
confirmed by all three replicas. All periods held 700–704 open streams (none in
no-live periods) and had zero probe errors. Count cadence and fallback assertions
passed. The run failed only the growth checks: a 5-minute soak leaves no samples
after their 5-minute warm-up. The 15-minute Linux run avoids this.

| Write p95, four pairs each | loaded/arm | 95% CI (pairs + block) |
| --- | --- | --- |
| no-live | 1.102 | 1.036–1.168 |
| no-projection | 0.980 | 0.898–1.041 |
| no-counts | 0.980 | 0.910–1.046 |
| replayed (both) | 0.953 | 0.896–1.025 |

Removing projection and count SQL did not reduce write p95, while stopping live
clients did. This is weak evidence against those reads causing most of the write
shift: one short local run, four pairs per arm, wide intervals. Replay itself may
add work (for example, a changed generation every two seconds). What remains with
live clients connected includes membership SQL, request-path SQL from stream
starts and rotations, connection and proxy load, and delivery CPU/GC. The
[Linux runbook](#representative-linux-run) repeats this with tracing and a
15-minute sustained phase. Raw data (git-ignored):
`.local/activity-soak-20261022-ablation-smoke*`.

### P1.3 encoding, projection and gate rerun — 2026-10-22

**Result: the write gate remains failed (pooled p95 1.108×; 95% CI 1.076–1.139
crosses 1.10, so inconclusive, not a pass).** Browse passes at 1.031× (0.979–1.042,
below the limit). Every other soak assertion passed. This is the single gate-of-record
run for this build. Its method, seed, threshold and order are unchanged.

**HTTP count caching: deferred.** A count GET that starts after a committed write
must reflect it on any replica. A shared TTL cache cannot guarantee that. A
cross-replica check would need a committed per-event count revision, which means a
schema and write-path change. The check would also cost a SQL round trip, about as
much as the index-only counts it replaces (0.4–0.6 ms server time per 101-ID batch
in the profile). `/api/v1/events/activity-counts` therefore reads MariaDB on every
request. SSE joining-count reuse, the 15-second cadence, failure behavior and
membership revision checks are unchanged.

**Changes:**

- *Encoding (benchmark-supported).* Every two-second generation re-encoded each
  warm view, and every SSE snapshot frame called `json.Marshal` on a view that
  already had cached `MarshalJSON` bytes. `encoding/json` re-validates and copies
  those bytes. The view encoding is now spliced from its cached items, and frames
  write the shared bytes directly. The output is byte-identical (tested with HTML
  escaping and non-ASCII). On Apple M1 with a rich 20-item view
  (`BenchmarkViewGenerationEncode`, `BenchmarkStreamPayloads`): per-generation
  re-encode fell from 28.1 µs, 12.4 KB and 3 allocations to 1.1 µs, 12.3 KB and 2
  allocations. A snapshot frame fell from 28.2 µs, 12.6 KB and 3 allocations to
  zero. Count payloads (12.6 µs and 9.5 KB per stream every 15 s) were left alone.
- *Projection (EXPLAIN-supported).* The grouped candidate read joined `accounts`
  for each contribution. That only re-proved existence: contributions reference
  `event_recommendations`, which references `accounts`, and accounts are never
  deleted. Removing the join returns identical rows in every shape. Each plan
  keeps the same join order and drops one `eq_ref` per contribution. Median
  `r_total_time_ms` with the history shape: 6.63 → 5.64 ms at 1,000 groups and
  88.1 → 80.2 ms at 10,000. With the soak shape at 1,000 groups: 5.05 → 5.05 ms,
  no measurable difference. No index or migration was added.

**Fresh-image diagnostic** (instrumented, not a gate result). Image
`sha256:8bf9273a…`, run with `LIVE_ACTIVITY_LOAD_TRACE=true` and
`LIVE_ACTIVITY_LOAD_PROFILE=true`, a 15-minute sustained phase, then ABBA×4. Write
1.105× (CI 1.069–1.153); browse 1.033×. Loaded-minus-no-live write means, compared
with the previous after-fix diagnostic: client +0.25 ms (+0.34), traced handler
network wait +0.10 (+0.19), runnable +0.02 (+0.03), and overlap with GC mark +0.06
(+0.08). r0 runtime per loaded period: Go CPU
0.056–0.058 cores (0.058–0.063 before), GC 1.47–1.64/s (1.46–1.66/s before),
scheduling-latency p99 178–218 µs (171–319 µs before). One unexplained result: the
statement log showed the projection's loaded-period p50 at 5.6–7.1 ms, against
4.4–4.7 ms in the previous diagnostic, with the same rows examined. The isolated
plans do not explain it. It may be run-to-run VM variation or a plan chosen under
in-soak statistics; this evidence cannot separate the two.

**Gate-of-record soak (40 min).** Image `sha256:d5b71d42…`, built from the working tree;
environment unchanged. 704 SSE tabs and 64 fallback clients. 11,902 SSE connections
(5,566 rotation/drop reconnects). 26,690 fallback requests with no failures, no
`unavailable` frames and no refused starts. Count cadence: 62,464 intervals of
14.97–16.08 s; none early or late. No growth: goroutines 404–532, heap 12–15 MiB,
memory 30–36 MiB, two SQL connections per replica, seven database sessions,
cohorts ≤160.

| Pair | 1 | 2 | 3 | 4 | 5 | 6 | 7 | 8 |
| --- | --- | --- | --- | --- | --- | --- | --- | --- |
| Write ratio | 1.090 | 1.159 | 1.064 | 1.090 | 1.102 | 1.117 | 1.126 | 1.104 |
| Browse ratio | 0.971 | 1.074 | 1.004 | 1.027 | 0.995 | 1.050 | 0.995 | 1.044 |

| Pooled (2,400 per state) | Loaded p95 | No-live p95 | Ratio | Gate ≤1.10 | 95% CI (8 pairs + block) |
| --- | --- | --- | --- | --- | --- |
| Browse | 51.00 ms | 49.49 ms | 1.031 | pass | 0.979–1.042 (below limit) |
| Write | 6.288 ms | 5.674 ms | 1.108 | **fail** | 1.076–1.139 (crosses limit) |

Write p50 is 4.29 ms loaded and 3.79 ms no-live (+0.50 ms). Mean CPU: VM busy 0.92
vs 0.61 cores, host QEMU 1.00 vs 0.68.

**Interpretation.** Two runs, one per build, cannot attribute the change from 1.160
to 1.108 to this work. The remaining write shift is still mostly SQL round-trip
time on the shared local VM. Next step unchanged: the instrumented soak on
representative Linux infrastructure, which is not run without authorization.

Raw data (git-ignored): `.local/activity-soak-20261022-diag2*` (diagnostic, with
traces) and `.local/activity-soak-20261022-gate2*` (gate; samples SHA-256
`3cf915b7…7bddd70`). Plans: `.local/plans-20261022/`.

### P1.3 write-gate request-level diagnosis — 2026-10-22

**Result: the write gate remains failed on this fixture, and the 1.10 threshold
is unchanged.** The membership-notification path was a measured application
contributor and is fixed. Most of the remaining shift is in SQL round trips and
r0 scheduling, not in a single application hot spot.

Method: two instrumented runs, which are diagnostics and not gate results. Each had
a 15-minute sustained phase followed by the standard ABBA×4 interleaved phase, with
`LIVE_ACTIVITY_LOAD_TRACE=true` and `LIVE_ACTIVITY_LOAD_PROFILE=true`
([tooling](#reproducible-local-deployment-fixture)). Each of the 2,400 probe writes per state is joined to
its nginx log line. In traced pairs (1,200 per state), all 600 requests per period
matched an r0 trace task (none ambiguous). Overhead: traced and untraced pairs gave
write p95 ratios of 1.131 and 1.149 (before the fix) and 1.127 and 1.133 (after),
so tracing does not inflate the shift. The statement log records every statement,
so it costs more in loaded periods; its server times are compared only by state.
Images: `sha256:719e6021…` (before) and `sha256:4ad51332…` (after), from the
working tree; r0 runs the same source built with `-tags activitytrace`.

Traced writes, loaded minus no-live, before the fix (ms). "Tail" is the slowest
10% in each state.

| Component | Δmean | Δtail |
| --- | --- | --- |
| Client total | +0.35 | +1.34 |
| Probe↔nginx, nginx itself, upstream connect | +0.01 | −0.01 |
| nginx↔r0 outside r0's Go process | +0.04 | +0.17 |
| r0 accept/read before the handler; response flush | +0.00 | +0.02 |
| Handler: waiting on network (SQL round trips) | +0.17 | +0.66 |
| …of which in the write transaction / its commit | +0.13 / +0.06 | +0.45 / +0.09 |
| Handler: runnable (waiting for one of r0's 2 Ps) | +0.04 | +0.22 |
| Handler: running / in syscalls | +0.06 / +0.02 | +0.13 / +0.10 |
| Membership notification (inside the handler rows above) | +0.03 | +0.13 |

MariaDB's own times (statement log, mean of eight periods per state, both runs)
rise when loaded. Write-transaction p50 is 0.95–0.96 vs 0.88–0.89 ms and p95 is
1.73–1.76 vs 1.46–1.48 ms. COMMIT p95 is 1.22 vs 1.04–1.07 ms. The writes are the
same in both states, so the increase comes from the read-only live load sharing
the server, the VM and its storage. Its CPU stays well below one core (0.31–0.41
vs 0.26–0.37 cores) and there were no lock waits.

In r0, runnable waits longer than 100 µs total 30–49 ms per loaded period against
7–8 ms no-live. About 75% of that time overlaps the GC background mark worker, and
most of the rest overlaps live-delivery work (the delivery loop, cache-miss
selection and JSON encoding). r0 used 0.07–0.08 cores of Go CPU, so this is
short-lived contention for GOMAXPROCS=2, not saturation.

**Application fix: synchronous membership notification.** An interest write
notified the activity service on the request path. It waited for the activity
mutex and scanned every subscription, while delivery held that mutex for
O(subscriptions) loops (each tick's visible-ID collection and every cache-miss
selection, which marshalled a JSON key per subscription). Private subscriptions are
now indexed by owner, so a notification and the membership sweep touch only that
owner's streams. Delivery copies subscriber pointers under the mutex and derives
IDs and keys outside it, and each subscription caches its public key (its filter
is immutable). After the fix, loaded notification time was 0.003 ms mean, the
same as no-live, with no mutex wait. Handler runnable p95 fell from 0.42 to
0.20 ms, loaded GC cycles from 2.1–2.3/s to 1.5–1.7/s, and r0 Go CPU from about
0.075 to 0.061 cores.

| Write p95 ratio (2,400 per state) | Pooled | 95% CI (8 pairs + block) |
| --- | --- | --- |
| Before fix, instrumented | 1.142 | 1.098–1.196 |
| After fix, instrumented | 1.137 | 1.096–1.185 |

Browse passed in both (1.026 and 1.032). The overall change is within run-to-run
variation: the notification was about a tenth of the shift. After the fix, the
traced loaded mean is still +0.33 ms, of which +0.19 ms is SQL network wait.

**Interpretation and next step.** The largest remaining component is SQL time
under concurrent read load, measured both at the client and in MariaDB, including
COMMIT. Smaller parts are runnable time on r0's two Ps (GC marking and delivery
work) and the nginx↔r0 hop. This evidence points to shared database, VM scheduling
and storage capacity on one 4-CPU local VM. It does not identify another
demonstrated application bottleneck, and it does not show that platform effects
explain the whole shift. Next: run the same instrumented soak on representative
Linux infrastructure, with production-like separation of the database, replicas
and storage. Reconsider the gate only after that, and only against an explicit
performance requirement. Until then the write gate stays failed.

Raw data (git-ignored, not committed): `.local/activity-soak-20261022-diag*` and
`.local/activity-soak-20261022-diagfix*` (samples, events, `-requests.csv` and
eight `.trace` files per run, plus run logs).

### P1.3 soak rerun, ABBA×4 with CPU diagnostics — 2026-10-22

**Result: write fails the gate with statistical support. The pooled p95 ratio is
1.160 and its 95% interval (1.105–1.226) lies entirely above 1.10.** Browse passes
at 1.048× (0.991–1.099, below the limit). Every other soak assertion passed. The
order (ABBA×4), settle, probe, block length, seed and gate were fixed before the run,
and it was not repeated.

Image: the same linux/arm64 manifest as the 2026-10-21 run,
`sha256:8b45a4d570b91ff21d25f1e2b88915cb90a37dda0283d3bd576b542d0c32f995`; it was
rebuilt from the working tree after `8bb7369`, with test-only changes (index
`sha256:49485dda34b8bcbeac4917393b9d2d4c165fc6dc8504aed9c09406109a3d48b6`).
Environment: unchanged (Apple M1, Colima arm64 VM with 4 CPUs and 8.3 GB, MariaDB 12.3.3).

**Sustained soak (40 min).** 704 SSE tabs and, separately, 64 fallback polling
clients; in the soak, 4,015 publications, 1,204 membership writes and 3,840 rotations.
Over the whole run there were 9 client epochs and 11,892 SSE connections: 704 per
epoch plus 5,556 reconnects, of which 4,912 were rotations (the rest are not
classified). 26,692 fallback requests had no failures; no refused starts or
`unavailable` frames. Count cadence: 63,275 intervals, 14.95–16.10 s; none early
or late. No growth: goroutines about 532/531/404, heap 12–16 MiB, memory 30–37 MiB,
SQL connections 2 per replica, database sessions 7, proxy about 698, cohorts ≤160,
owners 16. In-soak loaded probes: browse 15.1 → 34.3 ms with data growth, write
6.1–7.4 ms.

**Interleaved phase.** Same method as the
[2026-10-21 run](#p13-40-minute-soak-with-interleaved-gate--2026-10-21) with 16 periods
(eight loaded/no-live pairs). Readiness took 11–15 s (loaded) and 8–15 s (no-live),
followed by the fixed 60 s settle. At sampling start, loaded periods had 701–704/704
tabs open and 689–692 proxy connections; no-live periods had 0 tabs and 2 proxy
connections. All 16 periods had 300 browse + 300 write samples and 0 errors.

| Pair (periods) | Loaded write p95 | No-live write p95 | Write ratio | Browse ratio |
| --- | --- | --- | --- | --- |
| 1 (1/2) | 6.593 ms | 5.763 ms | 1.144 | 0.973 |
| 2 (4/3) | 6.480 ms | 5.857 ms | 1.106 | 1.051 |
| 3 (5/6) | 6.203 ms | 5.774 ms | 1.074 | 1.004 |
| 4 (8/7) | 6.625 ms | 5.710 ms | 1.160 | 1.073 |
| 5 (9/10) | 6.425 ms | 5.830 ms | 1.102 | 1.008 |
| 6 (12/11) | 6.683 ms | 5.690 ms | 1.175 | 1.068 |
| 7 (13/14) | 6.057 ms | 5.743 ms | 1.055 | 1.017 |
| 8 (16/15) | 7.589 ms | 5.908 ms | 1.285 | 1.179 |

Browse p95 per period rose 34.1 → 60.9 ms with data growth, and the pairs absorbed
that drift. Pair 8 is the largest for both operations; it was kept, as planned.

| Pooled (2,400 per state) | Loaded p95 | No-live p95 | Ratio | Gate ≤1.10 | 95% CI (8 pairs + block) | Block-only CI |
| --- | --- | --- | --- | --- | --- | --- |
| Browse | 51.62 ms | 49.26 ms | 1.048 | pass | 0.991–1.099 (below limit) | 1.034–1.070 |
| Write | 6.701 ms | 5.777 ms | 1.160 | **fail** | 1.105–1.226 (**above limit**) | 1.129–1.198 |

Write percentiles, loaded vs no-live: p50 4.56 vs 3.89 ms (+0.67), p90 +0.47, p95
+0.92 and p99 +1.97 ms. The whole distribution shifts. The bootstrap limitations
above still apply, but eight pairs give the outer stage 6,435 distinct multisets
instead of 35.

**Diagnostics (5 s samples during probe sampling, mean cores).**

| Source | Loaded | No-live |
| --- | --- | --- |
| Docker VM busy (of 4) | 0.97 | 0.62 |
| Host QEMU process | 1.04 | 0.69 |
| Host Lima SSH mux (port forwarding) | 0.10 | 0.01 |
| Host `limactl` | 0.04 | 0.03 |
| Test process (load generator) | 0.02 | 0.01 |
| r0 container (1-CPU quota; 0 throttled periods) | 0.084 | 0.047 |
| MariaDB | 0.46 | 0.43 |
| nginx | 0.010 | 0.008 |

The VM is about a quarter busy when loaded, and nothing is throttled. Host port
forwarding costs about 0.1 core. These 5 s means are evidence against sustained
saturation of the VM or of r0's quota. They cannot exclude CPU contention shorter
than a sample, or effects on scheduling latency.

Burst check against **recorded** r0 delivery ticks (194 ticks, median spacing
15.9 s): loaded slow writes (above the no-live p95; 14.8% of loaded writes) and
medians are flat across the 15 s after a tick. The first 3 s held 22% of slow writes,
where an even spread gives 20%. Browse is also flat. This is evidence against
delivery-aligned bursts as the main cause. Caveat: probe times use the VM clock
and ticks the host clock, so a small offset would blur only sub-second alignment.

**Remaining failure.** Writes slow by about 0.7–0.9 ms at p50–p95 when live
clients are connected. The data above is evidence against delivery-aligned bursts
and sustained CPU saturation. It does not locate the time, which the
[request-level diagnosis](#p13-write-gate-request-level-diagnosis--2026-10-22) follows.

Raw samples: `.local/activity-soak-20261022.csv` (11,800 rows, SHA-256
`2d83b7d4…3bfc692a`) and `.local/activity-soak-20261022-events.csv` (4,315 rows,
SHA-256 `e600dc2b…4013da`), git-ignored and not committed.

### P1.3 40-minute soak with interleaved gate — 2026-10-21

**Result: the observed write gate fails (1.135× > 1.10); its 95% interval
(1.076–1.193) crosses 1.10, so the evidence is inconclusive, not a pass.** Browse
passes (1.031×; interval 0.962–1.057). Every other soak assertion passed. This is one
predetermined run; it was not repeated.

Image: built from the working tree at `556ad66` plus measurement-only test changes,
index `sha256:e5d7204da1dddce29952211dece58cd700c2ae5e38ee6dea23c48f8ca13cc271`
(linux/arm64 manifest `sha256:8b45a4d570b91ff21d25f1e2b88915cb90a37dda0283d3bd576b542d0c32f995`).
Environment: Apple M1, Colima (Lima/QEMU) arm64 VM running Docker 29.1.4 (4 CPUs,
8.3 GB), MariaDB 12.3.3 without cgroup limits; the fixture as described above. This
run used ABBA×2; the harness has since moved to ABBA×4.

**Sustained soak (40 min, continuous).** 1,000 rich groups with natural ten-minute
expiry; 4,015 publications and 1,204 membership writes. **704 SSE tabs** (256/256/192)
and, separately, **64 fifteen-second fallback polling clients** (short requests, not
SSE connections); 101/20-ID count subscriptions; 3,840 filter/account/ID rotations.
Across the run, 8,390 SSE connections were established: 704 in each of 5 client
epochs plus 4,870 reconnects (4,352 rotations; the rest, such as server-closed private
streams, are not classified by cause). 23,566 fallback requests had no failures;
no refused starts or `unavailable` frames.

- Count cadence: 56,172 intervals, 14.95–16.11 s; none early or late.
- No growth after warmup: goroutines about 532/532/404, live heap 12–15 MiB, container
  memory 30–40 MiB, SQL connections 2 per replica, database sessions 7, proxy
  connections about 698, cached cohorts ≤152 and owners 16. Resource bounds held.
- In-soak loaded probes: browse p95 grew 14.9 → 36.1 ms with recommendation volume
  (the known `/community` scaling finding below); write p95 stayed 6.0–7.7 ms.

**Interleaved measurement (methodology).** After the soak, eight periods ran in the
predetermined order loaded, no-live, no-live, loaded ×2 (ABBA ABBA). Before every
period the harness stopped all live clients (SSE, fallback, rotation), closed idle
keep-alives and verified teardown: every replica logged a metrics record with zero
streams after the stop, the proxy had ≤8 active connections and no tab was open.
Loaded periods then started a fresh client epoch and waited for all 704 tabs to
reconnect. Readiness took 13–17 s (loaded) and 10–15 s (no-live), followed by an
identical fixed 60 s settle. Publication, membership writes and resource sampling
ran unchanged in both states. Each period ran the same in-network probe container
(same route through nginx to r0, alternating `/community` GET and interest PUT/DELETE,
50 ms between iterations, 5 discarded warmup iterations, 300 measured pairs, about
28 s). At sampling start loaded periods had 704/704 tabs open and 689–694 proxy
connections; no-live periods had 0 tabs, 2 proxy connections and zero fallback or
rotation activity during the sample.

| Period | State | Browse p95 | Write p95 |
| --- | --- | --- | --- |
| 1 | loaded | 34.18 ms | 6.214 ms |
| 2 | no-live | 35.25 ms | 5.718 ms |
| 3 | no-live | 36.35 ms | 5.866 ms |
| 4 | loaded | 39.67 ms | 6.882 ms |
| 5 | loaded | 39.13 ms | 6.561 ms |
| 6 | no-live | 40.30 ms | 5.564 ms |
| 7 | no-live | 41.37 ms | 5.713 ms |
| 8 | loaded | 42.60 ms | 6.449 ms |

Each period: 300 browse and 300 write samples, 0 errors (2,400 + 2,400, no errors in
total). Adjacent-pair ratios (1/2, 4/3, 5/6, 8/7): browse 0.970, 1.091, 0.971,
1.030; write 1.087, 1.173, 1.179, 1.129.

| Pooled (1,200 samples per state) | Loaded p95 | No-live p95 | Ratio | Gate ≤1.10 | 95% CI (paired-period + block) | Block-only CI |
| --- | --- | --- | --- | --- | --- | --- |
| Browse | 41.57 ms | 40.33 ms | 1.031 | pass | 0.962–1.057 (below limit) | 1.021–1.040 |
| Write | 6.485 ms | 5.713 ms | 1.135 | **fail** | 1.076–1.193 (**inconclusive**) | 1.101–1.175 |

**Bootstrap.** Percentile intervals over 10,000 replicates with seed 20261021, so
they are reproducible from the samples. The outer resampling unit is an adjacent
loaded/no-live **pair** drawn with replacement; each replicate keeps equal loaded and
no-live periods, and within-pair drift is left intact. Within each drawn period, the
inner unit is a circular **block of 20 consecutive requests** of that operation
(about 2 s). Individual requests are never resampled independently, because
neighbouring requests share load, GC and cache state. The block-only interval keeps
periods fixed, so it shows only within-period sampling noise. Limitations: four pairs
give the outer stage only 35 distinct multisets, so the interval is coarse and its
nominal 95% coverage is not assured. The block length was chosen in advance, not
tuned. Percentile bootstraps of a p95 are biased at small sizes. One run on one
local VM does not represent production. Loaded periods also put load-generator
connection handling (Colima/Lima host port forwarding and nginx) on the same 4-CPU VM.

Interpretation: the write slowdown is consistent in direction (all four pairs >1.0;
the interval excludes 1.0) and is about 0.5–1 ms at p95. The data cannot show whether
it exceeds 10%. Ordering does not explain the previous 1.113×: interleaving moved
the estimate to 1.135×. The probe's profile showed no CPU-quota throttling in either
state (r0 0.05 → 0.08–0.09 cores; MariaDB 0.38–0.42 → 0.40–0.45 cores).

Exploratory follow-up (after the run, with a decision rule fixed before looking; not a
gate result): 15.2% of loaded writes exceeded the no-live p95, against 5.0% of
no-live writes. Arranged by position within a 15 s cycle (count delivery) or 10 s
cycle (rotation), in 1 s slots, the loaded excess was spread across every slot rather
than concentrated. The busiest 3 s window held 24% (15 s) and 35% (10 s) of slow
writes, where an even spread gives 20% and 30%; the no-live control held 27% and 35%.
Each loaded period's busiest window fell at a different position. The whole
distribution shifted: write p50 4.59 vs 3.95 ms (+0.64 ms), p90 +0.58 ms, p99 +2.0 ms.
This argues against short periodic bursts as the main cause, but cannot exclude
them, because the replica's timer timing was not recorded.

Raw samples: `.local/activity-soak-20261021.csv` (7,000 rows: start baseline,
in-soak and interleaved, with UTC start time, operation, period, state, latency and
error; SHA-256 `7ba4d48c…7928b9`). The file is in the git-ignored local directory and
is not committed. `TestActivitySoakSampleAnalysis` reproduces the table above from it.

**Next step:** targeted profiling of the interest PUT/DELETE path during
interleaved loaded versus no-live periods. Collect r0 Go CPU, block and mutex profiles
and the opt-in statement timing, and separate the VM-level load-generator overhead.
Application queries were not changed here.

### P1 rollout gates — 2026-10-21

Apple M1 / Docker arm64 VM (4 CPUs, 8.3 GB), local MariaDB 12.3.3 as below. P1.1/P1.2
run three in-process replica services, each with its own pool, against real MariaDB;
P1.3 uses the production-image deployment fixture; P1.4 drives Chromium against two
in-process replicas. The default **rich** fixture has long multilingual titles on a
quarter of groups, 1–12 artists, 1–2 venues, 16 cities and 8 categories; **typical**
(`LIVE_ACTIVITY_CAPACITY_PROFILE=typical`) has 1–3 artists. Capacity seeds extend
contribution expiry so the measured window is stable; the soak uses natural expiry.

**P1.1 distinct public filters — passes at 100–10,000 rich groups.** 768 tabs (256
per replica) with one identical filter versus 256 distinct city/category/artist/venue
combinations per replica:

| Rich groups | SQL per generation, identical / distinct | Warm uncached selection p95 | Max accounting |
| --- | --- | --- | --- |
| 100 | 2.00 / 2.00 | 0.36 ms | 11.1 MiB |
| 1,000 | 2.00 / 2.00 | 0.29 ms | 19.6 MiB |
| 4,000 | 2.00 / 2.00 | 0.38 ms | 42.1 MiB |
| 10,000 (calibrated accounting) | 2.00 / 2.00 | 0.58 ms | 32.9 MiB |

Warm cached reads stay in microseconds; active views were never reselected or
evicted, and 512 extra snapshot-only filters kept cohorts at 256 active + 128 extra.
Reordered, duplicated and irregularly spelled lists (city whitespace/case, country
case) produced one cache miss per canonical filter; distinct views matched a
complete-window reference. The first 10,000-group run **failed**: the old accounting
charged about 3.7 KB per rich candidate (about 35 MiB for the index against about
10 MiB of retained heap), so real-lineup windows failed closed above about 4,475 rich
or 7,000 typical groups. City keys and facet IDs are now interned within and across
generations, and costs are calibrated against retained heap: `TestAccountingCoversRetainedHeap`
measures 1.15–1.48× retained heap for 10,000 rich, typical and one-artist windows.
Rich 10,000 is charged 10.9 MiB; the 64 MiB budget and every cap are unchanged.
The 1,000/4,000 rows predate the calibration.

**P1.2 personalization — passes at 1,000, 4,000 and 10,000 rich groups after fixes.** Each of 257
accounts has 2,000 saved, 1,000 interested, 500 followed-artist and 200 followed-venue
rows, nearly all outside the window (20,000 historical events, 140,000 historical
facets), plus zero/sparse/dense window matches by class. Defects found and fixed:

- Each queued cold or invalidated account assembled alone (4,096 membership calls
  per replica at 4,000 groups). Personal requests waiting for the projection gate now
  join the current assembly.
- A membership change rebuilds the account across the whole window. At 10,000
  groups, sweeps could not finish inside the two-second deadline, discarded completed
  batches and backed off (1,269–1,710 failed rebuilds per remote replica). Rebuilds now
  resume, and up to two batches run together within the two-SQL-job limit.
- While a remote replica was still rebuilding, it closed streams of accounts that had
  a complete view. Those streams now keep their last complete view for up to 10 s
  while the rebuild progresses without SQL errors (the revision poll's staleness).
- The delivery worker read through the 64-request admission meant for external
  requests, so a reconnect burst rejected the worker's own read and closed every
  stream on the replica. Worker reads no longer compete for that admission.
- Accounts left for the next sweep by the per-sweep cap were backed off like failures
  and their streams closed. Only SQL errors back off now, and queued accounts are held.

| Measurement (768 tabs, replicas started together) | 4,000 before fixes | 10,000 after |
| --- | --- | --- |
| 256 distinct accounts per replica, cold p95 | 6.58 s | 1.88 s |
| 64 distinct accounts changing at once, p95 (add / remove) | 10.3 / 8.6 s | 3.53 / 2.92 s |
| One account in 768 tabs, cold p95; mutation p95 | — | 0.52 s; ≤2.56 s |

Every batch stayed within 128 owners × 256 window candidates (no historical IDs);
duplicate tabs used one membership per replica, and all 768 views matched the
reference (including expected-empty zero-match views) before and after mutations.
Overload of 400 concurrent cold accounts on one replica served 64 correct views and
answered 336 with explicit unavailability, never a wrong or broadened view.

**Production image, 10,000 rich groups — passes.** The deployment fixture
(three 2 GiB/one-CPU containers, nginx) with 256 distinct accounts and rich lineups
passed every phase on image `sha256:926c1ec6…cdd`:

| Observation | Result |
| --- | --- |
| Ordinary browse / write p95, 300 pooled samples each | 1.10 gate: 0.977× / 1.030× |
| Initial public snapshot p95; publication p95 (3,840) | 159 ms; 3.12 s |
| 256 accounts changing at once (768 tabs, all converged) | add p95 2.80 s; remove 1.79 s |
| App container memory | 53–56 MiB |

Shared-IP limits, skewed-replica rejection, SQL outage/recovery, drain and log
privacy also passed. The fixture now pools three probe runs per phase: one
100-sample run of this scenario measured write at 1.149× while five others measured
0.86–1.09×. It also publishes a fresh membership target (a rich 10,000-group seed
outlasts the ten-minute window) and retries explicit 503s on local reconnect, as
browsers do. The single-process P1.2 harness, which also runs three replicas and
the load generator in one process, took about 15 s for the same 256-account burst;
production-image timing is the gate result.

`saved_events` has no event-first index, so Saved lookups for many accounts cost
about 5× Interested ones (17–20 vs 3.6 ms per 64 accounts × 256 events). An index
would need a migration and was not added.

The P1.3 soak and P1.4 browser runs below predate the 10,000-group accounting and
membership changes. The soak is superseded by the
[interleaved rerun](#p13-40-minute-soak-with-interleaved-gate--2026-10-21).

**Earlier P1.3 40-minute soak — passes except the write sub-gate.** 1,000 groups with natural
ten-minute expiry and continuous replacement (about 4,000 publications), 704 SSE tabs
spread 256/256/192, 64 fifteen-second fallback clients, count subscriptions of 20 or
101 IDs, 3,840 filter/account/ID rotations and 1,229 private membership writes:

- No growth: goroutines (about 531/531/404), live heap (12–16 MiB), container memory
  (31–37 MiB), SQL connections (2 per replica), database sessions (7), proxy
  connections (about 698), cached-cohort peaks (≤163) and owners (16). Bounds held.
- Count cadence: 52,225 established intervals, all within 14.9–16.1 s, unaffected by
  joins and rotations. 20,548 fallback requests had no failures.
- Ordinary traffic against a no-live baseline at the **same data volume and write
  workload**: browse 1.045× (pass), write 1.113× (6.17 vs 5.55 ms, **fail**), within
  the known ±0.1 single-run variance but over 1.10. The write p95 was 0.80× the
  start-of-soak baseline.
- `/community` browse p95 doubled from 15.4 to 33.5 ms with or without live clients as
  recommendations grew. Its `EXISTS` becomes a LooseScan of all active recommendations,
  then a temporary table and filesort. This is an ordinary-feed scaling finding,
  outside live activity, and is not changed here.

The soak image predates the `membership.changed` protocol change below.

**P1.4 browser-visible latency — passes after a fix.** Chromium, 1,000 rich groups,
timing to the first rendered frame showing the confirmed server view:

| Measurement | p95 |
| --- | --- |
| Warm public / warm personal / cold personal Apply | 29 / 44 / 63 ms |
| Publication / removal commit to rendered | 2.95 / 2.96 s |
| Membership change via another replica (add / remove) | 2.96 / 3.01 s |
| Membership change via the same replica (add / remove) | 1.07 / 0.77 s (was 5.04–9.19 / 5.00 s) |
| Restart cold start, public / personal (navigation to confirmed view) | 110 / 124 ms |
| Idle-eviction cold start, public / personal (max of two) | 115 / 137 ms |

Same-replica membership changes previously sent `unavailable`, so the browser waited
4–5 s of outage backoff (doubling on repeats). The server now sends
`membership.changed` and the client clears the view, then reconnects after
250–750 ms. **Cold-start target, proposed but not yet agreed:** first confirmed view
p95 ≤2 s after restart or idle eviction at the production window size, measured
through the real ingress. These local timings exclude network and ingress.

**P1.5 actual ingress — not run.** `TestLiveActivityIngress` checks flush timing and
heartbeat cadence over HTTP/2 and HTTP/1.1, refused compression, idle timeouts,
spoof-resistant client IPs (each replica limits separately, so expect the first 429
between 61 and replicas × 60 + 1 starts) and drain/replacement routing. It needs an
operator-owned staging URL and, for drain, a replica restart during its window.
Also verify from a second network that a fresh client is not limited.

### Regression-gate investigation — 2026-10-20

Profiling (opt-in `LIVE_ACTIVITY_LOAD_PROFILE=true`: a temporary zero-threshold
statement log on the isolated server, plus status and per-container CPU deltas)
showed **no database contention**: ordinary write transactions took the same server
time without and with 768 streams (p50 0.81→0.82 ms, p95 1.84→1.78 ms), with zero
row-lock waits, no connection churn and MariaDB at 0.47→0.52 cores. Server-side
handler time on r0 moved write p95 5.31→5.65 ms and browse 56.8→54.2 ms.

The extra ~2 ms appeared only on the host-side probe path, which shared the test
process and Docker's host port forwarding with the 768 load-generator streams. From
a separate probe container on the deployment network, through nginx, **unmodified
HEAD** also passed (write 1.012×, browse 1.013×) while the host probe in the same
run reported 1.294×. The fixture now measures ordinary traffic with that in-network
probe ([`testdata/loadprobe`](../internal/persistence/testdata/loadprobe/main.go)).
Earlier host-probe ratios below are retained as recorded, not as app regressions.
Single-run p95 ratios over 100 samples also vary by about ±0.1 between runs.
Replicas run with `GOMAXPROCS=2` under the one-CPU quota (Go's container minimum).

Projection assembly was still reduced, as measured headroom rather than the gate
fix: an allocation-free fingerprint and in-place row scanning cut a warm
10,000-group build from 12.7 to 2.4 ms and its allocation from 23 to 7 MB (GC
cycles per 30 projections 66→25) in the in-process real-MariaDB harness.

| Full 10,000-group scenario, image `sha256:13b5a1b7…19c6e5` | Result |
| --- | --- |
| Ordinary browse / write p95 (in-network via proxy) | 52.0→54.1 ms (1.040×) / 5.62→5.92 ms (1.053×) |
| Initial public snapshot p95 / max | 121.1 / 128.0 ms |
| Publication p95 (3,840 observations) | 3.025 s |
| Membership addition / removal p95 | 1.580 / 2.769 s |
| App memory samples | 36.0–39.9 MiB |

The run also passed skewed-replica rejection, 60/61 shared-IP starts, proxy
heartbeats, SQL outage/recovery, drain, resource bounds and log privacy.

### Resilience and privacy stress — 2026-10-20

`TestMariaDBLiveActivityResilienceStress` (real MariaDB, part of the normal
real-database suite; `LIVE_ACTIVITY_STRESS_SECONDS` extends the churn) runs two
replicas with separate pools plus stallable connections, 26 SSE and 6 fallback
clients (public, Berlin-filtered, private Interested). It continuously publishes,
shortens expiries, renames/redacts contributors, corrects titles/cities and changes
private interests, then stalls readers, mass-withdraws 15 groups and private
interests concurrently, forces a reconnect burst and an SQL outage. Every frame is
checked against database-clock commit bounds: no withdrawn, expired, stale-name,
stale-metadata or non-member item, no account data, and convergence within 5 s.

Against unmodified HEAD it found no privacy violation, but every healthy public
client was interrupted ~88 times in about 15 s. Fixed defects:

- **Expiry interrupted every stream:** any group expiring between projections made
  the generation unavailable, closing all streams and refusing reconnects with 503.
  Expiry is now per view (see operational telemetry above).
- **Stale worker failure:** after an outage the worker re-broadcast its own cached
  failed poll for up to two ticks, closing streams that had already recovered.
- **Private livelock under corrections:** membership assembly verified metadata
  revisions even for Saved/Interested, so corrections arriving faster than the
  projection interval failed and backed off nearly every rebuild. Only
  artist/venue facet joins now verify them; facet sources still fail closed.

With the fixes, eight consecutive runs passed (≈650–770 checked frames each).
Stalled readers release in ~6 s through the write deadline; mass withdrawal
converges in 1–3 s. A burst of concurrent removals can still discard one private
assembly and back off two seconds, by design; local membership changes still close
that owner's streams for quiet reconnect.

### Latest optimized deployment measurements — 2026-10-20

Both scenarios used image manifest
`sha256:5f7bd61942f131b907623ddd0e8248f0b4fb2778d9886756f1106656c3a89937`,
with 768 public streams followed by 768 authenticated streams. The rich scenario
uses long multilingual titles, eight additional artists drawn from 128 IDs, two
contributions per group, and 256 distinct recipient accounts across all replicas.

| Observation | 10,000 groups, shared account | 1,000 rich groups, 256 accounts |
| --- | --- | --- |
| Scenario result | **Fail: write latency** | **Pass** |
| Initial public snapshot p95 / max | 135.5 / 140.1 ms | 24.5 / 27.6 ms |
| Publication p95 (3,840 observations) | 2.081 s | 2.996 s |
| Membership addition / removal p95 | 2.308 / 2.744 s | 3.625 / 3.051 s |
| Browse p95 baseline → loaded | 52.481 → 57.536 ms (1.096×) | 17.032 → 16.466 ms (0.967×) |
| Write p95 baseline → loaded | 6.454 → 8.870 ms (1.374×) | 9.382 → 9.487 ms (1.011×) |
| App RSS samples | 42.2–43.6 MiB | 28.0–28.8 MiB |
| Private-phase cache accounting | about 44.6 MB | at most about 22.0 MB |

Both passed the query-sharing/resource assertions, 60/61 shared-IP starts, actual
proxy heartbeats, publication/removal, membership, SQL outage/recovery and graceful
drain checks. Deliberate skew rejected 64 starts on a full replica while another
replica accepted 64 replacements into its spare capacity. The rich run enforced
the 384-cohort ceiling across membership changes. Healthy projection histogram
samples stayed ≤250 ms at 10,000 groups and ≤50 ms at 1,000 rich groups; selection
p95 stayed in the ≤1 ms bucket. No limiter or activity memory bound was raised.

Unchanged 10,000-group generations now require two SELECTs rather than 42; for
example, nine projection reads plus cleanup used 59 statements (42 cold + 16 warm +
1 cleanup). Initial-stream p95 improved from 1.011 s to 135.5 ms in the large
fixture. These gains do **not** waive the failed write-regression gate. The fixtures
are bounded short runs, not a production capacity guarantee or complete soak test.

### Passing shared-account deployment scenario

The corrected 1,000-group scenario **passes** with 768 public streams followed by
768 private streams sharing an account. Image manifest:
`sha256:ffadb8135fbc6851a613b234f361a5ea65435e22a0001e3a927032b307cf8c6e`.

| Observation | Result |
| --- | --- |
| Initial public snapshot, mixed cold/warm handshakes | p95 987 ms; max 1.010 s |
| Publication (five writes, 3,840 recipient observations) | p95 2.994 s |
| Private Interested addition / removal | p95 1.872 / 2.003 s |
| Ordinary browsing p95, baseline → steady loaded | 16.126 → 14.717 ms (0.913×) |
| Ordinary writes p95, baseline → steady loaded | 8.948 → 8.912 ms (0.996×) |
| Healthy projection histogram | all recorded samples ≤50 ms |
| Uncached selection/encoding histogram | all recorded samples ≤1 ms |
| Container memory sample | apps 23.8–25.7 MiB; nginx 23.3 MiB |
| Accounted projection/cache bytes at recorded private phase | about 12.0 MB per replica |

The run passed SQL query-sharing/cache/budget assertions, 60/61 shared-IP starts,
full-replica rejection, proxy heartbeats, publication/withdrawal, private membership,
SQL failure clearing and recovery, graceful drain, surviving-replica reads and log
privacy. This is one workload result, not the complete section-8 matrix. Initial
handshake measurements do not establish the separate warm Filter Apply gate.

### Measured warm-read optimization

Profiling the shared warm public-snapshot benchmark attributed 52% of sampled CPU
to repeatedly scanning contribution expiry. Caching the generation's earliest
expiry preserves the full-window boundary and removes that per-reader scan.
Apple M1 host measurements (`ns/op`, **not p95**), three seconds per case:

| Candidates | Before | After |
| --- | --- | --- |
| 100 | 1,326 ns | 1,105 ns |
| 1,000 | 3,829 ns | 1,097 ns |
| 10,000 | 26,988 ns | 1,078 ns |

Allocation count remains nine per read. Reproduce with
`go test ./internal/activity -run '^$' -bench '^BenchmarkWarmSharedSnapshot$' -benchmem -benchtime=3s`.
This isolates warm public reads; it does not establish private rebuild or end-to-end
write performance. Query-reuse and end-to-end load measurements are separate gates.

### Environment and earlier failed runs

The 100-group rich/distinct-account run passed public capacity and steady browse/
write ratios (1.092/1.022), with publication p95 2.958 s, but failed mass private
reconnects with 503s. A focused regression reproduced an inactive-owner rebuild
being skipped after another owner consumed coalesced invalidations. Per-account
retry state now distinguishes pending assembly from failed-work backoff; this
failure is retained here rather than described as a passing distinct-account gate.
After that correction, the same workload completed membership addition/removal
(p95 2.227/2.957 s), SQL failure/recovery and drain, but ordinary browsing was
1.106× baseline (11.229 → 12.421 ms), still above the strict 1.10 gate; writes were
1.031×. Its telemetry also exposed 769 retained cohorts after private revisions.
A second regression reproduces this, and eviction now enforces the retired-cohort
allowance for active-view replacements as well as snapshot-only misses. The
deployment fixture now checks the 384 total-cohort ceiling (256 streams + 128).

The corrected 10,000-group scenario completed publication/removal, membership,
SQL outage/recovery and drain. Publication p95 was 2.788 s; membership addition/
removal p95 2.768/2.119 s. Recorded healthy projections stayed in the ≤250 ms bucket,
and cache accounting was about 44.6 MB in the private phase (RSS samples 41.4–48.7 MiB).
Browsing p95 was 52.610 → 57.340 ms (1.090×), but writes were 6.423 → 9.917 ms
(1.544×), failing the 10% gate. This run preceded the constant-time expiry and
unchanged-facet reuse optimizations; retain it as the large-window baseline.

2026-10-19 development environment: Apple M1 / Docker Linux arm64 VM with four
CPUs and 8,304,881,664 bytes RAM. Shared MariaDB 12.3.3 uses the repository's local
configuration (512 MiB InnoDB buffer pool, 60 connections); the DB container has
no CPU/memory cgroup limit. These resources are not the production deployment.

Initial 100-group measurement reached 768 streams, verified skewed-replica rejection,
shared-IP rate limiting and proxy heartbeats, and measured publication p95 3.016 s
over 3,840 recipient observations of five writes. Idle-with-streams memory samples
were 18.8–19.4 MiB per app and 23.2 MiB for nginx. The initial proxy default file
descriptor limit failed near 500 streams; the explicit fixture limit above fixes
that bottleneck. The initial private mutation test expected an uninterrupted local
stream; it was corrected to follow the intentional unavailable/quiet-reconnect
contract. This partial run alone is not a passing deployment scenario.

The subsequent 1,000-group run measured publication p95 2.910 s, private-interest
addition/removal across three replicas at 2.902/2.988 s, and app memory samples
24.2–25.7 MiB. Its SQL fault injection initially failed to interrupt established
MariaDB sessions because database grants are cached; the fixture now disconnects
only its own runtime user's sessions after revocation. The initial 10,000-group run
also reached 768 streams (app samples 43.6–46.9 MiB), then correctly became
unavailable when a new publication exceeded the 10,000-group cap. The healthy
workload now withdraws an existing group before publishing at that boundary.
Neither initial run is recorded as a complete passing resilience gate.

An expanded 768-private-tab run at 1,000 groups found immediate-post-ramp ordinary
browse/write p95 ratios of 1.292/1.626 (30 samples), exceeding the 1.10 gate. It also
exposed the fixture's serialized local reconnect workload and grant-cache behavior
after restoration. The current comparison records 100 observations with a 50 ms
inter-iteration interval after a real heartbeat on every replica; local reconnects
are concurrent with the same 24-start admission used for public handshakes. Fault
injection disconnects the fixture's sessions after both grant changes. Keep the
earlier burst measurements in the record; they are not a passing resilience result.

Regression verification: the real-MariaDB full `go test -race ./...`, `go vet ./...`,
OpenAPI lint (the two existing health-probe warnings), and Docker validation passed.
The final connection journey passed 25 checks with axe, and the filter/presentation journeys
passed 30/65 checks with axe enabled. The final cycle-deadline change passed affected
activity/API race tests. Final full real-MariaDB race tests, vet and Docker `make check`
also pass after the facet-reuse and scope-completeness regressions. Generated assets
and the production binary build in Docker;
this backend-only change introduces no generated source differences.
The final host `npm run build` also passes with no generated-file differences.

The full section-8 acceptance matrix remains the readiness authority. In particular,
record and pass all-distinct versus shared account/filter workloads, sparse/dense
and historical membership, cold rebuilds, continuous candidate/metadata changes,
mass unfollows, reconnect bursts and slow clients, expiry/redaction, 15-second
fallback clients, and ordinary browse/write p95 within 10% of a no-live baseline.
Measure projection p95 ≤250 ms, warm selection ≤10 ms, warm Apply-to-view ≤1 s and
publication/removal/membership ≤5 s. Agree a measured cold-start target before
rollout. Passing small-window runs cannot substitute for large-lineup windows.

Physical-phone, manual screen-reader and the **actual deployment proxy** checks
require operator/device access. Browser emulation and this local nginx are separate
evidence. Production migration/default-on deployment has not been performed.

## Coordinated rollout and rollback

1. Review the completed workload/device/proxy record, deployment resources, backup
   and recovery process, and explicit authorization to migrate/deploy. Keep the
   current production kill switch off while these gates remain open.
2. Schedule a drained-write upgrade of **all** replicas and background writers.
   Explain that schema 17 intentionally enables notifications for existing accounts
   once, including previously paused accounts. Existing channel permissions and
   delivery receipts remain independent; previously configured email/reply delivery
   can resume. Later pauses must survive this and all subsequent restarts.
3. Apply the normal migration command using migration credentials, through schema
   19; reconcile [runtime grants](../deploy/mariadb/runtime-grants.sql) using the
   documented [MariaDB procedure](persistence.md). Do not run migration credentials
   in app replicas. Do not manually repeat the schema-17 data reset.
4. Start the new image with consistent explicit configuration on all replicas.
   Verify readiness/runtime probes, stored preferences and recommendation writes
   with the kill switch still off. Configure trusted-proxy addresses and descriptor/
   connection budgets. Keep 256 streams and 60 starts/minute until measurements
   justify a change; three replicas do not divide the shared projection SQL work.
5. After authorized enablement, verify publication/withdrawal, membership removal,
   name clearing, expiry, pause/revocation, native Refresh and fallback through the
   real ingress. Observe per-replica failures, queue occupancy, query deltas, latency,
   RSS and ordinary browsing/writes. Record rollout date/image/config/results.
6. On a failed gate or regression, set `LIVE_ACTIVITY_ENABLED=false` consistently
   and drain/restart all replicas. Recommendation writes remain usable; stop SSE
   before closing SQL. Retain schema and preferences. Roll back only to a binary
   verified compatible with schema 19, with the kill switch off. Do not restore old
   preference data or reverse the migration as a feature rollback. Re-enablement
   only exposes still-eligible entries; it must not backfill old publications.

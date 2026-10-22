# Live activity: performance and rollout record

Part 5 is **in progress**, not a production-readiness or deployment sign-off.
Keep production's explicit `LIVE_ACTIVITY_ENABLED=false` until the outstanding
gates below pass and the coordinated migration/deployment is authorized.

**Latest result — 2026-10-21:** the [P1 rollout gates](#p1-rollout-gates--2026-10-21)
found and fixed calibration and latency defects. Distinct public filters,
personalization and the production-image deployment scenario now pass at 10,000
rich groups. Open: the [rerun 40-minute soak](#p13-40-minute-soak-with-interleaved-gate--2026-10-21)
on the current image fails the observed write gate (interleaved pooled p95 1.135×;
95% CI 1.076–1.193, inconclusive against 1.10), while browse passes. The
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
  -run '^TestMariaDBActivityDeploymentSoak$' -count=1 -timeout=110m -v
# P1.4 browser-visible latency (Chromium through Playwright)
MARIADB_TEST_ADDR=127.0.0.1:3307 PLAYWRIGHT_MODULE=/path/to/node_modules/playwright \
  LIVE_ACTIVITY_LATENCY_BROWSER_SCRIPT=../../scripts/live-activity-latency-browser.cjs \
  go test ./internal/persistence -run '^TestMariaDBLiveActivityLatencyBrowser$' -count=1 -v
# P1.5 actual ingress: sends real traffic; only an operated staging environment
LIVE_ACTIVITY_INGRESS_URL=https://staging.example LIVE_ACTIVITY_INGRESS_REPLICAS=3 \
  LIVE_ACTIVITY_INGRESS_DRAIN_WAIT=5m go test ./internal/persistence \
  -run '^TestLiveActivityIngress$' -count=1 -v
```

`LIVE_ACTIVITY_CAPACITY_PROFILE=typical` selects 1–3 artist lineups, and
`LIVE_ACTIVITY_PERSONAL_BURST` (default 64, at most 256) sets how many accounts change
membership at once. The soak keeps
1,000 groups (`LIVE_ACTIVITY_LOAD_GROUPS`, at most 9,000) under continuous replacement,
excludes a five-minute warmup from growth checks, and measures the ordinary-traffic
gate in an interleaved phase after the sustained soak (methodology in the
[soak result](#p13-40-minute-soak-with-interleaved-gate--2026-10-21)). Raw samples go
to `LIVE_ACTIVITY_SOAK_SAMPLES` (default `.local/activity-soak-<UTC>.csv`); rerun the
analysis offline with `LIVE_ACTIVITY_SOAK_ANALYZE=<csv> go test ./internal/persistence
-run '^TestActivitySoakSampleAnalysis$' -v`. Allow `-timeout=110m` for a 40-minute soak. The
latency fixture allows 600 starts per minute because every journey shares one
loopback address; production keeps 60.

## Results and outstanding gates

### P1.3 40-minute soak with interleaved gate — 2026-10-21

**Result: the observed write gate fails (1.135× > 1.10); its 95% interval
(1.076–1.193) crosses 1.10, so the evidence is inconclusive, not a pass.** Browse
passes (1.031×; interval 0.962–1.057). Every other soak assertion passed. This is one
predetermined run; it was not repeated.

Image: built from the working tree at `556ad66` plus measurement-only test changes,
index `sha256:e5d7204da1dddce29952211dece58cd700c2ae5e38ee6dea23c48f8ca13cc271`
(linux/arm64 manifest `sha256:8b45a4d570b91ff21d25f1e2b88915cb90a37dda0283d3bd576b542d0c32f995`).
Environment: Apple M1, Docker 29.1.4 arm64 VM (4 CPUs, 8.3 GB), MariaDB 12.3.3
without cgroup limits; the fixture as described above.

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
connection handling (Docker port forwarding and nginx) on the same 4-CPU VM.

Interpretation: the write slowdown is consistent in direction (all four pairs >1.0;
the interval excludes 1.0) and is about 0.5–1 ms at p95. The data cannot show whether
it exceeds 10%. Ordering does not explain the previous 1.113×: interleaving moved
the estimate to 1.135×. The probe's profile showed no CPU-quota throttling in either
state (r0 0.05 → 0.08–0.09 cores; MariaDB 0.38–0.42 → 0.40–0.45 cores).

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

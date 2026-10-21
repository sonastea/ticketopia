# Live activity: performance and rollout record

Part 5 is **in progress**, not a production-readiness or deployment sign-off.
Keep production's explicit `LIVE_ACTIVITY_ENABLED=false` until the outstanding
gates below pass and the coordinated migration/deployment is authorized.

**Latest result — 2026-10-20 (later):** the full 10,000-group scenario now passes,
including the ordinary-traffic gate (browse 1.040×, write 1.053×). The earlier
"37.4% write regression" was a probe-placement artifact, not database or app
contention; see [regression-gate investigation](#regression-gate-investigation--2026-10-20).
Resilience/privacy stress checks found and fixed three availability defects. The
rich 1,000-group run has not been repeated on the new image. Physical-device,
screen-reader and actual-ingress verification remain open.

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
Failed attempts back off per account for two seconds after assembly; duplicate tabs
cannot repeatedly retry a failed membership query.
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

## Results and outstanding gates

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

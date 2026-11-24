# MariaDB query performance

The opt-in persistence workload uses the existing disposable MariaDB fixture and
`MARIADB_TEST_ADDR`, `MARIADB_TEST_CA`, `MARIADB_TEST_ADMIN_USER`, and
`MARIADB_TEST_ADMIN_PASSWORD`. Each invocation owns and removes a random schema
and users. Runtime grants are unchanged. Schema 15 puts scope equality columns
before dates in `discovery_coverage` and removes the duplicate `discussion_recent`
index; `discussion_replies` retains the same columns and root foreign-key support.
Drain schema-14 application instances before migrating.

## Reproduce

```sh
export MARIADB_TEST_ADDR=127.0.0.1:3307
export MARIADB_BENCH_EVENTS=10000
export MARIADB_BENCH_CONFIG=baseline
go test ./internal/persistence -run '^$' -bench BenchmarkMariaDB \
  -benchmem -benchtime=1s -count=5 -timeout=60m > before.txt
export MARIADB_BENCH_CONFIG=current
go test ./internal/persistence -run '^$' -bench BenchmarkMariaDB \
  -benchmem -benchtime=1s -count=5 -timeout=60m > after.txt
benchstat before.txt after.txt

export MARIADB_BENCH_PLANS=1
export MARIADB_BENCH_PLAN_DIR="$PWD/query-plans"
go test ./internal/persistence -run '^TestMariaDBQueryPlans$' -v -timeout=60m
```

Set `MARIADB_BENCH_COMPARE=1` with the diagnostic test to capture all seven index
configurations against one fixture, resetting indexes and refreshing statistics
between configurations. The JSON contains five warm execution-time samples per
SELECT and per-index allocated bytes from `mysql.innodb_index_stats` (queried with
the fixture's existing admin connection).

For noisy end-to-end results, `TestMariaDBQueryComparison` reuses one fixture and
alternates baseline/current order over five rounds. It records Go ns/op, B/op and
allocs/op for filtered/global discussions, Discover category/count misses and
selective coverage in `<size>-paired-benchmarks.json`:

```sh
MARIADB_BENCH_COMPARE=1 go test ./internal/persistence \
  -run '^TestMariaDBQueryComparison$' -benchtime=20x -v -timeout=30m
```

Use `1000` for correctness/iteration and explicitly select `100000` for scaling.
Without the size variable, benchmarks run 1,000 and 10,000 events. Run comparisons
serially on the same idle server, alternating configurations to detect drift.
`baseline` restores the schema-14 coverage and duplicate discussion indexes;
`current` uses committed migrations. Candidate configurations `date_time`,
`date_time_desc`, `name_prefix`, `coverage_range`, and `coverage_order` replace only
the relevant disposable index. `coverage_range` also explicitly rebuilds the
retained coverage layout for comparison with the success-order alternative.
All configurations refresh optimizer statistics with `ANALYZE TABLE` after setup.
They never alter the application's configured database or runtime grants.

An explicit, bounded scaling run (fixture and real cursor traversal can be slow):

```sh
MARIADB_BENCH_EVENTS=100000 go test ./internal/persistence -run '^$' \
  -bench '^BenchmarkMariaDBDiscover$' -benchmem -benchtime=3x -timeout=2h
```

The deterministic workload pins reads to **2026-10-08 12:00 UTC**. Berlin holds
80% of events; Hamburg and Paris hold 10% each. Categories, artists and venues
include rare values, dates/times and full names tie, some times are null, and
names share more than 120 initial characters. Half the events have no community
activity; hot events have 32 recommendation authors and 64 replies. Contributions
include active/withdrawn recommendations and hidden/removed posts. Scheduled
coverage and accumulated overlapping scopes exercise distinct coverage paths.
The repository's internal reference-time path binds coverage clock values for
these runs; live coverage/lease comparisons still use MariaDB's clock. Diagnostics
use the same query builders and bound values as the fixed-clock benchmark reads.

## Timing boundaries and interpretation

`TestMariaDBCollectionKnownEventsUpgrade` reports the median of five warm
`ANALYZE FORMAT=JSON` runs for the old and new omission queries on a disposable
schema with 100,000 repeated receipts. This measures SQL selection only, not
the migration or the full completion transaction.

### Queue claim plans

```sh
MARIADB_TEST_ADDR=127.0.0.1:3307 MARIADB_QUEUE_PLANS=1 \
  go test ./internal/persistence -count=1 -v -run '^TestMariaDB(Reminder|Collection)ClaimPlans$'
```

The opt-in tests use disposable schemas. They roll back locking reads and
recovery updates; no mail is sent and no collector runs. The collection
comparison uses 10,000 stored tasks and reports each query's median and p95.
On MariaDB 12.3.3, binding the active IDs as one JSON parameter reduced median
ready-queue reads from 9.4 to 5.7 ms for 1,800 IDs and from 37.0 to 18.0 ms for
7,320 IDs. At 90 IDs it was about 2 ms either way. The matching-order index
was not required for the change.

In the 10,000-reminder fixture, a lease-first index helped when nearly all
reminders were leased, but raised all-ready p95 from about 3 to 31 ms. The
covering index was not a consistent improvement. The recovery update used
its inflight index and stayed below 0.6 ms p95 with zero or ten expired sends.
The reminder queries and recovery cadence are unchanged.

### Country-only and artist Discover filters

```sh
MARIADB_TEST_ADDR=127.0.0.1:3307 MARIADB_FILTER_PLANS=1 \
  go test ./internal/persistence -count=1 -v -run '^TestMariaDBDiscoverFilterPlans$'
```

This comparison uses 10,000 events, including events in two cities in the same
country. It checks first-page results and exact counts against explicit
country-first and artist-first alternatives and alternates 11 warm SQL plan
samples per variant. MariaDB already uses `search_country` and `search_facet`
for these cases. The forced alternatives did not improve both page and count
enough to justify changing the existing queries. For the dominant country,
the current broad-window query still processes 9,000 matches: about 27 ms
for the page and 17 ms for an uncached count. That workload is the better
candidate for a date-window or sort-plan comparison.

The date-window follow-up forced `events.search_date` for windows of 1 to 30
days. Germany's three- and seven-day counts fell from 3.97 to 2.71 ms and from
10.39 to 7.48 ms, respectively, but its 30-day count rose from 16.56 to
23.29 ms. France's 30-day first page rose from 4.42 to 34.95 ms. The one-day
Germany case was already under 1 ms on both plans. The existing optimizer
choice remains in use; no forced hint or schema change shipped.

### Existing query benchmarks

On the 1,000-event fixture, separating visible-count predicates from page-only
placeholder predicates reduced the median exact global discussion count from
1.31 to 0.59 ms over five warm SQL samples. `ANALYZE FORMAT=JSON` no longer
showed the extra 2,120-row child-post pass in the count. The page still uses
that predicate to retain withheld threads with visible replies.

### Query optimization batch closeout

The artist/venue follow list limits each indexed branch before merging; see
[follow timings and cursor verification](follows.md#query-verification).
The notification inbox filters visible envelope keys before loading event and
reply details. In the local 10,000-envelope fixture, median SQL time went from
198.68 to 2.99 ms with all visible, 177.95 to 3.02 ms with 100 hidden at the
front, and 37.31 to 7.18 ms when all were hidden. Both query variants returned
identical rows; a continuation started at the previous page's lookahead.

```sh
MARIADB_TEST_ADDR=127.0.0.1:3307 MARIADB_INBOX_PLANS=1 MARIADB_ACTIVITY_PLANS=1 \
  go test ./internal/persistence -count=1 -v \
  -run '^TestMariaDB(InboxQueryCosts|ActivityQueryCosts|LiveActivityResilienceStress|ModerationQueueStatementsAreBounded)$'
MARIADB_TEST_ADDR=127.0.0.1:3307 go test ./internal/persistence -run '^$' \
  -bench '^BenchmarkMariaDBDiscussionReplyFanout$' -benchtime=12x
```

The following were checked but are not part of the shipped changes:

| Item | Local evidence and disposition |
| --- | --- |
| Per-event counters | An uncached 101-event exact-total batch over 32 recommendations and 32 interests per event took 5.74 ms p95. Keep short-lived sharing; add transactional counters only if production profiles show repeated count misses matter. |
| Incremental live projection | Refreshing 1,000 event groups from 32,000 live contributions with reusable facets took 73.57 ms p95. The two-replica resilience test passed. Revisit if normal refresh timings or candidate-window growth warrant maintaining a second projection. |
| Sparse recommendation filters | On the 1,000-event fixture, city/country used indexed event matches and category/combined used the facet index; medians were 0.31–0.51 ms. Keep current queries until a slower production filter is identified. |
| Moderation | The MariaDB test verifies at most two statements for 0–101 rows, including a tied cursor, with the same redacted results. It is a statement budget, not a production latency claim. |
| Reply fan-out | Mean transaction time was 6.7, 9.6 and 46.8 ms at 1, 100 and 1,000 followers. Moving delivery out of the write needs a durable handoff and failure semantics; defer until lock or delivery telemetry demonstrates a need. |

These fixtures do not set production capacity or latency targets.

- Fixture creation, statistics refresh, cursor traversal and count-cache eviction
  are outside timing. Later Discover pages are reached through real cursors.
- Discover and Radar measure repository reads, including decoding and transaction
  overhead. They exclude service parsing, provider calls and service caches.
- `count_hit` primes the process count cache; `count_miss` evicts only its key
  before each operation. Both include the normal page and coverage work.
  `minority_city` and `minority_city_count_miss` search Paris (a tenth of the
  catalog), like one city among many in production: a plan that reads every
  city's events, as Berlin-only cases cannot show, is slow here. Radar's
  `minority_city` case reads for a Paris owner; `BenchmarkMariaDBCommunity`'s
  `previews` batches one page of event-card previews (four hot events).
- Both fixtures load in bulk; production plans can differ when statistics are
  stale. After large ingestion, `ANALYZE TABLE` the catalog tables (events,
  event_snapshots, event_search_places, event_search_facets) and community
  tables before comparing plans.
- Discussion reads include their count; grouped recommendations include batched
  author previews. The matrix samples distinct plans, not every filter product.
- Writes are separate: committed 100-event collection pages with newer observation
  timestamps, committed thread replies, and on-demand scope claims. Collection
  claim/finish and scope/reply cleanup are
  excluded. Collection receipts accumulate; use identical iteration counts when
  comparing write runs. The reply case keeps table cardinality stable.
- Plans run outside benchmarks. Each JSON records server version, dataset size,
  exact SQL/parameters, reference time, cache state and schema secondary-index
  bytes. `ANALYZE FORMAT=JSON` warms each SELECT, then records five samples and
  saves the last complete plan.
  This does **not** measure a cold buffer pool. Direct SQL bypasses Go count caches.
- Read nested-loop order and `key`/`used_key_parts`, estimated `rows` versus
  `r_rows`, `r_loops`, `filtered`/`r_filtered`, filesort/temporary-table nodes and
  `r_total_time_ms`. Where available, `r_index_rows` and `r_icp_filtered` also show
  index entries visited and index-condition rejection. Approximate rows delivered
  by the engine per node as `r_rows * r_loops`, and
  residual rejection as that product times `(1-r_filtered/100)`. Do not sum nested
  timings or count derived rows as additional base-table reads.
- Expanded cursor predicates are diagnostic alternatives derived from the actual
  SELECT. They are not enabled in production. Plans and index names are evidence,
  never CI assertions. The migration test checks the intended schema contract,
  not an optimizer choice.

## Acceptance

Compare repeated ns/op, B/op and allocs/op samples alongside plan evidence and
index bytes, keeping write-batch timing separate. Retain changes only after
behavioral tests, repeatable gains above noise and investigation of common-case
regressions. Full-name ordering must keep the database collation and event-ID
tie-break; a name prefix is not a substitute. A full 1,024-character utf8mb4 name
already exceeds InnoDB's 3,072-byte key limit before other ordering columns.
No generated normalized-time column or optimizer hint is justified by that limit.

See the [measurement report](query-performance-report.md) for this evaluation.

The [public HTTP benchmark](public-cache-measurements.md) extends this fixture
through the discovery service and HTTP router, comparing 200 and conditional 304
responses. It measures bytes and handler costs without claiming reduced SQL work.

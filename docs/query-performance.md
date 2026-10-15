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

- Fixture creation, statistics refresh, cursor traversal and count-cache eviction
  are outside timing. Later Discover pages are reached through real cursors.
- Discover and Radar measure repository reads, including decoding and transaction
  overhead. They exclude service parsing, provider calls and service caches.
- `count_hit` primes the process count cache; `count_miss` evicts only its key
  before each operation. Both include the normal page and coverage work.
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

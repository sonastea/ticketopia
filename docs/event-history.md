# Reliable event history

Implemented in MariaDB schema **9**: independent artist/venue catalogs and
case-sensitive provider mappings, immutable dated event observations, first/last
seen and meaningful-change times, durable city/date refresh ownership, collection
coverage/failure receipts, and shared Ticketmaster budgets/pacing/cooldowns.
Notification delivery and a public history API/timeline remain separate work.

## Enable scheduled collection

Drain older binaries, preserve a backup, apply migrations with separate migration
credentials, and reapply runtime grants before starting this binary. It accepts
clean schema 10 only, including [private follows](follows.md); drain older apps before migration. See
[migration and recovery procedures](persistence.md#explicit-migrations-and-failed-ddl-recovery).
The feature does not migrate databases on startup.

```sh
export PERSISTENCE_MODE=mariadb
export EVENT_HISTORY_ENABLED=true
export EVENT_HISTORY_CITIES='[{"city":"Chicago","country":"US"}]'
export EVENT_HISTORY_DAYS=90
export EVENT_HISTORY_INTERVAL=6h
export TICKETMASTER_DAILY_BUDGET=4500
```

Use the existing [MariaDB connection settings](persistence.md) and Ticketmaster
key. Collection is **off by default**, independent of authentication, and requires
a key and MariaDB. `EVENT_HISTORY_CITIES` accepts 1–20 city/country objects;
whitespace/country case are normalized and duplicate scopes deduplicated. Days
accepts 1–366 (default 90); interval accepts 1h–168h (default 6h).

The scheduler seeds one all-category task for each city and date, starting with
today UTC through `DAYS-1` days later. Queries use inclusive **venue-local** dates.
It refreshes due tasks oldest-first, fetching at most ten pages of 100 events per
task. One-day windows reduce Ticketmaster's 1,000-result paging limit. It polls
idle/error conditions once per minute. Removing a city or shrinking the horizon
stops claiming those tasks without deleting their history.

Tasks/runs are claimed atomically in MariaDB with two-minute leases. Every page
and completion checks ownership; successfully committed pages renew the lease.
A crashed worker's expired claim can be recovered by another worker, recording
an interrupted failed/partial run. Upstream calls happen outside transactions,
with a bounded 20-second page deadline. Shutdown cancels/drains the collector
before closing cache/SQL resources.

## Budget and freshness

In **MariaDB mode**, all app provider resources use shared request accounting,
even with scheduled collection disabled. A key fingerprint, not the secret, keys
`provider_budgets`. Four request starts/second, the configured 24-hour budget,
and provider cooldowns survive restarts and coordinate independent app pools.
The window starts with its first reservation. Lower limits take effect immediately;
increases wait for the next window. Failed/uncertain reservations are conservative:
database failure never permits an unaccounted request. Cache hits use no quota.

All replicas calling the same key must use this binary and the **same MariaDB**.
Database-free processes, other applications using the key, separate databases,
and unrelated key users cannot be coordinated. Leave headroom for those uses.
Database-free discovery retains its existing process-local budget. There is no
guarantee that every city/date finishes before its interval when quota is tight.
Reduce the horizon/cities or lengthen the interval rather than exceeding quota.

Collection bypasses cache/stale fallback and normalizes fresh provider data through
the same client as HTTP discovery. Successful pages also refresh discovery caches.
Each page atomically persists event identity, snapshot, observation, catalogs,
provider mappings, and run evidence. A retry cannot duplicate a page/observation.
Older observations are retained but cannot rewind current event metadata or
generate retrospective changes. Equal event/timestamp observations are immutable.

Fresh local save/interest/recommendation/discussion snapshot writes also record
observations in their existing activity transaction. Stale retained reads do not
advance history. Plain interactive discovery does not ingest every read into SQL.
The migration preserves old snapshots/activity but does not invent a past history;
change detection starts with the first recorded fresh observation.

## Coverage and failure semantics

`collection_tasks` retains next refresh, last attempt, last complete success and
last failure. `collection_runs`, immutable `collection_pages`, and distinct
`collection_run_events` retain each attempt's timestamps, totals and event evidence:

| Run status | Meaning |
| --- | --- |
| `complete` | Contiguous pages with stable reported totals and that many distinct events; a valid empty result also completes. |
| `limited` | Provider paging cap prevented complete coverage. |
| `partial` | Good pages committed before failure/interruption, or totals/distinct results shifted. |
| `failed` | No page completed before provider failure/interruption. |

Only `complete` advances a task's `last_success`. Pagination is not an upstream
atomic snapshot: even matching totals cannot prove no event shifted between pages.
Failures retry after an hour or the provider's longer cooldown/reset. Capped or
shifted collections retry at the configured interval. Receipt codes are bounded
(`provider_unavailable`, `provider_result_cap`, `result_set_changed`, `interrupted`),
never raw provider bodies/URLs. Worker logs include city/date, status and page count.

Failure, 404, empty results, omission, cache eviction, and leaving the configured
date horizon **never** delete good events or imply cancellation. Last-known public
event detail fallback remains available without accounts and is labeled stale,
with its actual `data_as_of`. Search results still follow cache/provider behavior;
this feature does not turn the event list into a historical search engine.

## Meaningful changes

`event_observations` preserves the normalized DTO, collection instant and before/
after evidence. `event_history_state` records first/last seen, last meaningful
change and a comparison baseline. Change kinds include:

- `date`: UTC/local dates and times, time zone, TBA/TBD/no-specific-time flags.
- `venue`: case-sensitive provider identities, independent of ordering/renames.
- `sale_time`: public-sale start/end/TBD and known presale time windows.
- `cancellation` / `postponement`: explicit provider statuses only.
- `status`: other explicit transitions, including restoration/rescheduling.

Equivalent UTC instants, title/image/price edits, venue ordering and presale
presentation alone do not produce meaningful changes. Sparse unknown date/venue/
sale/status data retains prior known comparison facts, avoiding false disappearance
and reappearance notifications; the actual incomplete DTO remains in its dated
observation. Unchanged refreshes advance last seen but not last changed. There are
no reminders/jobs/emails yet, and unobserved upstream changes cannot be inferred.

## Verification

```sh
export MARIADB_TEST_ADDR=127.0.0.1:3307
export MARIADB_TEST_CA="$PWD/.local/mariadb/certs/ca.crt"
go test -race ./...
go vet ./...
```

Tests use provider fixtures and isolated random schemas on real MariaDB: cross-pool
claims, expired fencing/recovery, page rollback/retries, capped/shifted coverage,
failure/empty retention, dated changes, sparse/stale facts, case-distinct catalogs,
public fallback, restarts, shared quota/cooldown/fail-closed behavior and schema-8
upgrade preservation. No live Ticketmaster quota, production migration, cluster
deployment or backup/restore exercise is performed by this feature's tests.

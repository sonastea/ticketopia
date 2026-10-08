# Reliable event history

Implemented through MariaDB schema **11**: independent artist/venue catalogs and
case-sensitive provider mappings, immutable dated event observations, first/last
seen and meaningful-change times, durable city/date refresh ownership, collection
coverage/failure receipts, and shared Ticketmaster budgets/pacing/cooldowns.
Discover and the existing event-list API search that stored catalog; event details
show first/last seen and dated changes through the same service. Email reminders
and notification delivery remain separate later features.

## Stored Discover and coverage-aware reads

`PERSISTENCE_MODE=mariadb` enables durable Discover reads independently of accounts
and the scheduler. The database-free mode retains the existing provider/cache path.
SQL filters city/country, inclusive local dates, category/genre, artist/venue IDs,
and literal case-insensitive event/artist/venue-name keywords (including short names
and escaped `%`/`_`). Indexed public place/facet projections and date/name indexes
narrow queries; substring keyword matching is a residual filter, not full-text
ranking. Sort by `date_asc`, `date_desc`, or `name_asc`. SQL counts/paginates the
stored matches, including past dates when explicitly requested; it is not capped
at the provider's 1,000-result window. Filter/limit/sort-bound cursors are not
snapshot cursors: changes between requests may shift pages.

Fresh complete city/day receipts covering every requested date, or an enclosing
complete on-demand city/date receipt, avoid event-provider calls. Freshness is six
hours by default or the configured scheduler interval when enabled. Last-good
snapshots remain searchable during failures, quota exhaustion, cooldowns, and
restarts. Older individual snapshots are still labeled older even if collection
coverage is complete; omission does not remove events or assert live availability.
Known omitted events awaiting targeted verification are labeled older/last-known
even when their previous observation is still within the normal freshness interval.

Missing/due coverage triggers one broadly scoped on-demand collection, independent
of the configured city list. It clears category/genre/keyword filters and, for city
searches, artist/venue IDs so later local filters reuse the same coverage. Without
a city, supplied artist/venue IDs retain their scope. Claims are SQL-fenced across
replicas with 30-second leases; foreground work is bounded to 12 seconds and ten
100-event pages. Each page commits public metadata/history and distinct-event
evidence atomically. Page transactions allow at least ten seconds (or the longer
configured SQL timeout), clipped by the foreground collection deadline; ordinary
reads retain the configured query timeout. Only stable totals matching contiguous pages/distinct events
establish complete coverage. Interrupted/capped/shifted collections remain partial;
previous complete success is retained. Failed requests retry after an hour or the
longer provider reset, partial/capped attempts after the freshness interval. Long
on-demand ranges may hit the cap; shorten dates or configure one-day scheduling.
On-demand scopes refresh when requested, not through a permanent new city schedule.

`meta.coverage.status` and website copy distinguish `complete`, `partial`, `stale`,
and `not_collected`, with complete-date counts and total requested dates. An empty
uncollected/partial/stale search is never presented as a confirmed no-events result.
`data_as_of` is null when no successful collection or retained snapshot exists.
Coverage reports evidence, not an atomic upstream snapshot or ticket inventory.
Optional taxonomy failures never prevent the stored event list from rendering.
Public searches/history contain no save/follow/interest ownership or preferences;
coverage stores broad public scopes, not users' search keywords or identities.

### Missing runtime grants

`Discover refresh incomplete; retaining catalog` with `discover claim` and MariaDB
code **1142** means the runtime user lacks a required table permission. The claim
needs INSERT/UPDATE on `discovery_scopes`; collection also needs the projection,
scope-event and detail-task grants in
[`runtime-grants.sql`](../deploy/mariadb/runtime-grants.sql). Schema migrations do
not apply runtime grants. Application startup now
[validates all runtime writes](persistence.md#startup-permission-validation) and
refuses to serve HTTP if any grant is missing, even when the schema is clean.
Older binaries, or permissions revoked after startup, can still produce this
warning; until repaired, Discover retains only existing stored matches.

For local Compose development, `make db-setup` reapplies the grants and verifies
every required Discover write with zero-row statements, without resetting data.
For shared deployments, have an authorized operator reconcile the runtime grants
after migration; do not run the local setup workflow against a shared database.

## Enable scheduled collection

Drain older binaries, preserve a backup, apply migrations with separate migration
credentials, and reapply runtime grants before starting this binary. It accepts
clean schema 11 only, including [private follows](follows.md); drain older apps before migration. See
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
advance history. On-demand event searches and fresh detail refreshes now ingest
provider observations; reuse of stored results never advances last seen.
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
with its actual `data_as_of`. In MariaDB mode searches use SQL plus coverage-aware
collection; database-free mode retains cache/provider behavior.

## Missing events and operational visibility

Complete searches enqueue known omitted events for targeted detail reads, including
events previously seen in the scope and retained matches. The scheduled collector
and on-demand-only deployments process at most one targeted detail per minute per
worker, with independent SQL leases and the same shared budget. Fresh success
records actual moved/TBA/TBD facts and clears the queue entry. Unavailable reads
retry after an hour or provider reset; 404 retries after 24 hours. Neither records
a cancellation or advances last seen. Repeated omission can enqueue another read;
there is no guarantee of timely detection when the shared allowance is exhausted.

All MariaDB deployments emit an `Event catalog operational status` structured log
at startup and every 15 minutes: collected/stale/failed dates, on-demand and
stale/partial/failed scopes, pending/failed detail refreshes, active-window quota
used/budget, and active provider cooldowns. Counts include retained inactive scopes,
so inspect city/date receipts to distinguish inactive from active backlogs.
Per-collection logs include coverage/status/page counts; on-demand logs use a scope
digest, not keywords or account identities. Database/provider errors remain
sanitized. These logs are operator-only, not a public diagnostics endpoint.

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

Event Overview displays first/last-seen instants and up to 20 recent meaningful
observation instants with dated summaries such as “Moved from …” and “Previously
scheduled for …”. Venue names resolve from the public catalog (IDs remain the
immutable comparison evidence); renames may affect the displayed catalog name.
Both `/api/v1/events/{event_id}` and
`/api/v1/events/{event_id}/history?limit=20&before=RFC3339` share these reads. The
history endpoint makes no provider requests and exposes no private activity.
Use `next_before` for earlier observation instants; all changes at one instant
stay on the same page. A known event without observations returns null; an unknown
event returns 404; database-free history returns 503.

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
upgrade preservation, SQL filtering/sorting/cursors, on-demand/daily coverage,
partial/outage/quota/restart HTML/API reads, missing-event details, and privacy.
`scripts/history-browser.cjs`, driven by `TestMariaDBHistoryBrowserReview` with
`HISTORY_BROWSER_SCRIPT`, uses isolated SQL fixtures and no provider key for
desktop/tablet/390px/320px/200%-text, keyword/sort, enhanced and native pagination,
history detail, distinct coverage/empty states, keyboard focus and axe checks.
Set `PLAYWRIGHT_MODULE`, `CHROMIUM_EXECUTABLE`, `AXE_SCRIPT`, and optionally
`HISTORY_REVIEW_DIR` to local browser tooling. No live Ticketmaster quota,
production migration, cluster
deployment or backup/restore exercise is performed by this feature's tests.

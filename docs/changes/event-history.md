# Reliable event history changes

## Unreleased

### 2026-10-09 — Keep Discover usable during refresh and cancellation

- Serve retained catalog results immediately during due collection; isolate
  shared refreshes from browser cancellation and drain them before SQL/cache close.
- Finalize failed/partial ingestion with bounded cleanup so interrupted writes
  do not leave the scope running until its lease expires.
- Give each on-demand page a fresh 20-second fetch/ingestion budget instead of
  sharing 12 seconds across the entire scan. Bound cold-request waits to 12
  seconds without canceling collection; regression-test later-page SQL time,
  partial-to-complete background progress and stalled-page cleanup.
- Buffer HTML and commit through Echo, preventing partial pages and duplicate
  response headers when rendering fails; cap the optional page taxonomy wait at
  one second. Regression-test cancellation, retained reads, failure receipts,
  shutdown, slow optional metadata and response tracking.
- See [stored Discover behavior](../event-history.md#stored-discover-and-coverage-aware-reads).

### 2026-10-08 — Durable observations and budgeted city/date collection

- Finish the website integration in schema 11: existing Discover/event-list API
  use indexed SQL filters, sorting and pagination, fresh coverage reuse and
  durable partial/stale/outage/quota fallback, including on-demand unconfigured cities.
- Show truthful collection/empty states and first/last-seen plus dated changes
  on event Overview; share detail/history API reads without private activity.
- Queue budgeted, fenced detail refreshes for omitted events; retain good facts
  through absence/404 without inferring cancellation. Add operator coverage,
  backlog, failure/cooldown and quota aggregate logs.
- Verify isolated MariaDB SQL/API/restart/quota/partial coverage and Chromium
  keyword/sort/pagination/history/empty states, 320px/200%-text and accessibility.
- Check all schema-11 Discover write permissions during local database setup;
  regression-test missing grants individually and use least-privilege Discover
  grants in SQL fixtures. Document [1142 recovery](../event-history.md#missing-runtime-grants).
- Refuse application startup when runtime writes are missing; see shared
  [permission validation](../persistence.md#startup-permission-validation).

- Persist independent artists/venues/provider mappings and immutable dated event
  observations with first/last seen and meaningful-change evidence in schema 9.
- Add opt-in one-day city refreshes, fenced expiring claims, restart-safe shared
  Ticketmaster accounting/pacing/cooldowns, and complete/partial/capped/failed receipts.
- Preserve last-good data through failure/absence/older samples; detect dates,
  venues, public/presale timing and explicit cancellation/postponement without
  false sparse-data changes. Keep public snapshot fallback independent of accounts.
- Initially preserve the event-detail contract with account-independent retained
  metadata; the integration above now extends shared read metadata/history.
- Verify real-MariaDB concurrency, atomic rollback/retries, quotas/cooldowns,
  restart/upgrade retention, changed paging coverage and fixture-based collection.
  No live quota, running-schema migration or deployment was performed.
- See [feature/setup](../event-history.md), [persistence](../persistence.md) and
  [delivery goals](../goals/delivery.md#2-keep-reliable-event-and-application-history).

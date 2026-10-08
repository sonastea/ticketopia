# Reliable event history changes

## Unreleased

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

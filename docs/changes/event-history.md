# Reliable event history changes

## Unreleased

### 2026-10-08 — Durable observations and budgeted city/date collection

- Persist independent artists/venues/provider mappings and immutable dated event
  observations with first/last seen and meaningful-change evidence in schema 9.
- Add opt-in one-day city refreshes, fenced expiring claims, restart-safe shared
  Ticketmaster accounting/pacing/cooldowns, and complete/partial/capped/failed receipts.
- Preserve last-good data through failure/absence/older samples; detect dates,
  venues, public/presale timing and explicit cancellation/postponement without
  false sparse-data changes. Keep public snapshot fallback independent of accounts.
- Align the existing event-detail OpenAPI description with account-independent
  retained metadata; keep HTTP DTOs and routes unchanged.
- Verify real-MariaDB concurrency, atomic rollback/retries, quotas/cooldowns,
  restart/upgrade retention, changed paging coverage and fixture-based collection.
  No live quota, running-schema migration or deployment was performed.
- See [feature/setup](../event-history.md), [persistence](../persistence.md) and
  [delivery goals](../goals/delivery.md#2-keep-reliable-event-and-application-history).

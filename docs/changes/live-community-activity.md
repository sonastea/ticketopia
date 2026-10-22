# Live community activity changes

## Unreleased

### 2026-10-21 — Part 5: P1 capacity, soak, latency and ingress gates

- Add opt-in gates for distinct public filters, personalization with large histories,
  a 40-minute mixed soak, browser-visible latency and actual-ingress verification.
  Add process growth signals (goroutines, heap, SQL connections) to metrics logs.
- Batch queued cold/invalidated accounts into shared membership assembly. At 4,000
  rich groups, cold p95 drops from 6.58 to 1.25 s and mutation bursts from 10.3 to 3.3 s.
- Send `membership.changed` for same-replica membership changes; browsers reconnect
  quietly in 250–750 ms rather than with outage backoff (p95 1.07 s, was 5.0–9.2 s).
- Intern city/facet strings and calibrate memory accounting to retained heap (rich
  10,000 groups: 35 → 11 MiB charged), so real-lineup 10,000-group windows no longer
  fail closed above about 4,475 groups; limits are unchanged.
- Make long membership rebuilds resume and read two batches at once; hold the last
  complete view (≤10 s) while a remote rebuild progresses; back off only on SQL
  errors; keep worker reads out of request admission. The production-image
  10,000-rich-group, 256-account scenario passes (membership p95 2.8 s).
- Measure the soak's ordinary-traffic gate in an interleaved ABBA×2 phase (live
  clients stopped/restarted per period, writers unchanged), retain timestamped raw
  samples, and report a seeded paired-period block-bootstrap interval.
- Open: interleaved soak write p95 is 1.135× (95% CI 1.076–1.193, inconclusive
  against 1.10); ingress verification not run.
  See the [rollout record](../live-activity-rollout.md#p13-40-minute-soak-with-interleaved-gate--2026-10-21).

### 2026-10-20 — Part 5: operational measurement and deployment fixture

- Add bounded, identity-free metrics for caches, work, SQL statements, timing,
  failures and delivery; reuse fresh counts for joining tabs and bound count reads.
- Bound the aggregate delivery cycle and fix inactive-account membership rebuilds
  after coalesced invalidation, retaining per-account failed-read backoff.
- Enforce the unreferenced-cohort cap across private membership revisions too.
- Profile warm reads, cache complete-window expiry, and reuse revision-matched
  facets without reusing attribution; improve contended warm stream delivery.
- Add a production-image three-replica/nginx load and SQL-recovery/drain fixture.
- Pass the rich 1,000-group/256-account/768-stream scenario. Profiling showed the
  10,000-group write "regression" was host-probe placement, not DB/app contention;
  probe in-network and pass the full 10,000-group gate (1.040×/1.053×).
- Add a real-MariaDB resilience/privacy stress test; fix expiry closing all streams,
  stale worker failures after recovery and private rebuild livelock under corrections.
- Record measurements and outstanding gates in the [rollout record](../live-activity-rollout.md).
  Part 5 remains in progress; production remains explicitly off.

### 2026-10-19 — Part 4: private connection filters

- Add schema 19 transactional membership revisions, candidate-scoped batched joins,
  incremental per-account bitmaps, private cache separation and bounded rebuilds.
- Connect Saved/Interested/followed-artist/followed-venue controls to authenticated
  snapshots/streams and native preferences, with guest explanation and quiet updates.
- Preserve public attribution/counts, later pauses, drafts and owner isolation.
  See the [guide and verification](../live-community-activity.md#private-connections-part-4).
- Pass 25 new and 95 existing browser checks, real-MariaDB race/rollback/replica
  regressions, vet, OpenAPI lint, generated build and Docker `make check`.

Older entries: [archive](archive/live-community-activity/).

### 2026-10-18 — Part 3: specific artists and venues

- Add exact full-lineup/venue predicates, canonical shared views and bounded facets.
- Connect authenticated name pickers, labels, native drafts and independent clears
  on Happening Now/Preferences; preserve guest public IDs, revisions and quiet updates.
- Pass 29 entity, 30 filter and 65 presentation browser checks, real-MariaDB race
  regressions/full suite, vet, OpenAPI lint, generated build and Docker validation. See the
  [guide](../live-community-activity.md#verification); production rollout remains Part 5.

### 2026-10-18 — Toasts and Happening Now presentation checkpoint

- Implement the native `/happening-now` snapshot/Refresh/filter page, all navigation
  layouts and bounded eight-second toasts with shared preferences and one stream.
- Preserve hide/pause/disable, quiet baselines, first-name-only attribution,
  immediate redaction/expiry, explicit updates, independent counts and Overview/Back.
- Pass 65 presentation and 30 filter/draft/count browser checks against isolated
  MariaDB, whole-page axe, responsive/enlarged-text/focus checks, race suite, vet
  and Docker `make check`.
  See the [current guide](../live-community-activity.md#verification). No new migration
  or production configuration change; rollout/manual-device gates remain Part 5.

### 2026-10-17 — Toasts and Happening Now specification

- Revise the [interface contract](../design/live-community-activity.md#interface-and-notification-controls)
  to replace above-content activity with bounded top-right toast notifications.
- Specify a **Happening Now** navigation page for recent public recommendations,
  with shared filters, native browsing, accessible dismissal and preserved pauses.

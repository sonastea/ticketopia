# Live community activity changes

## Unreleased

### 2026-10-20 — Part 5: operational measurement and deployment fixture

- Add bounded, identity-free metrics for caches, work, SQL statements, timing,
  failures and delivery; reuse fresh counts for joining tabs and bound count reads.
- Bound the aggregate delivery cycle and fix inactive-account membership rebuilds
  after coalesced invalidation, retaining per-account failed-read backoff.
- Enforce the unreferenced-cohort cap across private membership revisions too.
- Profile warm reads, cache complete-window expiry, and reuse revision-matched
  facets without reusing attribution; improve contended warm stream delivery.
- Add a production-image three-replica/nginx load and SQL-recovery/drain fixture.
- Pass the rich 1,000-group/256-account/768-stream scenario and final regressions;
  retain the failed 10,000-group write-latency gate (+37.4% versus +10%) explicitly.
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

### 2026-10-16 — Part 2: complete-window city and event-type filters

- Add lightweight complete-window projection, stable bitmap slots, canonical shared
  views, coalesced snapshots, immediate warm activity and bounded unavailable states.
- Schema 18 persists city/category filters and projects titles without resetting
  notification choices; reuse Community's literal MariaDB location collation.
- Connect guest-local and conditional account-synced Apply/Reset, confirmed summaries,
  explicit empty states, browser-filter reuse and native account controls.
- Preserve independent counts, Overview/Back, draft/participation state and quiet
  changes. Artist/venue, personal and radius filters remain explicitly unsupported.
- See the [implemented guide](../live-community-activity.md#city-and-event-type-filters-part-2)
  for bounds, local verification and populated-window baseline. Production stays off.
- Pass 25 filter browser checks plus the existing 64-check live journey, real
  MariaDB race regressions/full suite, vet, OpenAPI lint, generated builds and final
  Docker validation. Record 100/1,000/10,000-group baselines and fail-closed overflow;
  mark Part 2 complete locally, leaving Parts 3–6 unchecked.

### 2026-10-16 — Part 1: default-on accounts and live preferences

- Add dependency-aware availability and preserve explicit deployment kill switches.
- Schema 17 enables notifications once for existing accounts and defaults new
  accounts/live activity on, with revisioned preferences and pause provenance.
  Channel permissions, subscriptions and delivery receipts remain independent.
- Add owner-only conditional preference APIs and native enable/disable controls,
  authenticated all-events snapshots/streams, batched credential/settings checks,
  immediate local revocation, known-expiry closure and explicit paused states.
- Preserve browser hiding, quiet reconnect, fallback and counts. Reject unfinished
  filters. Production migration/rollout remains Part 5; see the [guide](../live-community-activity.md).
- Verify real migration/rerun and credential-lifecycle tests plus 64 production-route
  browser checks, including upgraded paused/unpaused accounts, native/JS-disabled
  forms, conflict recovery, account switching, fallback and automated accessibility.
- Pass the real-MariaDB race suite, vet, OpenAPI lint, generated build and Docker
  validation; mark Part 1 complete locally. Later milestones remain unchecked.

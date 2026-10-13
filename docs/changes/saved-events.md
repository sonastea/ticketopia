# Private saved-event changes

## Unreleased

### 2026-10-12 — Action feedback and collection removal

- Keep surviving collection rows, reading position and keyboard focus mounted;
  show event-named confirmations and focused newest-page/empty-state recovery.
- Explain uncertain transport/response failures with safe retry; retain confirmed
  Save/Interested choices and independent public recommendations. No undo is added.
- Add shared Chromium/WebKit/native SQL-fixture journeys and truthful later-page
  rendering checks. See the [contract and fixture command](../saved-events.md#action-feedback-and-collection-removal).
  Verify 548 Chromium/WebKit checks and 22 clean axe views, existing behavior
  regressions, the full isolated-MariaDB race suite, host vet and `make check`.
  Physical-device and manual screen-reader verification remain open.

### 2026-10-06 — Durable private saves

- Deliver Save/Remove on discovery rows, contextual previews and full events,
  plus a newest-saved-first private `/saved` collection with keyset pagination.
- Share owner-only, idempotent browser/bearer APIs, preserve original save times,
  use CSRF/origin checks, and exclude bookmarks from public profiles/discovery.
- Atomically store durable identity, mapping, normalized last-known snapshot and
  bookmark in schema v3; retain details after cache/provider loss without claiming
  scheduled ingestion, image-binary archival or dated observations.
- Preserve native forms, synchronize enhanced controls and revalidate restored
  session state. Keep Interested/recommendations/reminders explicitly unavailable.
- Verify real-MariaDB concurrency/ownership/rollback/restart/migration/pagination,
  and 86 browser checks with ten zero-violation accessibility views including
  320–390px, tablet/desktop, 200% text, native forms, multiple clients and fallback.
- Document enablement, drain/migrate/grants/start upgrade and test harness in
  [private saves](../saved-events.md). The running application database was not
  migrated; live Google consent and production rollout remain operator tasks.
- Pass the real-MariaDB Go race suite, vet, production asset/application build,
  and OpenAPI lint with only the two existing health-probe warnings.
- Diagnose a local post-migration Save failure caused by missing snapshot/bookmark
  runtime grants; reapply the scoped grants and verify save/remove with a rolled-back
  transaction that leaves no records. Add [troubleshooting](../saved-events.md#save-fails-after-a-schema-upgrade).
- Prevent pending/success/error feedback from moving Save relative to Interested
  or Details. Share a controls-row/feedback layout across discovery, previews,
  full events, Saved and loaded results; add rendering and responsive browser
  regressions, including long messages and 200% text. Record the reusable
  [action layout contract](../ui-components.md#theme-and-coverage).
- Let adjacent event-section links wrap on narrow, enlarged-text layouts instead
  of overflowing the event preview or full page; constrain Load more's minimum
  width and allow filter-summary metadata to shrink without displacing its icon
  at 200% text.

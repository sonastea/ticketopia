# Live community activity changes

## Unreleased

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

### 2026-10-16 — Default-on filtering specification

- Specify default-on live activity and the agreed one-time migration enabling
  account notifications for existing accounts, including previously paused ones.
  Later pause choices persist; email permissions and subscriptions stay independent.
- Specify event-city/type/artist/venue and recipient saved/interested/followed
  filters, with radius as a later coordinate-backed extension.
- Define full-window shared indexing, reusable filter views, batched incremental
  membership reads, explicit overload behavior and three-replica load gates.
- Divide implementation into six runnable checkpoints: defaults/preferences,
  city/type filtering, artist/venue filtering, private connections, production
  readiness and optional radius filtering, each with a demo and exit checks.
- Link the [proposed extension](../design/live-activity-filters.md). Runtime defaults,
  routes and filters remain unchanged; implementation milestones are unchecked.

### 2026-10-15 — Live community activity

- Specify sitewide live public recommendations and aggregate event-count refreshes,
  grouped and dismissible, with SSE snapshots backed by shared MariaDB state.
- Choose an optional, explicit activity first name with Someone as the fallback;
  omit surnames, display names and email fragments from notification payloads.
- Define atomic eligibility/withdrawal, anti-repeat behavior, expiry, replica
  delivery, pause controls, proposed API contracts and acceptance checks.
- Implement schema 16, optional owner-only first-name profile validation, transactional
  eligibility/removal, current grouped projection, batched counts and shared per-replica
  SSE delivery with bounded connections, rate limits, deadlines and shutdown.
- Add the shell disclosure, explicit updates, immediate redaction/withdrawal, local
  hide, account pause, watchdog/reconnect, JSON fallback and count-only reconciliation.
- Add real MariaDB lifecycle/rollback/replica tests and production-shell browser
  journeys. Keep target-proxy/load, physical-device and manual screen-reader checks
  as rollout work. See the [feature guide](../live-community-activity.md) and retained
  [implementation specification](../design/live-community-activity.md).
- Verify the full real-MariaDB race suite, vet, OpenAPI lint, generated build,
  final Docker `make check` and 49 browser checks including automated accessibility.

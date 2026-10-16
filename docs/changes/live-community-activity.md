# Live community activity changes

## Unreleased

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

# Followed discussion changes

## Unreleased

### 2026-10-08 — Private conversation follows and reply notifications

- Follow/unfollow individual conversations; choose immediate, UTC daily/weekly,
  or muted in-app updates and respect account-wide pause.
- Persist retry-safe notification envelopes atomically with new replies; provide
  owner-only collections, read controls and exact off-page reply links.
- Recheck root/reply visibility at delivery; never copy contribution text or private
  report/moderation evidence. No report/decision notifications, email or push.
- Add schema 12, aligned runtime grants/probes and OpenAPI 1.12.0. See
  [followed discussions](../followed-discussions.md) for behavior, upgrade and checks.
- Verify the real-MariaDB race suite, queue rollback/upgrade/pagination tests,
  vet/build/OpenAPI lint, 52 browser outcomes and six clean axe scans, plus 24
  existing discussion regression checks. The independent review scores the design-
  documentation fix resolved: ship for that fix only, not public-pilot approval.

# Followed discussion changes

## Unreleased

### 2026-10-29 — Batched reply fan-out and followed-conversation pages

- Enqueue reply notifications with one recipient read plus two multi-row writes per
  500 followers, replacing three writes per follower inside the root-locked
  transaction; reopening read digests now happens in the envelope upsert.
- Load each followed-conversations page, roots included, in one query using the
  shared post projection (visibility, reply counts and Helpful state unchanged).
- Add real-MariaDB statement caps (1,000 followers: at most 11 statements, was
  2,107; a 100-follow page: 1, was 101) and a 1/100/1,000-follower publish
  benchmark: local M1 medians fell from 8.1/191/1,864 ms to 7.6/11.7/41 ms.

### 2026-10-12 — Scannable conversations and reply updates

- Share Updates navigation; lead followed records with event context, actual
  reply count and readable frequency. Prioritize Open conversation over read/tools.
- Label Available UTC times and textual read state; retain distinct conversations,
  exact reply links, visibility filtering, frequency changes and native Mark read.
- Verify 87 Chromium checks/14 clean axe views, including 200%-text action wrapping;
  see [followed discussions](../followed-discussions.md) and the
  [phase-3 record](../../.impeccable/surfaces/views-layouts-account-nav-templ.md).

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

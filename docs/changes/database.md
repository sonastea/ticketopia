# Database and persistence changes

[Older history](archive/database/).

## Unreleased

### 2026-11-23 — Read indexes, schema v26

- Schema 26 adds `discussion_visible_roots` (previews and community lists rank
  visible roots from the index), a virtual `event_observations.has_changes` with
  `observation_changes` (history reads 5 changed rows instead of all 2,001 in a
  test), and rebuilds `detail_due` to match the claim's order (one entry read,
  no backlog sort). No grant changes. See [persistence](../persistence.md#rolling-update-compatibility).

### 2026-11-22 — Same question marks, schema v25

- Schema 25 adds `discussion_same_question` (one mark per account and root
  post). Runtime needs INSERT and DELETE on it, in every provisioning path and
  startup probe. Open questions are computed from replies and `events` with no
  new columns. See [persistence](../persistence.md).

### 2026-11-15 — Discussion drafts and post attribution, schema v24

- Add nullable `discussion_posts.via_client_name`/`via_client_host`, the app
  whose draft the author published, and `agent_drafts.thread_id`/`parent_id`
  for reply drafts. Additive and nullable: existing table-level runtime grants
  suffice. Drain v23 replicas, migrate, then start v24. See
  [persistence](../persistence.md) and
  [agent publishing](../app-mcp.md#publishing-in-your-name).

### 2026-11-14 — Public activity timeline index, schema v23

- Add `discussion_posts.discussion_author (account_id, created_at, post_id)` so
  `GET /api/v1/users/{id}/activity` reads a person's visible posts newest first.
  The timeline merges posts, active recommendations and public interest through
  owner-leading indexes, then reads each page row's snapshot by primary key.
- Index only: no columns, rows or grants change. Drain v22 replicas, migrate,
  then start v23. See [persistence](../persistence.md) and
  [community activity from other clients](../accounts.md#community-activity-from-other-clients).

### 2026-10-14 — Reproducible query baseline, schema v15

- Add deterministic 1k/10k and explicit 100k workloads, community/coverage/write
  benchmarks, fixed read clocks and opt-in actual-query JSON plans. Evaluate
  candidate indexes in disposable schemas; put coverage scope equality columns
  before dates and remove the duplicate discussion index, with fresh/upgrade/repeat
  migration coverage and unchanged runtime grants.
- See the [workflow and measurement report](../query-performance.md).

### 2026-10-14 — Indexed community filters, schema v14

- Replace per-read JSON location/category predicates in recommendation feeds and
  discussion lists/counts with indexed snapshot locations and category facets.
  Preserve first-venue/per-field fallback, literal/case-insensitive locations,
  binary categories, deduplication, and winning-snapshot freshness.
- Backfill existing snapshots/categories; require drained v13 traffic before
  migration. See [persistence](../persistence.md#durable-identity-boundary).

### 2026-10-08 — Fail-fast runtime permissions

- Validate every required runtime write before HTTP startup with zero-row,
  rolled-back probes bounded by the startup deadline. Identify missing operation/
  table grants without leaking credentials; keep migrations schema-only.
- Remind operators to reconcile runtime grants in the migration success log.
- Align startup, local SQL/operator grants and setup checks in database-free
  contract tests; verify missing grants, recovery, no data changes and deadlines
  on isolated MariaDB. See [permission validation](../persistence.md#startup-permission-validation).

### 2026-10-07 — Private moderation and durable roles schema v8

- Add hiding/review versions, private reports, append-only decisions and author requests.
- Keep role membership/audits runtime-read-only; add audited operator grant/revoke CLI.
  See [moderation](../moderation.md) and [upgrade/grants](../persistence.md).
- Verify v7 preservation, races/rollback, privacy/restart and immediate role revocation;
  require drained old traffic. Leave the running database and real role grants unchanged.

### 2026-10-07 — Recommendation feed and observation schema v7

- Retain private withdrawn publication markers with cleared reasons; backfill
  immutable event feed positions from earliest surviving endorsements.
- Add one observe-only window/totals row per account, committed with publication
  or reactivation, plus runtime/operator grants and schema validation/probes.
- Require drained old traffic and clean schema 7; leave the development database
  unchanged. See [recommendations](../event-recommendations.md) and [persistence](../persistence.md).
- Verify schema-6 backfill, original positions across restart/reactivation, bounded
  counters/reset, concurrent cross-pool increments and atomic rollback in real MariaDB.

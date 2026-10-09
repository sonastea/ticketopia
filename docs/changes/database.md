# Database and persistence changes

[Older history](archive/database/).

## Unreleased

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

### 2026-10-07 — Discussion schema v6

- Add posts with immutable ancestry, durable creation keys/fingerprints, soft
  removal, and unique Helpful reactions, plus least-privilege grants/probes.
- Require clean schema 6 and drained schema-5 traffic before migration. Preserve
  prior accounts/activity/snapshots; leave the running development schema unchanged.
  See [discussions](../event-discussions.md) and [persistence](../persistence.md).
- Verify concurrent retries/Helpful across pools, ancestry, removed reply context,
  restart/provider loss, filtered keysets, schema-5 preservation, atomic rollback,
  and DB-clock read-after-write boundaries with the full real-MariaDB race suite.

### 2026-10-06 — Account/save schemas, runtime grants, and server configuration

- Add schema v2 for accounts/issuer-subject identities, hashed credentials,
  browser-bound one-time flows, and shared rate counters, with scoped local/operator
  grants and full startup schema checks; see [accounts](../accounts.md).
- Add schema v3 last-known event snapshots and owner-only bookmarks, transactional
  identity/activity writes, scoped local/operator grants and startup validation.
  Verify v2 account/credential/event preservation in isolated migration tests;
  require draining v2 apps before migration. See [private saves](../saved-events.md).
- Add schema v4 independent event interest with private/public visibility, scoped
  runtime/operator grants and write probes. Share transactional event snapshots
  without merging save/interest records; verify migration preservation, rollback,
  independent pools, keysets and provider-free restart. See [interest](../event-interest.md).
  Require draining v3 apps before migration; the current development database is unchanged.
- Add schema v5 public recommendations with unique account/event keys, stable
  publication/edit times, bounded reasons, and scoped runtime/operator grants/probes.
  Verify atomic writes/rollback, independent pools, offline edits/restarts, filtered
  binary keysets, and v4 activity/session preservation; drain v4 apps before upgrading.
  Leave the running development database unchanged; see [recommendations](../event-recommendations.md).
- Add local `make db-setup` (originally `make migrate`) to prepare TLS/Compose/users,
  run current-source migrations, reapply runtime grants and verify runtime access
  with zero-row write probes.
  Pin migration connection settings to local fixtures; stop on any failed step.
  Verify repeat runs locally, fail-fast ordering/configuration isolation tests,
  missing-grant detection in isolated MariaDB, the full Go race suite and vet.
  See [local development](../persistence.md#repeatable-local-development).
- Make `make migrate` migrations-only using existing environment/`.env` settings;
  move the combined local workflow to `make db-setup` and `mariadb-setup.sh`.
  Test configured connection preservation without Docker/OpenSSL, migration failure
  propagation, and combined setup ordering/fail-fast behavior; see
  [local development](../persistence.md#repeatable-local-development).
- Document the required v1 traffic drain before migration and v2 restart, rather
  than claiming overlapping schema support or a zero-downtime upgrade.
- Pin local/operator MariaDB images to stable **13.0.2**, after confirming that
  requested 13.1.1 is RC and receiving the user's stable-version choice. Distinguish
  server upgrades from app migrations and preserve existing volumes; see
  [server upgrade guidance](../persistence.md#server-version-upgrades).
- Verify `go test -race -count=1 ./...` against an isolated TLS-enabled 13.0.2
  server, the migration CLI/repeat run, runtime grants, and Compose configuration.
  Initially leave the 11.8.6 instance/data untouched; no in-place or cluster upgrade
  is claimed.
- Upgrade the local Compose server to 13.0.2 through a private logical backup and
  verified restore into `data-v13`, preserving the old volume and rollback override.
  Match all nine tables and application grants, then verify writable state, runtime
  TLS/schema/readiness, and the full race suite on port 3307. Domain tables were
  empty; populated-data/production restores remain unverified. See the
  [local cutover](../persistence.md#local-1186-to-1302-cutover).
- Add requested local Compose memory, connection, durability, and I/O settings;
  encode fractional MiB sizes as whole bytes and identify two removed, ignored
  compatibility flags. Verify Compose syntax, isolated 13.0.2 startup, and all
  active tuning values with engine rounding; do not restart the existing database.
  Keep the operator budget unchanged; see
  [local development](../persistence.md#repeatable-local-development).

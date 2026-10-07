# Database and persistence changes

[Older history](archive/database/).

## Unreleased

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
- Switch local/operator image pins to **12.3.3**, the latest stable 12.3 LTS patch.
  Retain local tuning and use a separate `data-v12-3` volume to preserve 13.0 data.
  Require a verified logical restore for the engine downgrade; do not restart or
  migrate the running database. Verify isolated TLS-enabled 12.3.3 startup/tuning,
  migrations/repeat run, runtime grants, the full Go race suite, vet, and YAML.
  Preserve the earlier cutover as history; see
  [server-version guidance](../persistence.md#server-version-upgrades).
- Revise local Compose tuning to the requested integer `M` values (`102M`, `51M`,
  `10M`) and remove both obsolete compatibility flags. Verify Compose and all
  17 exact tuning values on isolated 12.3.3; see
  [local development](../persistence.md#repeatable-local-development).

### 2026-10-05 — MariaDB direction and connection/migration/identity foundation

- Replace the SQLite-first/later-PlanetScale direction with MariaDB accessed via
  `database/sql` and `github.com/go-sql-driver/mysql`, with `mariadb-operator` for
  Kubernetes; see the [database plan](../design/database.md).
- Define shared primary routing for three stateless app replicas, independently
  selected database HA, bounded pools, verified TLS, serialized migrations,
  cross-pod job/budget coordination, and backup/restore verification.
- Align root goals, delivery tasks, API architecture, cache/discovery notes, and
  deployment guidance with the selected stack and delivered foundation.
- Deliver explicit database-free/MariaDB modes, bounded pools/deadlines, verified
  TLS, UTC/strict sessions, fail-closed startup/schema validation, SQL readiness,
  redacted errors, and pool cleanup after HTTP shutdown.
- Add a shell-free `ticketopia migrate` subcommand with embedded pinned Goose
  migrations, separate DDL credentials, advisory-lock serialization, and a durable
  failed-DDL guard. Document inspection/recovery and schema-version compatibility.
- Add minimal event/provider identity with case-sensitive keys, atomic concurrent
  upserts, retained categories/optional artists, stable public IDs, and shared service
  boundaries without changing discovery reads or adding mutation endpoints.
- Add repeatable TLS-enabled local MariaDB and standalone operator/Secret/Job/
  three-app-replica examples with surge/draining capacity budgets. No cluster/HA/
  backup deployment is claimed; broader persistence milestones remain unchecked.
- Verify real MariaDB migrations, partial DDL/recovery, runtime privilege denial,
  multi-pool identities, metadata/UTC/TLS, cache-independent restarts, startup and
  readiness failures/recovery, shutdown, and redaction; see the [foundation guide](../persistence.md).
- Pass `go test -race ./...` with real MariaDB, `go vet ./...`, `npm run build`,
  the Docker build, and non-root/read-only migration/application image smoke checks.

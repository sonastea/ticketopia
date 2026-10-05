# Database and persistence changes

## Unreleased

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

### 2026-10-03 — Local event identity and independent community records

- Extend the [database plan](../design/database.md#planned-event-and-community-model)
  with bookmark/interest/recommendation/post-reaction separation, thread ancestry,
  visibility, retry-safe uniqueness, and durable event references across cache or
  provider loss. Preserve SQLite-first, single-host, and later migration decisions.

### 2026-10-02 — SQLite first, with a later PlanetScale option

- Select SQLite as the initial database for planned radar, community, event
  history, and notification state.
- Document a single-host operating model, durable storage, and migration-aware
  persistence boundaries in the [database plan](../design/database.md).
- Record PlanetScale as a later option, with workload/deployment triggers and
  an explicit engine and data migration rather than a connection-URL swap.
- Align the API draft and project overview with the database decision.

The older entries record design history, not delivered account/community storage.

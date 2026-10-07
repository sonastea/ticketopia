# MariaDB foundation

Implemented: explicit SQL connection lifecycle, serialized embedded migrations,
schema validation/readiness, minimal durable event/provider identity, accounts,
credentials/private preferences, schema-v3 [private saves/snapshots](saved-events.md),
and schema-v4 [event interest](event-interest.md). Discovery
still reads its existing cache/provider services and does **not** write SQL. There
are no community mutations, ingestion scheduler, global provider
budgets, database HA, or verified production backup/restore in this slice.

The [database plan](design/database.md) owns the broader direction. Code lives in
[`internal/persistence`](../internal/persistence/), shared durable services in
[`internal/events`](../internal/events/), and lifecycle wiring in
[`internal/ticketopia/main.go`](../internal/ticketopia/main.go).

## Explicit modes and configuration

`PERSISTENCE_MODE=disabled` (also the unset default) preserves database-free
discovery. `PERSISTENCE_MODE=mariadb` requires valid connection settings and a
clean supported schema **before HTTP listens**. A failure exits nonzero within
`DB_STARTUP_TIMEOUT`; there is no fallback to memory and no startup migration.

| Setting | Default / requirement in MariaDB mode |
| --- | --- |
| `DB_HOST`, `DB_NAME` | Required hostname/IP and database name (ASCII letters/digits/underscores, max 64) |
| `DB_PORT` | `3306` |
| `DB_USER`, `DB_PASSWORD` | Required runtime credentials |
| `DB_MIGRATION_USER`, `DB_MIGRATION_PASSWORD` | Required by `ticketopia migrate` only; a different user |
| `DB_TLS_MODE` | `verify-full`; only `disabled` is also accepted, for isolated local development |
| `DB_TLS_CA_FILE` | Required mounted PEM CA bundle with `verify-full` |
| `DB_MAX_OPEN`, `DB_MAX_IDLE` | `10`, `2`; open 1–100, idle 0–open |
| `DB_CONN_LIFETIME`, `DB_CONN_IDLE_TIME` | `5m`, `1m`; maximum `1h` |
| `DB_DIAL_TIMEOUT`, `DB_READ_TIMEOUT`, `DB_WRITE_TIMEOUT` | `3s`, `5s`, `5s`; maximum `30s` each |
| `DB_STARTUP_TIMEOUT` | `10s`; maximum `1m` |
| `DB_QUERY_TIMEOUT` | `3s`; maximum `30s` for an entire repository operation, including retries |
| `DB_READY_TIMEOUT` | `500ms`; maximum `2s` |
| `DB_MIGRATION_TIMEOUT` | `2m`; maximum `10m`, including advisory-lock wait |

Durations use Go syntax and must be at least `1ms`. Migrations use their own
two-connection pool regardless of runtime pool settings. DDL still has the
configured socket read/write limits; raise them (up to `30s`) for planned operations,
and use separately reviewed operational procedures for longer DDL.

Connections use pinned `go-sql-driver/mysql` **v1.10.1**, `mysql.Config` and
`sql.OpenDB`, not hand-built DSNs. Driver parsing uses `parseTime`/UTC separately
from the session's `time_zone='+00:00'`; examples also set the server default to
UTC. SQL uses strict mode, utf8mb4, InnoDB, and microsecond UTC instants. Known
local date/time, time-zone name, and TBA/TBD flags remain separate nullable fields.

Verified TLS checks both the mounted CA and `DB_HOST` against the server's SANs;
there is no skip-verify or plaintext fallback. Keep credentials out of build args,
Git, logs, and command-line DSNs. Driver errors are reduced to operation names,
context failures, and numeric MariaDB codes; raw server messages are not exposed.
CA/credential files are read at pool creation: roll/restart pools when rotating
credentials or trust bundles, keeping overlapping CAs during rotation.

`/healthz` never checks dependencies. Enabled `/readyz` makes bounded schema/version
queries, returning 503 on database unavailability, dirty/unsupported schema, or
shutdown; subsequent successful checks recover to 200. Disabled readiness remains
dependency-free. SQL pools close **after** HTTP drains (five seconds maximum;
forced HTTP close on timeout), not at the beginning of SIGTERM handling.

## Repeatable local development

Requires Make, Go 1.27+, Docker Compose and OpenSSL. These credentials/certificates
are disposable public development fixtures, **not** production secrets. The server binds only
`127.0.0.1:3307`, uses MariaDB **12.3.3 LTS**, and stores its data in the versioned
`<compose-project>_data-v12-3` volume (normally `ticketopia-db_data-v12-3`).
For an existing 11.8 or 13.0 database, read [server upgrades](#server-version-upgrades)
before starting the updated Compose image.

Compose sets a 512 MiB InnoDB buffer pool, 128 MiB redo log, 60-connection limit,
and explicit buffer/I/O tuning in [`compose.yaml`](../deploy/mariadb/compose.yaml).
MariaDB's `M` suffix means MiB. Key, temporary-table, and InnoDB log buffers use
`102M`, `51M`, and `10M` respectively. Removed compatibility-only settings
`innodb_buffer_pool_instances` and `innodb_thread_concurrency` are not passed.
Per-session buffers can multiply across connections; these settings are **not**
a total server memory cap.
The standalone operator example retains its separate 100-connection budget.

```sh
make db-setup
export PERSISTENCE_MODE=mariadb DB_HOST=localhost DB_PORT=3307 DB_NAME=ticketopia
export DB_TLS_CA_FILE="$PWD/.local/mariadb/certs/ca.crt"
export DB_USER=ticketopia_runtime DB_PASSWORD=local-runtime-only
export TICKETMASTER_KEY=your-api-key
go run ./cmd/ticketopia
```

`make migrate` runs `go run ./cmd/ticketopia migrate` only. It uses the existing
environment/`.env` connection and TLS settings, requires `PERSISTENCE_MODE=mariadb`
and separate `DB_MIGRATION_USER`/`DB_MIGRATION_PASSWORD`, and expects an already
provisioned, reachable database. It does **not** create TLS fixtures, start/recreate
containers, provision users, apply runtime grants, or check runtime access. Reapply
the relevant grants separately after adding tables. For this local setup, with the
connection settings above exported:

```sh
export DB_MIGRATION_USER=ticketopia_migration DB_MIGRATION_PASSWORD=local-migration-only
make migrate
```

`make db-setup` runs [`scripts/mariadb-setup.sh`](../scripts/mariadb-setup.sh):
prepare local TLS fixtures, start/wait for Compose MariaDB (up to 60 seconds),
reapply local database/user provisioning, run the current-source migration CLI,
apply all runtime grants, then check TLS/read access and every required write
permission as the runtime user. The [`runtime check`](../deploy/mariadb/runtime-check.sql)
uses zero-row writes in a rolled-back transaction; it creates no application data.
Any failed step stops the command without reporting readiness.

The combined `db-setup` target is deliberately pinned to `localhost:3307/ticketopia`
and the public Compose credentials, overriding inherited and `.env` database
connection settings for migrations. It is **local development only**, not a
production deployment tool.
It does not reset data, rotate existing credentials/certificates, stop the app, or
configure the app's runtime environment. Stop/drain the app before schema-changing
migrations; follow [upgrade compatibility](#rolling-update-compatibility). Rerunning
the target is safe on a clean schema and repairs missing grants even when no
migrations are pending. Run it after pulling/adding migrations, not every restart.

Initialization SQL runs only on a **new** MariaDB volume. Reapplying
`deploy/mariadb/init.sql` as the local admin is safe if user provisioning was
interrupted. The final table-level grants intentionally run **after** migration:
MariaDB refuses them while those tables are absent. Runtime can select the schema
and events, and modify only explicitly granted domain tables. Account identity
mappings are insert-only; credentials and short-lived auth rows allow deletion for
revocation/cleanup. Runtime cannot perform DDL, delete accounts/events, or modify
migration state. Migration privileges are limited to this database,
without user administration or grant option.

Repeat `make db-setup` safely. It can recreate the container when Compose changes,
but retains the configured volume. `docker compose ... stop` keeps data; do
not use `down -v` unless you deliberately want to erase this local database.
Certificates last 30 days. To renew local fixtures, replace the three generated
certificate/key files intentionally, rerun the script, and restart MariaDB/apps.
Only `ca.crt`, never CA private keys, belongs in application mounts.

## Server-version upgrades

Compose and operator examples pin **MariaDB 12.3.3**, the latest stable patch of
the [12.3 LTS series](https://mariadb.com/docs/release-notes/community-server/12.3/mariadb-12.3-changes-and-improvements),
maintained until June 2029. The separate `data-v12-3` local volume prevents this
image from opening either `ticketopia-db_data` (11.8) or `ticketopia-db_data-v13`
(13.0). Without an explicit restore, starting updated Compose creates a
**separate empty database**, not an automatic data migration. Engine changes are
separate from Ticketopia's application schema migrations.

Moving from 13.0 to 12.3 is an **engine downgrade**. Never mount a 13.0 data
directory or PVC in 12.3. Preserve a backup and drain writers before a logical
export of application data; restore into fresh 12.3 storage and check SQL feature
compatibility, contents, grants, TLS, schema validation, and readiness before
cutover. Provision users separately; do not import the newer engine's system
tables. Keep the 13.0 volume intact until the restore is verified. The 12.3 change
updates configuration only; the running 13.0 server was not stopped or migrated.

Before changing an existing 11.8 server or PVC, preserve a backup and rehearse
the [MariaDB server upgrade path](https://mariadb.com/docs/server/server-management/install-and-upgrade-mariadb/upgrading/mariadb-community-server-upgrade-paths)
and any required `mariadb-upgrade`/system-table work on a separate copy. Do not
assume changing the image alone is safe or that an older image can reopen an
upgraded data directory. For local development, a fresh separate Compose project
and volume can leave the old database intact; a logical export/import also needs
compatibility checks. Never erase the existing volume simply to change versions.

In Kubernetes, review the pinned operator's compatibility and server upgrade
procedure, test storage/restore and readiness in staging, and plan any required
downtime before updating the database resource. The full Go race suite, TLS-enabled
SQL integration tests, migration CLI/repeat run, and local runtime grants were
verified on an isolated fresh 12.3.3 server. This does not establish an in-place
engine upgrade, a cluster rollout, or a production restore.

### Local 11.8.6 to 13.0.2 cutover

The local Compose server was upgraded on 2026-10-06 using a logical export/restore,
not an in-place data-directory upgrade.

This is a historical cutover record. Current Compose targets 12.3.3 and does not
mount the retained 13.0 volume; these steps describe the earlier 13.0 configuration:

1. Check the source version, database inventory, clean schema, and active writers.
   Pause application writes/migrations; source `read_only=ON` also blocks ordinary
   runtime users, but does not block administrative writers or replace draining.
2. Keep a private `mariadb-dump --single-transaction --routines --events --triggers
   --hex-blob --databases ticketopia` export. Preserve application users' `SHOW CREATE
   USER` and `SHOW GRANTS` separately; they contain credential hashes and need the
   same protection as account data. Do **not** import old MariaDB system tables or
   internal users into the new engine.
3. Restore into a fresh 13.0.2 volume/server on a separate loopback port. Compare
   all table row counts and deterministic content hashes, application grants, TLS,
   schema validation, and runtime readiness before switching.
4. Stop both servers, then start the 13.0.2 Compose configuration with `data-v13`
   on port **3307**.
   Recheck version, writable status, restored contents, runtime readiness, and tests.
   If abandoning preparation, restore the source's prior writable status.

The completed local restore matched all nine tables and both application users'
grants; the full Go race suite passed against the upgraded server. The seven domain
tables were empty at export, so this does **not** establish a populated-account
restore or production backup policy. The original `ticketopia-db_data` volume is
retained unchanged and is not mounted by normal Compose.

Private local artifacts live in a dated directory under `.local/mariadb/backups/`:
`ticketopia.sql`, `application-users.sql`, comparison metadata, and `rollback.yaml`.
SQL/metadata files are owner-only, the directory is owner-only, and `.local/` is
Git-ignored. Keep the backup and old volume until satisfied with the new server.

To check the running version:

```sh
docker compose -f deploy/mariadb/compose.yaml ps
docker compose -f deploy/mariadb/compose.yaml exec -T -e MYSQL_PWD=local-root-only mariadb mariadb -uroot -e 'SELECT VERSION(), @@read_only;'
```

For rollback of that historical cutover, **pause writers first** and set `BACKUP`
to the successful upgrade's directory. The saved override selects the original
11.8.6 image and volume:

```sh
BACKUP=.local/mariadb/backups/YOUR_UPGRADE_DIRECTORY
docker compose -f deploy/mariadb/compose.yaml -f "$BACKUP/rollback.yaml" config --quiet
docker compose -f deploy/mariadb/compose.yaml -f "$BACKUP/rollback.yaml" up -d --wait
```

This restores the **pre-cutover snapshot** only: later writes on 13.0.2 or 12.3.3
will not be present. Preserve/export those separately before deciding to roll back.
Do not point 11.8 at either newer volume or remove volumes with `down -v`/pruning.

## Explicit migrations and failed-DDL recovery

`go run ./cmd/ticketopia migrate`, `./bin/ticketopia migrate`, and
`docker run ... ticketopia:local migrate` run the same shell-free subcommand.
SQL migrations are embedded and pinned Goose **v3.28.0** runs them with contexts.
A connection-scoped `GET_LOCK('ticketopia:migrate:<database>', ...)` serializes
the entire operation, including version-table initialization. The command polls
the lock with a bounded context, releases it on exit, and discards an unusable
session if unlock fails. This coordinates independent processes/pools, not just
goroutines in one app. Runtime startup only validates; no DDL is issued.

`schema_state` is an InnoDB guard, separate from `goose_db_version`. Before Goose
applies pending migrations, the command durably sets `dirty=true` and the target
version; only complete success clears it. Migrations explicitly use `NO
TRANSACTION` because MariaDB commits DDL implicitly. A failed command or interruption
can leave tables/columns created **even though Goose has not recorded that version**.
Both subsequent migration commands and runtime startup refuse a dirty schema.

Recovery is a reviewed operator action, not automatic rollback or a blind retry:

1. Stop release automation and database-backed app traffic; preserve database data
   and migration logs. Do not repeatedly restart a failed Job or force a version.
2. With admin/migration credentials in one MariaDB session, acquire
   `SELECT GET_LOCK('ticketopia:migrate:ticketopia', 30);` and verify result `1`.
3. Inspect `schema_state`, `goose_db_version`, `SHOW CREATE TABLE`, and the exact
   embedded migration from the failing image. Identify every committed statement.
4. For an empty/disposable initial install, remove **only** verified partial objects
   from that attempt, in foreign-key order. For populated databases, preserve data
   and devise an explicit repair/forward migration. If completing DDL manually,
   reconcile Goose history only after verifying every statement and required data
   transformation; never mark incomplete DDL as applied.
5. Once actual schema and recorded history agree, clear only the guard with
   `UPDATE schema_state SET dirty=FALSE WHERE id=1;`, release the same session's lock
   (`SELECT RELEASE_LOCK('ticketopia:migrate:ticketopia');`), then run the migration
   command again. It applies whatever versions remain pending.
6. Confirm runtime schema validation, least-privilege access, and readiness before
   restoring traffic. No automated down/force command is exposed.

### Rolling-update compatibility

This binary supports **clean schema version 4 only**. Versions 0–3 and 5+ are rejected;
partial schemas/missing columns or non-InnoDB tables fail startup. A same-schema
application rollout can run old/new binaries together. Before a future migration,
ship binaries with an explicitly reviewed overlapping supported-version range;
apply additive/expand changes with a serialized Job, then roll compatible apps.
Backfill separately with bounded operations and contract/drop columns only after
old binaries are gone. The previous save binary supports version 3 only.
For the v3-to-v4 interest upgrade, stop/drain v3 traffic, preserve a backup, apply the
v4 migration with the v4 image, reapply/reconcile runtime table grants, then start
v4 apps. Do not leave v3 pods serving after migration; they will fail readiness.
This release does not claim a zero-downtime cross-version rollout. Keep a compatible
rollback image; app rollback does not imply DDL rollback. These guidelines are not
a claim that a three-pod rollout was exercised.

## Durable identity boundary

`events.Service.EnsureDurable` explicitly persists a normalized occurrence for
local actions; `Get` resolves it independently of cache/provider availability.
Private saves compose identity, provider mapping, snapshot and bookmark in one
transaction; discovery reads still do not write SQL or run ingestion. Detail reads
can fall back to a stored snapshot during provider/cache loss without exposing
bookmark state. See [private saves](saved-events.md) for the HTTP/API boundary.

`events.event_id` keeps the existing `ticketmaster:SOURCE_ID` public identity.
`event_providers` has a binary primary key `(provider, source_id)`, an indexed
foreign key to the event, and no cascade delete. `VARBINARY` compares identity
byte-for-byte, including case, independently of the user-content collation. Titles,
dates, venues, and statuses never participate in identity. No title/date matching,
absence-based cancellation, eviction-driven delete, or mapping reassignment exists.

Metadata includes name/source URL, known UTC instant, local date/time/time-zone and
TBA/TBD flags, explicit provider status, venue/place snapshots, optional artists,
and every classification's category/genre/subgenre references. JSON fields hold
the shared normalized model's small arrays/place, not raw provider responses.
This minimal identity record is **not** dated observation history or independent
artist/venue catalogs. Schema v3 additionally stores a complete normalized
last-known DTO in `event_snapshots`, including price/image/sale/description fields,
with its collection time. Image URLs, not image binaries, are stored. Owner-only
`saved_events` reference that snapshot and the account; runtime gets INSERT/DELETE
but not UPDATE on bookmarks, and no DELETE on shared snapshots/events.
Schema v4 adds independent `event_interests` with one row per account/event,
private-by-default visibility, stable interest timestamps, and owner/public/event
pagination indexes. Runtime can INSERT/UPDATE/DELETE interest without changing saves.
Interest composes the same event/snapshot transaction and retained-detail fallback.

Upserts lock the stable event key and insert its provider mapping in one short
InnoDB transaction. Deadlocks/lock-wait timeouts before commit get at most three
attempts within one operation deadline. A transport failure during commit is
uncertain and is **not** automatically replayed; resolve the unique identity before
retrying. Last committed explicit metadata wins. Missing artists are valid, and
only an explicit supplied status can record cancellation.

## Standalone operator example (not deployed)

[`deploy/mariadb/operator`](../deploy/mariadb/operator/) contains database/PVC,
Database/User/Grant CRs, Secret placeholders, enforced TLS, CA mounts, a migration
Job, and a three-replica application example. It targets MariaDB **12.3.3 LTS** and
`mariadb-operator` / CRD charts **26.10.1**:

```sh
helm install mariadb-operator-crds oci://ghcr.io/mariadb-operator/charts/mariadb-operator-crds --version 26.10.1
helm install mariadb-operator oci://ghcr.io/mariadb-operator/charts/mariadb-operator --version 26.10.1 --namespace mariadb-system --create-namespace
```

These are instructions/examples only; **no cluster deployment was performed**.
Review release-specific Kubernetes compatibility/storage classes first. Replace
Secret placeholders using your secret manager, add a `ticketopia-provider` Secret
with `ticketmaster-key`, and provision the namespace-local `ghcr` pull Secret.
Job/apps use the moving `edge` tag with `imagePullPolicy: Always`. Follow the
[production-style staging rollout](deployment.md#production-style-staging-rollout)
for explicit rollout and manual recovery. Avoid changing `edge` between migration
and app rollout; pin both to the same digest when identical artifacts are required.
Provision database/users/migration grants; wait for those resources to be Ready,
then run the Job. Wait for Job success **and** runtime table-grant reconciliation
before rolling applications. Use a new Job name per release; `backoffLimit: 0`
avoids blind failed-DDL retries. App pods never receive the migration/root Secret.

The operator issues TLS certificates and `ticketopia-db-ca-bundle` (`ca.crt`) in
the same namespace. Apps verify `ticketopia-db.ticketopia.svc.cluster.local`, the
single-instance Service. If apps move namespaces, distribute the public bundle
and reconcile rotations; do not copy private CA keys. Restrict database ingress
with NetworkPolicies permitting only app/Job and operator/admin connections; review
actual operator labels, DNS egress, and operational access before applying policies.

Capacity with `max_connections=100`, application pool size 10, replicas 3, surge 1:

- 30 steady application connections; 40 with the active surge pod.
- Reserve up to another 30 for old terminating pods (Kubernetes can exceed surge
  while draining); the runtime user's cap is 70.
- One migration pool: 2. Reserve 20 for operator/admin/monitoring: total **92**.
- Overlapping migration invocations also consume connections while waiting (2 each);
  release automation must limit Job concurrency. Rebudget before adding workers,
  more replicas, larger pools, or longer draining/overlapping rollouts.

All app reads/writes use the same endpoint. This is **one database pod**, not
replication/Galera/MaxScale HA. PVC persistence is not a backup; define and verify
off-volume backups/restoration and any required failover before production claims.
Existing provider budgets/deduplication remain process-local despite shared SQL/KV.

## Verification

```sh
export MARIADB_TEST_ADDR=127.0.0.1:3307
export MARIADB_TEST_CA="$PWD/.local/mariadb/certs/ca.crt"
go test -race ./...
go vet ./...
npm run build
docker build -t ticketopia:local .
sh scripts/mariadb-smoke.sh
```

The opt-in SQL tests create/drop only their own random databases/users using local
root fixtures (override with `MARIADB_TEST_ADMIN_USER`/`MARIADB_TEST_ADMIN_PASSWORD`).
Never point these administrative tests at production/shared data. Without
`MARIADB_TEST_ADDR`, SQL integration tests explicitly skip; ordinary unit/provider
tests still run. The suite tests independent-pool migrations/upserts, advisory-lock
deadlines, committed partial DDL and repair, runtime privilege denial, clean/missing/
unsupported schemas, startup/handshake deadlines, case-distinct identities,
category/artist-less metadata updates, UTC/TLS verification, mapping atomicity,
pool/service restarts and cache removal, unavailable/recovered readiness, closed
pools, shutdown drain ordering, credential redaction, and unchanged discovery reads.

The smoke script runs migration in the non-root/read-only shell-free image, verifies
bad-credential startup failure/redaction, checks UI/assets/probes, **temporarily pauses the local database**, checks readiness 503
without liveness failure, recovers, and restarts/stops the application with exit 0.
It does not remove the database volume or exercise three Kubernetes pods, HA,
historical ingestion, or backup/restore.

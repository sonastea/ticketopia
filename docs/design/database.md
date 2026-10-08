# Database: MariaDB

Status: Connection/migration support, accounts/private preferences/credentials,
private bookmarks/last-known snapshots, independent event interest/visibility, public recommendations,
event discussions/Helpful,
and minimal durable event identity are
implemented, opt-in alongside KV-backed discovery and durable detail fallback. Broader application
storage, scheduled observations, deployed multi-pod operation, HA, and verified
backup/restore remain planned. See the [foundation guide](../persistence.md).

The [account guide](../accounts.md) describes the delivered schema-v2 identity,
sessions, privacy projection, rate counters, and credential lifecycle.

## Decision

Use **MariaDB** for durable application storage, accessed through Go's
`database/sql` and **`github.com/go-sql-driver/mysql`**. Use **`mariadb-operator`**
to manage MariaDB in Kubernetes. This replaces the earlier SQLite-first and later
PlanetScale direction: multiple application hosts writing shared state are a
deployment requirement, not a later migration trigger.

All application replicas connect over TCP to the same logical database. Database
storage belongs to MariaDB, not the application pods. Web, mobile, and other
clients access durable state through the application API, never directly through
database credentials. Use a standalone MariaDB instance for local development
and test persistence against the deployed MariaDB release, not SQLite.

The foundation uses pinned `go-sql-driver/mysql` and Goose, embedded serialized
migrations, and standalone operator examples. The examples have not been deployed
to a cluster; ordinary discovery reads do not ingest events into SQL.

## What MariaDB owns

- Users, preferences, follows, saved events, and reminder settings.
- Events, artists, venues, dated observations, and ingestion coverage.
- Separate event interest, public recommendations, threaded posts, and positive
  post reactions; later attendance states and followed discussions.
- Private reports, moderation decisions, notification jobs, and delivery history.

The existing [KV cache](../cache.md) contains replaceable read results. MariaDB
holds durable application state, including data needed for future venue analysis.
Cache expiry or switching cache providers must not lose saved activity or jobs.

## Planned event and community model

Event/provider identity, identifying metadata, accounts/preferences and private
bookmarks/full last-known snapshots, interest, recommendations, and discussion posts/
Helpful are delivered. Moderation and later participation remain planned.
See [saves](../saved-events.md) and [interest](../event-interest.md). They implement
the [interaction distinctions](../design-guidelines.md#interaction-semantics-and-hierarchy).

| Record | Identity / relationship | Required meaning |
| --- | --- | --- |
| Event + provider mapping | Stable event ID; unique `(provider, source_id)` | Existing `ticketmaster:SOURCE_ID` IDs remain resolvable; never join by title/date alone |
| Bookmark | Unique `(user_id, event_id)` | Owner-only saved collection; no implicit interest or public counts |
| Event interest | Unique `(user_id, event_id)` | Reversible interest; profile visibility private by default, explicit public opt-in |
| Event recommendation | Unique `(user_id, event_id)` | Public endorsement, optional bounded reason, creation/update times; independent of interest |
| Discussion post | `id`, `event_id`, `author_id`, body, timestamps, moderation/removal state | Root post or reply; not an event endorsement record |
| Helpful reaction | Unique `(user_id, post_id, kind)` | Positive post-level reaction; initially only `helpful` |
| Preferences/profile | User-owned identity and category/privacy preferences | Interest preferences do not create event interest; public profile projection excludes private fields |

All local records reference a durable event, never a cache entry. Upsert a minimal
normalized event and its provider mapping before committing the first local
action; do not wait for scheduled ingestion. Preserve it and its contributions
when upstream results expire, omit it, or report cancellation. Distinguish source
unavailability from cancellation. Retain category/segment references for all-category
discovery without requiring artist metadata on non-music events.

For root posts, `root_post_id` and `parent_post_id` are null. Replies retain the
root ID and immediate parent ID, which may be the root or another reply. Enforce
same-event/same-root references, existing parents, and no cycles; ancestry is
immutable. Display at most one visual reply level, with explicit reply targets.
Soft removal keeps IDs and reply structure while public reads withhold body text;
private moderation evidence follows existing access rules. Index root feeds by
`(event_id, created_at, id)` and replies by `(root_post_id, created_at, id)`.

Count interests independently of recommendations and visible root discussions.
Private interest contributes to totals but not public participant identities.
Viewer action state is queried separately from aggregate/public projections;
bookmarks are never inferred into public activity. Index collection reads by user
and event, and recommendations by event/time; use transactions and unique
constraints for retry-safe toggles. Compute bounded-page summaries in batched
queries initially; no separate counter service is necessary.

Future Going/Went attendance records remain distinct from MVP interest and do
not replace bookmarks or imply verified ticket ownership. Discussion follows,
notification jobs, and moderation records retain their previously planned roles.

## Application operating model

- Use InnoDB, foreign keys, unique constraints, and indexes for the actual event,
  discussion, and scheduling queries. Keep write transactions short; fetch
  upstream data and send email outside them.
- Give each process one bounded `*sql.DB` pool. Budget the sum of all pools,
  including rolling-update surge pods, workers, migrations, and operator/admin
  connections, against MariaDB's connection limit. Set connection lifetimes and
  idle limits rather than relying on unlimited defaults.
- Configure dial/read/write timeouts and use context-aware queries with deadlines.
  Build connection settings with `mysql.Config`; never log credentials or full
  DSNs. Require verified TLS in production, trusting the operator's CA bundle and
  checking the database service hostname; do not use `tls=skip-verify` or plaintext
  fallback. Plan pool renewal or a rolling restart when credentials/CAs rotate.
- Use `utf8mb4` for user content, explicit case-sensitive/binary identity columns,
  and strict SQL mode. Preserve case in provider IDs; the default case-insensitive
  collation must not merge distinct source identities.
- Store known instants as UTC with an explicit precision, preserving time-zone
  names and local date/TBA/TBD fields separately. Enable `parseTime` and use UTC
  in the driver; configure server/session time zones separately because `loc=UTC`
  does not set MariaDB's time zone. Use nullable values and exact decimal money.
- Route reads and writes to the same primary endpoint initially. Do not split
  account, saved-event, or community reads onto lagging replicas; people must be
  able to read their own committed changes from any application pod.
- Apply versioned migrations as a serialized deployment Job or local command,
  not from each app pod's startup. Use a migration lock to prevent overlapping
  releases, separate DDL credentials from the runtime user, and keep schema
  changes compatible with old and new app versions during rolling updates.
- Handle transaction deadlocks with bounded retries of retry-safe operations.
  A dropped connection during commit has an uncertain outcome: use unique
  constraints/idempotency records rather than blindly replaying mutations.
- Coordinate jobs with atomic database claims and expiring ownership, or an
  explicitly fenced leader. Process-local locks do not coordinate three pods.
  External request budgets also need shared atomic accounting; the existing
  discovery counters and deduplication remain per process even with shared KV.

## Kubernetes operating model

```text
Ingress -> Service -> Ticketopia Deployment (3 stateless replicas)
                              |
                       MariaDB primary Service
                              |
                    operator-managed MariaDB + PVCs
```

Application replica count and database replica count are independent. Three app
pods can use one standalone MariaDB server, but that is not database HA. If HA is
required, select an operator-supported replication or Galera topology and its
failure/durability guarantees before deploying; do not infer three database pods
from three application replicas.

For a replication topology, one primary plus two database replicas is a possible
HA layout. The operator exposes `<mariadb-name>-primary` and updates it during
primary changes. Each database pod has its own PVC; replication is managed by
MariaDB, not by sharing a data directory. Configure node separation, resource
requests, disruption budgets, replication durability, and automatic failover
explicitly. Semi-synchronous replication is not a blanket zero-data-loss promise;
acknowledgment timeouts and fallback behavior must fit the recovery requirements.

The operator recommends MaxScale for production HA routing and failover. It is
not an assumed Ticketopia dependency; decide whether its operational and licensing
requirements are warranted when selecting the database topology. Service-based
primary routing still needs tested connection recovery and uncertain-write handling.

Pin compatible MariaDB images, operator/CRD/chart versions, and the Go driver
release when implementing deployment. Provision a database and least-privilege
runtime/migration users with operator SQL resources, keep credentials in Secrets,
mount the CA bundle into the application namespace, and restrict network access.
Keep database checks out of liveness. Enabled persistence now uses bounded
database/schema readiness checks; disabled discovery remains dependency-free.

Schedule consistent operator-managed backups to storage outside the database
volumes, with explicit retention and recovery objectives. Verify restoration into
a fresh instance, including credentials, schema, event identities, private data,
and job state; database replicas are not backups. Rehearse primary failover when
HA is enabled and verify access from every app replica after recovery. See the
[foundation guide](../persistence.md#standalone-operator-example-not-deployed) for
example rollout sequencing; backups/failover still require separate implementation and verification.

## Persistence boundaries and verification

- Keep MariaDB queries behind application domain services so HTML/API handlers
  and clients do not depend on SQL rows or driver types. Share ownership and
  visibility rules between clients.
- Define types, nullability, uniqueness, timestamp precision, and money explicitly.
  Keep engine-specific SQL/migrations identifiable; `database/sql` does not make
  SQL dialects or transaction behavior interchangeable.
- Verify concurrent upserts/toggles, thread constraints, atomic job claims, and
  private reads against real MariaDB. Exercise cross-pod read-after-write, app and
  database restarts, migration serialization, rolling updates, and backup/restore
  before checking the [persistence milestones](../goals/delivery.md#2-keep-reliable-event-and-application-history).
- Start with connection/migration support and minimal durable event identity;
  accounts, saves, and community records build on it. Scheduled observation
  history remains a later milestone, not a prerequisite for local actions.

## References

- [go-sql-driver/mysql](https://github.com/go-sql-driver/mysql)
- [Go database/sql](https://pkg.go.dev/database/sql)
- [mariadb-operator](https://github.com/mariadb-operator/mariadb-operator)
- [Operator replication](https://github.com/mariadb-operator/mariadb-operator/blob/main/docs/replication.md)
- [Operator high availability](https://github.com/mariadb-operator/mariadb-operator/blob/main/docs/high_availability.md)
- [Operator TLS](https://github.com/mariadb-operator/mariadb-operator/blob/main/docs/tls.md)
- [Operator physical backups](https://github.com/mariadb-operator/mariadb-operator/blob/main/docs/physical_backup.md)

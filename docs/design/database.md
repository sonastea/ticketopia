# Database: SQLite first

Status: Selected direction for planned persistence. The current application has
a KV cache; durable application storage is upcoming work.

## Decision

Use **SQLite as the initial database** for development and the first deployed
version. Start with one application host and a database on persistent local
storage. Web, mobile, and other clients access it through the application API.

**PlanetScale is the intended option to evaluate later** if measured workload or
operational needs justify a managed database. Its PostgreSQL and MySQL-compatible
Vitess offerings use different database engines from SQLite. Select the target
engine when planning that move; migration will involve schema, query, and data
changes rather than only changing a connection URL.

## What SQLite owns

- Users, preferences, follows, saved events, and reminder settings.
- Events, artists, venues, dated observations, and ingestion coverage.
- Interested/Going/Went states, comments, positive reactions, and followed
  discussions.
- Private reports, moderation decisions, notification jobs, and delivery history.

The existing [KV cache](../cache.md) contains replaceable read results. SQLite
holds durable application state, including data needed for future venue analysis.
Cache expiry or switching cache providers must not lose saved activity or jobs.

## Initial operating model

- Keep the application and background workers on the same host as the database,
  with a durable disk/volume that survives deployments.
- Use a maintained SQLite version, WAL mode, short write transactions, and a
  bounded busy timeout. WAL allows readers alongside a writer; each database
  still has only one writer at a time. Fetch upstream data and send email outside
  write transactions.
- Enable foreign-key enforcement on every connection, use schema migrations,
  and index the actual event, discussion, and scheduling queries.
- Take consistent SQLite-aware backups and verify restoration. In WAL mode,
  copying the main database file alone while it is active is not a complete
  backup strategy.
- Use consistent snapshots or exports for heavier exploratory analysis so long
  analysis reads do not hold up normal database maintenance.

This model can support the initial radar and community goals. Capacity depends
on query patterns, write frequency, transaction duration, and the host's storage.

## Keep a later migration manageable

- Keep persistence queries behind the application's domain services, so API
  handlers and clients do not depend on SQLite-specific SQL or database rows.
- Preserve stable application IDs and source-ID mappings across databases.
- Define types, nullability, uniqueness, timestamp/time-zone handling, and money
  representation explicitly. Validate data instead of relying on SQLite's
  permissive typing.
- Keep engine-specific queries and migrations identifiable. A common Go database
  interface does not make SQL dialects or transaction behavior interchangeable.
- Preserve atomic operations for reactions, attendance updates, moderation,
  and notification job claims when implementing a different storage backend.

The goal is a contained migration that preserves client behavior. Implement
SQLite first and adapt persistence to the chosen PlanetScale engine when needed.

## When to reconsider

Review the database choice when there is evidence of:

- Sustained write contention or unacceptable comment, reaction, or job latency
  after improving indexes, query shapes, and transaction duration.
- A need for multiple application/worker hosts writing to one shared database.
- Availability, recovery, storage, or analysis requirements better served by a
  managed database.

Use measured behavior and deployment requirements as the triggers. User count
or total row count alone is not a capacity threshold.

Before a move, establish that the selected engine supports the needed queries
and constraints. Rehearse data transfer, preserve identities and delivery state,
and plan a controlled write/worker cutover with recovery options. Saved shows,
comments, reports, and pending reminders should retain their meaning afterward.

## References

- [SQLite: appropriate uses](https://sqlite.org/whentouse.html)
- [SQLite: write-ahead logging](https://sqlite.org/wal.html)
- [PlanetScale database offerings](https://planetscale.com/docs)

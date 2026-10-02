# Database and persistence changes

## Unreleased

### 2026-10-02 — SQLite first, with a later PlanetScale option

- Select SQLite as the initial database for planned radar, community, event
  history, and notification state.
- Document a single-host operating model, durable storage, and migration-aware
  persistence boundaries in the [database plan](../design/database.md).
- Record PlanetScale as a later option, with workload/deployment triggers and
  an explicit engine and data migration rather than a connection-URL swap.
- Align the API draft and project overview with the database decision.

This records the storage direction; persistence implementation is upcoming work.

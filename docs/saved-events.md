# Private saved events

Introduced with opt-in [Google accounts](accounts.md) and MariaDB schema **3**;
the current binary requires schema **5** with independent [event interest](event-interest.md)
and [public recommendations](event-recommendations.md).
Save an event from discovery, its preview, or the full event page, then find it at
`/saved` on a later visit or another authenticated client. Saved events are always
private: there are no public save counts, saver lists, visibility switches, or
profile activity. Saving does not express Interested, recommend an event, reserve
tickets, or schedule reminders.

## Setup and upgrade

There is no additional feature flag. For local development, `make db-setup` prepares
MariaDB, applies migrations and all grants, then verifies runtime access. Configure
accounts and follow the
[schema upgrade procedure](persistence.md#rolling-update-compatibility): drain
apps that support only the previous schema, preserve a backup, run `ticketopia migrate` with migration
credentials, reapply `deploy/mariadb/runtime-grants.sql`, then start the new app.
For the current v4-to-v5 upgrade, follow the [recommendation upgrade](event-recommendations.md#setup-and-upgrade).
The additive migration retains existing accounts, sessions, preferences and events.
It does not migrate the running database automatically. Database-free discovery
remains available; disabled deployments explain that saving requires accounts.

### Save fails after a schema upgrade

If sign-in and discovery work but Save returns “We couldn't complete this account
request,” check the runtime grants as well as the current schema version 5. Startup verifies
read access and schema shape, not mutation privileges. The runtime user needs
`INSERT, UPDATE` on `event_snapshots` and `INSERT, DELETE` on `saved_events`.
For the local Compose database, run the combined migration/grants/access-check target:

```sh
make db-setup
```

Then retry Save; restoring these table grants does not require an application restart.
For Kubernetes, reconcile the corresponding grants in the operator manifests.

## Web behavior

- **Save** and **Remove from Saved** work as ordinary CSRF-protected POST forms
  without JavaScript. Sign-in resumes the local event/discovery/Saved task; users
  choose Save after signing in rather than triggering a write through a GET.
- Enhancement keeps discovery filters, event selection, focus and loaded results
  intact and synchronizes visible row/preview controls after a successful write.
  A failed or timed-out save retains the prior state with retry guidance.
  Feedback appears below the initiating action row without moving or rewrapping
  its controls; Saved removal restores focus
  to a remaining event, or Discover when the collection becomes empty.
- `/saved` lists newest saves first, with event IDs breaking timestamp ties.
  **More saved events** is an ordinary paginated link. Event detail links retain
  the Saved page/cursor for returning; removing the last save restores the empty state.
- Saved collections read last-known SQL snapshots without calling Ticketmaster.
  They retain event identity, dates/TBA flags, venue, category, artists, source URL,
  prices, images, sale information and description. Snapshot copy explicitly tells
  users to check current availability. Provider image URLs are retained, not image
  files; artwork can still disappear and use the existing fallback.
- Detail reads prefer the live/cache discovery service, then fall back to durable
  metadata with `stale: true` when the provider/cache cannot resolve an event.
  Provider disappearance does not imply cancellation. Removing a bookmark never
  removes the shared durable event or another user's bookmark.
- Restored discovery controls revalidate private state in bounded batches. Results
  with another session's CSRF token are not restored as private controls.

## API

Use a browser session or a personal bearer token; all personal responses are
`private, no-store`. Cookie writes require same-origin checks and `X-CSRF-Token`
from `GET /api/v1/me`. Bearer writes do not use ambient cookies/CSRF.

| Method | Route | Meaning |
| --- | --- | --- |
| GET | `/api/v1/me/saved-events` | Owner-only `{items, next_cursor}` collection; `limit` defaults to 20, max 100 |
| GET | `/api/v1/me/saved-events/{event_id}` | Own bookmark and last-known event; 404 if not saved by you |
| PUT | `/api/v1/me/saved-events/{event_id}` | Empty body; 201 + Location for a new save, 200 for an existing save |
| DELETE | `/api/v1/me/saved-events/{event_id}` | Empty body; 204, including when already absent |
| GET | `/api/v1/me/saved-events/states?event_id=…` | Batched owner state; repeat `event_id` up to 101 times; no provider calls |

Collection cursors are opaque, owner-bound keysets ordered by `(saved_at, event_id)`
descending. Repeated PUT preserves the original `saved_at`; removals can shorten
later pages, and new saves appear after refreshing. Bookmark resources contain
`event`, `saved_at`, and `meta`; stored reads always mark metadata stale rather
than claiming a provider refresh. Clients cannot supply account IDs, event
metadata, visibility, or a request body for PUT/DELETE. See the
[OpenAPI contract](openapi.md) for validation/error responses.

Identity, provider mapping, full snapshot and the first bookmark commit in one
short InnoDB transaction. Snapshot replacement requires a newer collection time.
Unique keys coordinate concurrent app pools. Retrying a committed save needs no
provider call; uncertain commit responses can be safely resolved/retried by ID.
This is last-known metadata, not dated observation history or scheduled ingestion.

## Verification

Run `MARIADB_TEST_ADDR=127.0.0.1:3307 go test -race ./...`, `go vet ./...`,
`npm run build`, and [OpenAPI lint](openapi.md#viewing-validating-and-updating).
SQL tests create only isolated random databases/users on the local test server.
They cover ownership, atomic rollback, concurrent writes, stable timestamps,
case-sensitive ordering, migration preservation, and provider/cache-free restarts.

Optional browser checks use `scripts/saved-browser.cjs` against isolated SQL
fixtures and production handlers. Supply an installed Playwright module and
optionally an axe-core script (neither is a runtime dependency):

```sh
MARIADB_TEST_ADDR=127.0.0.1:3307 \
SAVED_BROWSER_SCRIPT="$PWD/scripts/saved-browser.cjs" \
PLAYWRIGHT_MODULE=/absolute/path/to/node_modules/playwright \
AXE_SCRIPT=/absolute/path/to/axe.min.js \
go test -race ./internal/persistence -run '^TestMariaDBSavedBrowserReview$' -v
```

The harness verifies enhanced/native saves, multiple clients/owners, guests,
disabled accounts, paging/return context, empty/error states, retained metadata,
desktop/tablet/320–390px, 200% text, and optional automated accessibility. Captures
and its report go to ignored `.impeccable/review/saved-*` files. It uses synthetic
event fixtures, not live Google consent or a production deployment. Action-layout
checks cover discovery, previews, loaded results, Saved, and full events with
hidden/pending/success/remove/long-error feedback across the same viewports and
text zoom. Rendering tests also keep feedback outside the controls row.

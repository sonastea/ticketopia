# Private artist and venue follows

Search for artists and venues, follow or unfollow them, and manage your private
collection at `/follows`. Follows are MariaDB-backed and available on later visits
and other authenticated clients. There are no public follower counts, follower
lists, visibility switches, or follow activity on public profiles. Following is
independent of Saved, Interested and recommendations; it helps explain and rank
[private Radar matches](radar.md), but does not reserve tickets or schedule notifications.

## Setup and upgrade

Requires opt-in [accounts](accounts.md) and clean MariaDB schema **10**. There is no
additional feature flag; scheduled event collection does not need to be enabled.
Database-free discovery stays available and explains that follows require accounts.

Before upgrading from schema 9, drain the old application, preserve a backup,
apply migrations with the migration credentials, and reapply
`deploy/mariadb/runtime-grants.sql` (or reconcile the operator grants). Then start
the schema-10 binary. Do not leave schema-9 binaries serving after migration.
The additive migration preserves accounts, sessions, preferences, event activity
and history. See [migration compatibility](persistence.md#rolling-update-compatibility).
Runtime gets INSERT/DELETE, not UPDATE, on `artist_follows` and `venue_follows`.
Runtime also uses the existing catalog and provider-mapping write grants.

For the disposable local setup, `make db-setup` migrates and reapplies/checks
runtime grants. This feature does not migrate a database at application startup.

## Web behavior

- **Follows** is in desktop Your space and the profile's account navigation; compact
  screens keep the existing four-destination navigation and reach it through Profile.
- Choose Artists or Venues and search by name. Venue results include supplied
  address/city/state/country to distinguish similar names, or “Location not supplied”
  when unavailable. After selecting Venues and searching, optional city/country
  filters are available. Switching back to
  Artists clears venue filters. Search is an ordinary GET, never a follow action.
- Follow/Unfollow are CSRF-protected POST forms that work without JavaScript.
  Successful writes redirect to the same search/collection page with confirmed
  controls. A failure offers a safe return to that task and retry guidance; uncertain
  results can be checked in the collection and retried idempotently.
- Guests sign in and return to their local follows task, then explicitly choose
  Follow. Accounts-disabled deployments explain the requirement.
- The owner-only collection supports all/artist/venue filters, ordinary pagination,
  and Unfollow. It reads retained catalog metadata without Ticketmaster or cache
  calls, ordered newest followed first. Empty pages link back to all follows.
- Search errors leave existing follows visible. Search and collection pagination
  have separate cursors. Unknown names return an honest empty result.

## API

All routes below require a browser session or personal bearer token and return
`Cache-Control: private, no-store`. Cookie writes require same-origin checks and
`X-CSRF-Token` from `GET /api/v1/me`; bearer writes do not use cookie CSRF.

| Method | Route | Behavior |
| --- | --- | --- |
| GET | `/api/v1/artists?keyword=…` | Name search with `limit` and `cursor` |
| GET | `/api/v1/venues?keyword=…` | Name search; optional `city`, `country`, `limit`, `cursor` |
| GET | `/api/v1/me/follows` | Owner-only `{items, next_cursor}`; optional `kind=artist` or `venue`, `limit`, `cursor` |
| GET | `/api/v1/me/follows/{kind}/{target_id}` | Own follow and retained metadata; 404 when not followed by you |
| PUT | `/api/v1/me/follows/{kind}/{target_id}` | Empty body; 201 + Location for new follows, 200 for existing follows |
| DELETE | `/api/v1/me/follows/{kind}/{target_id}` | Empty body; 204, including already absent follows |

`kind` is singular `artist` or `venue`. Target IDs are case-sensitive
`ticketmaster:SOURCE_ID` identities; artists and venues with identical source IDs
remain separate. Clients cannot supply owner IDs, metadata, visibility or write
bodies. Limits default to 20, maximum 100. Search keywords are nonblank, at most
120 UTF-8 bytes; unknown/repeated parameters and control characters are rejected.
See the [OpenAPI contract](openapi.md) for complete schemas and error responses.

Search uses Ticketmaster attractions/venues through the shared discovery cache,
singleflight, request budget, pacing and cooldowns. Results are cached for 24 hours
and retained for seven days for outage fallback. Search primes detail cache entries
so following a result normally needs no additional provider request. Name-ordered
provider pages may shift on refresh and are capped at 1,000 results; search cursors
are bound to kind and filters, not a frozen provider snapshot.

The first follow resolves server-owned provider or retained metadata, then atomically
stores catalog identity/provider mapping and the owner follow. A failed account FK
rolls back the catalog write too. Concurrent pools coordinate through catalog row
locks and unique keys. Retrying an existing follow needs no provider and preserves
`followed_at`. Collections/removals remain provider-free after restart; removing a
follow never deletes metadata or another account's follow. Stored metadata is marked
stale/last-known. Older data cannot overwrite newer catalog snapshots.

Collection cursors are owner- and kind-filter-bound keysets ordered by
`(followed_at, kind, target_id)` descending. New follows appear after refreshing;
removals may shorten subsequent pages. SQL retains provider metadata, not images
or user-supplied entity names.

## Verification

Run `MARIADB_TEST_ADDR=127.0.0.1:3307 go test -race ./...`, `go vet ./...`,
`npm run build` and [OpenAPI lint](openapi.md#viewing-validating-and-updating).
`make check` additionally validates asset generation, race tests and vet inside
the publishing Docker context. Real MariaDB and browser checks remain opt-in.
SQL tests create and clean isolated random schemas/users on the local test server;
they cover ownership, atomicity, concurrency, retries, case/kind keysets, retained
metadata, provider-free restart and schema-9 migration preservation.

Optional Chromium verification uses production handlers and isolated SQL/provider
fixtures, not live Google consent or production accounts:

```sh
MARIADB_TEST_ADDR=127.0.0.1:3307 \
FOLLOWS_BROWSER_SCRIPT="$PWD/scripts/follows-browser.cjs" \
PLAYWRIGHT_MODULE=/absolute/path/to/node_modules/playwright \
AXE_SCRIPT=/absolute/path/to/axe.min.js \
go test -race ./internal/persistence -run '^TestMariaDBFollowsBrowserReview$' -v
```

If needed, set `CHROMIUM_EXECUTABLE` to an installed Chromium/Chrome binary.
The harness exercises native/no-JavaScript forms, multiple sessions/owners, API
parity, guests, disabled accounts, paging, empty/outage recovery, long content,
missing venue locations in search and the retained collection, desktop/tablet/mobile/
320px, enlarged text and forced-colors/reduced-motion focus.
Captures/report go to ignored `.impeccable/review/follows-*`. Manual screen-reader,
physical-device, live OAuth and comprehensive cross-browser checks remain separate.

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
request,” check the runtime grants as well as the current schema version 7. Startup verifies
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
  without JavaScript. Signed-out **Sign in to save** completes that Save once
  Google sign-in succeeds and resumes the event; other sign-in returns resume
  the local event/discovery/Saved task without writing. See
  [task continuity](accounts.md#sign-in-task-continuity).
- Enhancement keeps discovery filters, event selection, focus and loaded results
  intact and synchronizes visible row/preview controls after a successful write.
  A failed or timed-out save retains the prior state with retry guidance.
  Feedback appears below the initiating action row without moving or rewrapping
  its controls; Saved removal restores focus
  to a remaining event, or Discover when the collection becomes empty.
- `/saved` lists newest saves first, with event IDs breaking timestamp ties.
  **More saved events** is an ordinary paginated link. Event detail links retain
  the Saved page/cursor for returning. Enhanced return across event sections also
  restores browsing position and event-title focus; removing the last save restores
  the empty state. Row titles and artwork open Overview; tickets are direct and
  reminders live in the event view's secondary participation group.
- Saved collections read last-known SQL snapshots without calling Ticketmaster.
  They retain event identity, dates/TBA flags, venue, category, artists, source URL,
  prices, images, sale information and description. Snapshot copy explicitly tells
  users to check current availability. Provider image URLs are retained, not image
  files; artwork can still disappear and use the existing fallback.
- Detail reads prefer the live/cache discovery service, then fall back to durable
  metadata with `stale: true` when the provider/cache cannot resolve an event.
  Provider disappearance does not imply cancellation. Removing a bookmark never
  removes the shared durable event or another user's bookmark.
- Restored discovery results are rendered again by the server with current private
  state ([restoring loaded pages](discovery.md#restoring-loaded-pages)); browser
  history never stores private controls. Back/forward-cache restores revalidate
  private state in bounded batches.

## Action feedback and collection removal

Save and Interested retain their last confirmed control state during a pending or
failed write. A transport failure or unreadable response explains that confirmation
is uncertain and that retrying the same explicit action is safe; it does not claim
the server made no change. Save stays private, interest keeps its Private/Public
label, and neither action withdraws independently published recommendations.

A write still pending after 300ms swaps the pressed control's icon for a small
spinner (Save, Interested), or a text-only action's label (Save visibility,
publish/save/withdraw recommendation), without resizing the button. Reduced motion
pulses the ring instead of rotating it; forced colors keep text labels. When the
connection drops (offline, or the request or response is lost), Save, Interested
and recommendations all show "Couldn't connect. Check your connection and try
again; repeating it is safe." A timeout or unreadable response keeps the
action-specific "could not be confirmed" wording.

Enhanced removal keeps surviving Saved/Interests rows mounted instead of reloading
and refilling the page. The collection cursor and remaining row's reading position
are retained; focus moves to the next removal control, or the previous one when
removing the last row. If that control is busy, its event-title link is the fallback.
Focus already moved elsewhere during the write is not stolen while rows survive.
Each collection exposes a visible, event-named status confirmation. Restored action
focus stays above the sticky compact navigation, including enlarged labels.

When the last rendered row is removed, the server reloads the same collection URL
to supply current rows or truthful empty/error guidance. Newly available rows take
focus; an empty later page prioritizes **Back to newest saves/interest**, because
earlier choices may remain. A wholly empty first page prioritizes Discover. Native
POST forms still redirect to the same collection and use the same recovery copy,
with ordinary document navigation rather than enhanced focus restoration. No new
undo or automatic participation/publication/reminder action is introduced.

The cross-browser fixture exercises pending/rejected/lost-response writes, safe
retry, independent recommendation retention, first/middle/last removals on both
collection pages, cursor continuity, focus moved during slow writes, empty-page
recovery, desktop/390px/320px at 200% text, and native forms:

```sh
MARIADB_TEST_ADDR=127.0.0.1:3307 \
ACTION_FEEDBACK_BROWSER_SCRIPT="$PWD/scripts/action-feedback-browser.cjs" \
PLAYWRIGHT_MODULE=/absolute/path/to/node_modules/playwright \
AXE_SCRIPT=/absolute/path/to/axe.min.js \
go test -race ./internal/persistence -run '^TestMariaDBActionFeedbackBrowserReview$' -v
```

Reports and captures go to ignored `.impeccable/review/action-feedback/` directories.
The 2026-10-12 run passed 274 checks per engine (548 total), 22 clean axe views,
and the existing 115 Saved/Interested behavior regressions. The full isolated-MariaDB
race suite, host vet, and Docker publishing-context validation (`make check`) also passed.
These synthetic SQL/provider fixtures supplement, not replace, real-phone and
manual screen-reader journeys. Live Google consent and public-pilot acceptance
are not claimed.

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
CHROMIUM_EXECUTABLE=/absolute/path/to/full/chromium \
AXE_SCRIPT=/absolute/path/to/axe.min.js \
go test -race ./internal/persistence -run '^TestMariaDBSavedBrowserReview$' -v
```

Use a full Chromium build: Playwright's default headless shell reports every
cross-document view transition as a page error.

The harness verifies enhanced/native saves, multiple clients/owners, guests,
disabled accounts, paging/return context, empty/error states, retained metadata,
desktop/tablet/320–390px, 200% text, and optional automated accessibility. Captures
and its report go to ignored `.impeccable/review/saved-*` files. It uses synthetic
event fixtures, not live Google consent or a production deployment. Action-layout
checks cover discovery, previews, loaded results, Saved, and full events with
hidden/pending/success/remove/long-error feedback across the same viewports and
text zoom. Rendering tests also keep feedback outside the controls row. Every
capture also focuses each tab-order control and requires it to scroll clear of
the floating navigation and the phone Tickets/Save bar, with 44px action targets.
That check replaces axe `target-size` results caused only by that sticky chrome:
axe judges them at one scroll position, where whatever sits under the bar fails.

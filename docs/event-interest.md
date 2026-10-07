# Interested in events

Introduced with opt-in [Google accounts](accounts.md) and MariaDB schema **4**;
the current binary requires schema **5** with [public recommendations](event-recommendations.md).
Mark an event Interested in discovery, its preview, Saved, or the event page;
manage your choices at `/me/interests`. Interest is independent of private saves,
category preferences, recommendations, ticket ownership, and attendance. It does
not reserve tickets or schedule notifications.

## Setup and upgrade

There is no new feature flag. Enable accounts and apply migrations and grants.
For local development, `make db-setup` prepares the local database, migrates it,
and verifies runtime privileges. See [persistence](persistence.md).

This binary requires clean schema **5 only**. Follow the current
[recommendation upgrade](event-recommendations.md#setup-and-upgrade), draining older
apps before applying all pending migrations and runtime grants. Additive migrations
preserve accounts, sessions, preferences, snapshots, saves, and interest; existing
saves never become interest and interest never becomes a recommendation. No
automatic startup migration or zero-downtime cross-version rollout is claimed.
Runtime needs SELECT plus INSERT/UPDATE/DELETE on `event_interests`, and the existing
event/provider/snapshot write privileges. Operator examples include these grants.
The running development database is not changed by the isolated test suite.

## Privacy and public activity

- A new account's interest visibility defaults to **Private**. The profile setting
  applies only to **new** choices; changing it never republishes existing activity.
- The Interested button discloses Private/Public before the action. Its selected
  `aria-pressed` state describes **your choice**, separately from the aggregate count.
- **Private** contributes to an event's count without naming the account. Counts
  include public and private choices; in small groups they do not guarantee anonymity.
- **Public** explicitly lists your display name/profile on the event's Community
  section and public participant page, and the event on your public profile.
  Email, preferences, saves, and private interest never enter these projections.
- Open **Change visibility** beside the count on an event or in your collection to change that
  event alone. Changing to Private or removing interest excludes it from all
  subsequent public reads; the initiating community panel refreshes its identities.
  Public activity responses are also `private, no-store` to avoid cached identities.
- Removal affects only your interest: it leaves your bookmark, another account's
  choice, and shared event details intact. Changing visibility/retrying preserves
  the original interest timestamp; remove/reselect creates a new choice.

## Web behavior

Ordinary CSRF-protected POST forms work without JavaScript. Sign-in resumes local
event/filter/interest-collection context, but never writes through a GET. Enhanced
writes retain discovery selection, filters, loaded results and focus, synchronize
visible row/preview controls, and use the confirmed button/count as success feedback.
One Private/Public label on the button avoids repeated state text. Counts use a
readable, emphasized number; the compact **Change visibility** disclosure opens
the form only when needed, with one editor open at a time. Successful visibility
changes close the editor and restore focus to its summary. Pending/success outcomes
are announced to assistive technology without adding persistent visual messages.
Errors remain visible near the action until dismissed, retried, or replaced by a
new action; at most one interest error is shown at a time. Dismissal restores focus
to the action. Failed writes retain the last confirmed selected state. Browser-history
restoration revalidates choices and credentials in bounded batches.

The own collection is newest-first, paginated, and separate from private category
preferences on the same page. Detail links retain the collection cursor for return;
removal confirms the action and restores focus to a remaining event or Discover.
Public profile activity and participant lists are independently paginated.
Participant pagination preserves the originating discovery or collection page for
returning through the event. Provider or cache disappearance does not erase interest
or imply cancellation: interest
retains the same shared last-known event snapshots as saves. Stored reads are stale,
not current ticket availability. Disabled deployments explain the account requirement;
storage failures show unavailable state rather than inventing zero counts.

## API

Own resources accept browser sessions or personal bearer tokens. Cookie writes
require same-origin checks and `X-CSRF-Token` from `GET /api/v1/me`; bearer writes
do not use ambient cookies or CSRF. All interest responses are `private, no-store`.

| Method | Route | Meaning |
| --- | --- | --- |
| GET | `/api/v1/me/event-interests` | Owner-only `{items, next_cursor}`, including private choices |
| GET | `/api/v1/me/event-interests/{event_id}` | Own choice/snapshot; 404 when absent |
| PUT | `/api/v1/me/event-interests/{event_id}` | JSON `{}` or `{"visibility":"private"}` / `{"visibility":"public"}`; 201 + Location for new, 200 for existing |
| DELETE | `/api/v1/me/event-interests/{event_id}` | Empty body; idempotent 204 |
| GET | `/api/v1/me/event-interests/states?event_id=…` | Repeat IDs up to 101; counts, own state, default visibility, browser CSRF token |
| GET | `/api/v1/events/{event_id}/interest` | Public `{count}` only, including private choices |
| GET | `/api/v1/events/{event_id}/interested-users` | Only public participants' allowlisted profiles and interest timestamps |
| GET | `/api/v1/users/{user_id}/event-interests` | Only explicitly public interest, even when the reader owns the profile |

PUT omission preserves existing visibility or uses the account default for a new
choice. Unknown/repeated fields, null, unsupported visibility, client owner/metadata,
and query parameters on single-resource writes are rejected. Collections accept
`limit` (default 20, maximum 100) and `cursor`; keysets are bound to owner/audience
or event, with binary IDs breaking timestamp ties. Removal/visibility changes can
shorten later pages. Counts and participant reads require no provider calls;
unknown valid event IDs return zero/empty lists. See the [OpenAPI contract](openapi.md).

Identity, provider mapping, last-known snapshot and first interest commit in one
InnoDB transaction. Unique account/event keys and event-first locks coordinate
concurrent pools and independent save/interest writers. Retries for an existing
choice and visibility changes need no provider call. Snapshot replacement requires
a newer collection time; removal retains shared metadata.

## Verification

Run `MARIADB_TEST_ADDR=127.0.0.1:3307 go test -race ./...`, `go vet ./...`,
`npm run build`, and [OpenAPI lint](openapi.md#viewing-validating-and-updating).
SQL tests use isolated random schemas/users, covering atomic rollback, uniqueness
across independent pools, save separation, privacy revocation, owner/audience/event
cursor boundaries, case-sensitive pagination, migration preservation, and restart
without provider/cache data.

Optional Chromium verification reuses the isolated activity fixture harness:

```sh
MARIADB_TEST_ADDR=127.0.0.1:3307 \
INTEREST_BROWSER_SCRIPT="$PWD/scripts/interests-browser.cjs" \
PLAYWRIGHT_MODULE=/absolute/path/to/node_modules/playwright \
AXE_SCRIPT=/absolute/path/to/axe.min.js \
go test -race ./internal/persistence -run '^TestMariaDBInterestBrowserReview$' -v
```

Playwright and axe are verification-only, not runtime dependencies. Checks cover
enhanced/native forms, multiple owners/guests, private/public projections, pending
and failed writes, collection/return pagination, disabled accounts, retained details,
desktop/tablet/320–390px, 200% text, and optional automated accessibility. Captures
and the report are ignored `.impeccable/review/interest-*` artifacts. These are
synthetic provider/account fixtures, not live Google consent or production deployment.
The refined controls passed 84 Chromium checks and ten zero-violation axe captures,
including dismissible/replaced errors, compact/open editors, focus after saving,
and native visibility changes. This is not a comprehensive cross-browser or WCAG
certification.

# Followed discussions

Implemented with opt-in accounts and MariaDB **schema 12**. Follow/unfollow an
individual event conversation privately; posting, saving and artist/venue follows
do not automatically subscribe you. No email or browser push is sent.

## Use

Open a conversation and expand **Follow this conversation**. Choose:

- **Every new reply:** one in-app update for each new reply from someone else.
- **Daily summary:** one grouped update per conversation at the next midnight UTC.
- **Weekly summary:** one grouped update at the next Monday midnight UTC.
- **Muted:** retain the follow without delivering updates.

Read `/me/discussions` for followed conversations and `/me/notifications` for due
reply updates; both are linked from Profile. Updates name the event and visible
reply count, not a copy of contribution text. **Open conversation at reply** selects
the latest visible reply even outside the first reply page. Mark read separately;
opening a link does not silently change state. Read updates remain in the inbox.
Native forms and links work without JavaScript.

Accounts start with **Pause all notifications** enabled. Resume in Preferences to
receive future replies. Pause overrides every conversation frequency: no replies
created during the pause are queued, and the inbox is withheld until resumed.
Already queued pre-pause updates remain. Digest schedules use UTC, not the profile
time zone; the UI explicitly explains this.

No historical replay occurs on follow, resume or re-follow. Own replies, edits,
Helpful, reports and moderation decisions do not generate updates. Repeated
publication requests do not duplicate notifications. Changing frequency cancels
unread updates, including pending summaries; unchanged retries preserve them.
Unfollowing deletes that conversation's notification history and pending updates.

## Visibility and privacy

Follows and inboxes are owner-only and `private, no-store`; they never enter public
profiles or participant counts. Followed roots use the same public, permission-
filtered projection as discussions. Hidden/removed roots remain content-free
placeholders in the follow collection, with remaining replies reachable normally.

Delivery checks current visibility, not the publication-time snapshot. Hidden or
removed roots suppress all notifications for that thread. Hidden/removed replies
are excluded from summaries and direct link selection. An empty summary is withheld.
Restored, currently public content can become eligible again. Following a hidden
root retains it privately but does not queue replies while the root is hidden.
Direct thread reads recheck visibility if moderation occurs after inbox delivery.

Notification storage contains only owner/thread/reply IDs, delivery window, and
read state. It copies no contribution body, author attribution, report reason,
reporter identity, private notes, or evidence. No reporting/moderation tables are
joined for notification delivery. Report/decision activity itself is not announced.

## API and persistence

OpenAPI **1.12.0** documents:

| Method | Route | Purpose |
| --- | --- | --- |
| GET, PUT, DELETE | `/api/v1/events/{event_id}/discussions/{discussion_id}/follow` | Read/set/remove your follow |
| GET | `/api/v1/me/discussions` | Private followed collection |
| GET | `/api/v1/me/notifications` | Due, currently visible reply updates |
| PUT | `/api/v1/me/notifications/{notification_id}/read` | Mark a delivered update read |

Follow PUT requires `{"frequency":"immediate|daily|weekly|muted"}` (one enum value).
DELETE/read PUT require empty bodies. Writes reject query parameters, repeated,
unknown and null JSON fields; cookie writes require same-origin/CSRF checks, bearer
identity takes precedence. Event/thread ancestry is checked. Collections accept
`limit` (1–100, default 20) and owner/collection-bound `cursor`; they use descending
time/binary-ID keysets without a snapshot pin. Read-state updates are owner-only,
idempotent; not-due/hidden/paused/muted or another owner's IDs return 404.

Schema 12 adds `discussion_follows`, `discussion_notifications` and
`discussion_notification_posts`. Reply publication and notification enqueue commit
in the same transaction. Root locking serializes subscription changes/publication;
durable envelope uniqueness coalesces UTC summary windows. Delivery happens on
inbox reads after the due time; no background worker, external transport, live
polling, retention job or high-fanout capacity certification is included.

Drain older binaries, back up, migrate explicitly, reconcile runtime grants, then
start schema-12 apps. Do not run `make db-setup` against a shared service. See
[upgrade compatibility](persistence.md#rolling-update-compatibility).

## Verification

Tests cover reply/queue atomic rollback, concurrent retry uniqueness, schema-11
preservation, restart, owner/collection-scoped pagination, frequencies, pause/mute,
own-reply suppression, ancestry, unfollow cleanup and delivery-time moderation.
HTTP tests cover strict inputs, credentials, CSRF, private projections and redirects.
The synthetic browser journey checks native follow/read/unfollow, exact off-page
reply navigation, compact/enlarged text layouts and automated accessibility:

```sh
MARIADB_TEST_ADDR=127.0.0.1:3307 go test -race ./...
go vet ./...
DISCUSSION_BROWSER_SCRIPT="$PWD/scripts/discussion-follows-browser.cjs" \
PLAYWRIGHT_MODULE=/path/to/node_modules/playwright \
AXE_SCRIPT=/path/to/node_modules/axe-core/axe.min.js \
MARIADB_TEST_ADDR=127.0.0.1:3307 \
go test ./internal/persistence -run TestMariaDBDiscussionBrowserReview -count=1 -v
make check
```

The browser journey passes 52 checks and six zero-violation axe scans; existing
discussion behavior passes another 24 checks without recapturing unrelated UI.
The real-MariaDB race suite, targeted queue rollback/schema-upgrade/pagination
tests, vet, production build and OpenAPI lint pass (lint retains the two existing
health-route warnings).

This is isolated fixture evidence, not physical-device, manual screen-reader,
cross-browser, production delivery or public-pilot certification.

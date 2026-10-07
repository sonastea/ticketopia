# Public event recommendations

Implemented with opt-in [Google accounts](accounts.md) and MariaDB schema **5**.
A recommendation is a public endorsement, not a save, interest choice, attendance
declaration, discussion post, or Helpful reaction. Reading does not require sign-in;
publishing/editing/withdrawal requires an authenticated account. There is no new flag.

## Setup and upgrade

This binary requires **clean schema 5 only**. For an existing schema-4 deployment:
stop/drain v4 apps, preserve a backup, run `ticketopia migrate` using migration
credentials, reapply/reconcile runtime grants, then start v5 apps. The additive
migration preserves accounts, sessions, preferences, saves, interest, and snapshots;
it does not publish existing activity. Startup never migrates automatically and
cross-version zero-downtime rollout is not supported. See
[persistence](persistence.md#rolling-update-compatibility).

For local development, `make db-setup` prepares MariaDB, migrates, reapplies grants,
and verifies runtime privileges. Runtime requires SELECT and INSERT/UPDATE/DELETE
on `event_recommendations`, plus existing event/provider/snapshot writes. SQL and
operator examples include these grants. Verification uses isolated test schemas/users
and does not migrate the running development database.

## Publish, edit, and withdraw

- Open an event or preview, then **Recommend** to open the native composer directly.
  The Community section also provides a **Recommend this event** disclosure.
- Before **Publish recommendation**, the form explains that your display name,
  profile link, and reason appear publicly on the event, in Community, and on your
  profile. Private interest visibility does **not** hide recommendations.
- A reason is optional, plain text, up to 500 characters. Surrounding whitespace
  is trimmed; invalid encoding/control characters and overlong reasons are
  rejected, not truncated. Reasons are escaped in HTML, never rendered as markup.
- Each account has one recommendation per event. **Edit your recommendation**
  replaces or clears the reason. Publication time remains stable; changed reasons
  update the edit time, and unchanged retries preserve both times. Edits do not
  bump an event to the top of Community.
- **Withdraw recommendation** removes only your endorsement. Save, Interested,
  other people's recommendations, and shared snapshots remain intact. Withdrawal
  excludes the endorsement from subsequent public reads, not copies already viewed.
  Republish creates a new publication.

Ordinary POST forms work without JavaScript. Failed validation/authentication/CSRF
returns a retry/recovery page retaining the draft and safe event/return destination,
without applying the rejected mutation. With an authenticated session a retry uses
the current CSRF credential. After session expiry, the draft stays available to
copy while sign-in returns to the event; native forms do not transfer it through OAuth.
Enhancement announces pending/success, disables duplicate submits, keeps confirmed
state on failure, refreshes the initiating panel after confirmation, closes the
editor, and returns focus. Drafts survive section changes and resizing; browser
session storage is scoped to account/event and never publishes a draft. Storage
failure still permits in-page drafting. Late responses cannot replace another event.

## Public browsing

The event's Community section displays explicit recommendation counts, authors,
plain-text reasons, publication dates, and an edited label when appropriate. A
separate section lists public-opt-in interest identities; these are not endorsements.
`/events/{event_id}/recommendations` provides ordinary recommendation pagination.

`/community` lists recent endorsements with optional city, country-code, and category
filters. Several people can independently recommend the same event, so it may appear
more than once with different authors/reasons. Ordering is original publication time,
then binary event and account ID descending, not personalized ranking. City/country
match the first retained venue, falling back to the event place when missing; city
matching is exact and case-insensitive, not a wildcard search. Category matches an
event segment ID. Applying filters resets pagination; catalog failure does not block
retained recommendations or clear the selected category.

Public profiles at `/users/{user_id}` show recommendations independently of private
interest. Recommendation pagination uses a separate `recommendation_cursor` from
interest's `cursor`. Event links retain the originating community/profile page.
Email, preferences, private saves, and private interest identities never enter
recommendation projections. All activity responses use `private, no-store`;
browser history restoration refreshes recommendation surfaces.

## Persistence and availability

The unique account/event key enforces one endorsement. New identity, provider mapping,
complete last-known snapshot, and recommendation commit atomically. Existing edits
and withdrawal require no provider requests. Collections use retained snapshots
with `meta.stale=true` and `data_as_of`, not current ticket availability. Event
details use normal discovery with retained-detail fallback. IDs are case-sensitive;
newer snapshots cannot be erased by older action data.

Disabled accounts explain web availability and return 503 for public recommendation
APIs rather than fabricated zero activity. Storage failures mean unavailable, not
empty. Discussions, Helpful, reminders, reporting, and moderation remain unimplemented.
**Reporting/moderation is required before a public community pilot.**

## API

The [OpenAPI contract](../internal/api/openapi.yaml) is version **1.5.0**.

| Method | Route | Purpose |
| --- | --- | --- |
| GET | `/api/v1/recommendations` | Public city/country/category endorsements |
| GET | `/api/v1/events/{event_id}/recommendations` | Public event endorsements and total count |
| GET | `/api/v1/users/{user_id}/event-recommendations` | Public account endorsements |
| GET | `/api/v1/me/event-recommendations` | Own published endorsements |
| GET, PUT, DELETE | `/api/v1/me/event-recommendations/{event_id}` | Read, publish/replace reason, or withdraw own endorsement |

Collections return `items` and nullable `next_cursor`. Event collections also return
total `count`, independently of pagination; valid unknown event IDs return zero and
an empty array without provider calls. Limits are 1–100, default 20. Cursors bind to
collection/audience/account/event and normalized filters; cross-scope reuse and
repeated/unknown parameters return 400. Clients must disclose public attribution.

PUT requires a JSON object: `{}` publishes without a reason or clears an existing
reason, and `{"reason":"Worth seeing live"}` replaces it. Null, unknown/repeated
fields, owner/visibility/metadata injection, and query parameters are rejected.
Creation returns 201 with Location; replacement/retry returns 200. Missing own
recommendations return 404. DELETE requires an empty body and returns retry-safe
204 even when absent. Cookie writes require same-origin and CSRF checks; bearer
writes use the token's owner, not ambient cookies. See
[accounts](accounts.md#sessions-and-api-clients).

## Verification

```sh
MARIADB_TEST_ADDR=127.0.0.1:3307 go test -race ./...
go vet ./...
npm run build
npx --yes @redocly/cli lint internal/api/openapi.yaml
```

Tests cover concurrent uniqueness/ownership, save/interest independence, rollback,
migration preservation, offline edits/restarts, binary keysets, city/place/category
filters, scoped cursors, strict input/CSRF/privacy, and native draft/return behavior.

Optional Chromium journeys use existing Playwright/Chromium and axe installations:

```sh
MARIADB_TEST_ADDR=127.0.0.1:3307 \
RECOMMENDATION_BROWSER_SCRIPT="$PWD/scripts/recommendations-browser.cjs" \
PLAYWRIGHT_MODULE=/path/to/node_modules/playwright \
AXE_SCRIPT=/path/to/axe-core/axe.min.js \
go test -race ./internal/persistence -run '^TestMariaDBRecommendationBrowserReview$' -v
```

Set `CHROMIUM_EXECUTABLE` only for a separately installed Chromium binary. The fixture
exercises native/enhanced publication/edit/withdrawal, failed writes/session/CSRF and
draft recovery, public privacy, pagination/return, provider outage, disabled accounts,
desktop/tablet/320–390px reflow, 200% text, focus, and automated accessibility.
Reports/screenshots go to `.impeccable/review/recommendation-*`; fixtures are synthetic.
The independent review scored all four requested fixes resolved. It also noted
pre-existing ticket-button clipping at 200% text, unchanged by this feature;
the recommendation reflow checks do not certify the entire event page.

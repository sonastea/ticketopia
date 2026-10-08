# Event discussions

Implemented with opt-in [accounts](accounts.md) and MariaDB schema **6**. Public
questions, tips, replies, and positive **Helpful** acknowledgments are independent
of private saves, Interested, recommendations, and attendance. Reading is public;
writing requires sign-in. Conversations are asynchronous, not live chat.

## Setup and upgrade

Discussions were introduced in schema 6; this binary supports clean schema 8 only.
Drain old apps, preserve a backup,
run `ticketopia migrate` with migration credentials, reconcile runtime grants,
then start schema-8 apps. There is no startup migration or cross-version
zero-downtime rollout. See [persistence](persistence.md#rolling-update-compatibility).
Local `make db-setup` applies migrations/grants; testing uses isolated schemas
and does not upgrade the running development database.

Runtime needs SELECT plus INSERT/UPDATE on `discussion_posts`, and
INSERT/UPDATE/DELETE on `post_helpful`, alongside existing snapshot/account grants.
Posts are not physically deleted: removal preserves immutable ancestry and retry
identity. Root creation commits identity, snapshot, and post atomically. Replies,
edits, removal, Helpful, and direct thread reads do not need Ticketmaster.

## Web experience

Open an event's **Discussion** section or selected desktop preview to ask a
question or share a tip. Public attribution is explained before publication.
Contributions are required plain text, up to 4000 Unicode characters including
surrounding whitespace, and must be nonempty after trimming;
invalid encoding/control characters and overlong text are rejected, not truncated.
Text is escaped, never rendered as markup. Names and profile links are public
even when interest is private.

Root posts are newest-first. **Open conversation** leads to a shareable
`/events/{event_id}/discussions/{discussion_id}` page, with event context and
oldest-first replies. Replies to replies name a target within the same root;
there is at most one visual nesting level. Reply targets can be read independently
of the current reply page. Authors can edit or remove their own posts. Removal
withholds the body and author attribution, clears reactions, and leaves a
content-free placeholder. Replies remain readable; removal cannot be undone.

Helpful can be set or removed independently per user/post. It is a positive
acknowledgment, not an event endorsement or negative vote. Counts never imply
private saves or publish private interest identities.

[Private reporting and moderation](moderation.md) adds Report privately, own
receipts/outcomes, and authorized keep/hide/restore with author review requests.
Hidden posts withhold text, attribution, and Helpful state/counts publicly, while
replies remain accessible. Authors cannot edit hidden text to bypass review.
Hiding is reversible; author removal is not. Reports retain private original-text
evidence for authorized moderators, including after author removal.

Personal report/outcome links select the exact contribution with `post_id` and
`#post-ID` in a dedicated thread. Off-page replies render once in a **Selected
contribution** section, independently of reply pagination and without exposing
hidden text or changing the reply target. Selection enforces event/thread ancestry.

Community has distinct **Recommendations** and **Conversations** collections.
`/community/discussions` supports city, country, category, and pagination. Ordering
is newest **started**, not newest reply or personalized ranking. Filters use
retained venue/place metadata, exact case-insensitive city matching, and category
segment IDs. Changing filters resets pagination; category-catalog outages do not
block retained threads. Event/collection links retain the originating return context.

Native forms work without JavaScript. Rejected writes retain escaped drafts and
safe return context; native sign-in recovery leaves the draft available to copy
rather than sending it through OAuth. Enhancement preserves account/event/thread/
post/target-scoped drafts in browser session storage, disables pending submission, and
confirms writes before navigation. Storage failure still permits in-page drafting.
Late responses cannot replace a different selected event. No live polling or
notification delivery is included.

## API

The [OpenAPI contract](openapi.md) is version **1.8.0**.

| Method | Route | Purpose |
| --- | --- | --- |
| GET, POST | `/api/v1/events/{event_id}/discussions` | Read roots / publish a question or tip |
| GET | `/api/v1/events/{event_id}/discussions/{discussion_id}` | Root and retained event context |
| GET, POST | `/api/v1/events/{event_id}/discussions/{discussion_id}/replies` | Independent reply pagination / publish reply |
| PATCH, DELETE | `/api/v1/posts/{post_id}` | Author-only text replacement / removal |
| PUT, DELETE | `/api/v1/posts/{post_id}/reactions/helpful` | Set/remove your positive acknowledgment |
| GET | `/api/v1/community/discussions` | City/country/category root browsing |

Creation takes `{"body":"Question or reply"}`; replies may add `parent_id`.
Send one **Idempotency-Key** header: 16–128 ASCII letters/digits/hyphens/underscores,
unique within the account. Keys and payload fingerprints are retained for the
contribution's lifetime, including after editing/removal. Retrying the same normalized
event/thread/parent/body returns the current post with 200, never republishes removed
content. Reuse with another payload returns 409. First creation returns 201 and
the root-thread Location. Creation allows 20 attempts/account/ten minutes across
roots/replies using durable account rate counters.

PATCH replaces required `body`; DELETE and Helpful requests require empty bodies.
Writes reject query parameters, unknown/repeated/null JSON fields, and JSON bodies
over 16 KiB. Native URL-encoded forms allow 64 KiB so Unicode text fits. Cookie
writes require CSRF and same-origin checks; bearer identity takes precedence.

Collections return `items`, nullable `next_cursor`, and a separate `count` of
nonremoved roots/replies. Removed roots with visible replies remain navigable but
do not increment root count. Default page size is 20, maximum 100. Cursors bind
collection, viewer, event/thread, filters, and a creation-time snapshot boundary:
the boundary uses MariaDB's creation clock rather than the application host clock.
Later creations appear on refresh, while edits/removals remain current. Unknown
valid event IDs return an empty root list without provider calls. Failures mean
unavailable, never fabricated zero activity. All responses are `private, no-store`;
anonymous projections include no private account fields or another viewer's choices.

## Verification and remaining scope

SQL tests cover concurrent retry uniqueness, Helpful uniqueness, ownership,
ancestry, independent social state, offline restart, removal context, ordered
pagination, scoped filters/cursors, schema-5 preservation, atomic snapshot rollback,
and MariaDB-clock creation boundaries. HTTP tests cover strict inputs, CSRF,
credentials, public privacy, unavailable state, and native draft recovery.

```sh
MARIADB_TEST_ADDR=127.0.0.1:3307 go test -race ./...
go vet ./...
npm run build
npx --yes @redocly/cli lint internal/api/openapi.yaml
```

Optional Chromium/axe journeys use `scripts/discussions-browser.cjs` through
`TestMariaDBDiscussionBrowserReview`, with `DISCUSSION_BROWSER_SCRIPT`,
`PLAYWRIGHT_MODULE`, and `AXE_SCRIPT` configured like the
[recommendation browser fixture](event-recommendations.md#verification).
Use `go test -count=1` for browser reruns after changing the external script,
so Go's test cache does not substitute a previous run.
The synthetic fixture verifies 24 browser outcomes, including native/enhanced
writes, draft/retry recovery after network failures, privacy, return context,
and provider-free threads.
Desktop/tablet/390px/320px/200%-text thread captures passed overflow and axe checks;
this is scoped evidence, not a whole-application or public-pilot certification.
The finish review's **ship** verdict scored the transport-error copy fix resolved;
it did not certify every community surface or the public pilot.

**Basic private reporting and moderator review are implemented**; see
[moderation setup and verification](moderation.md). Discussion follows/notifications,
attendance, and profile-wide contribution feeds remain later work. No public-pilot approval,
multi-pod deployment, or production backup/restore is claimed.

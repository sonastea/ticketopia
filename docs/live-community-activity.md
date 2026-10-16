# Live community activity

Live activity is an opt-in, sitewide view of public recommendations from the last
ten minutes. Guests and signed-in visitors see the same public view. It starts
collapsed above app content and never reorders Community or other collections.

## Attribution and controls

Profile & privacy has an optional **First name for live activity**. Blank means
**Someone**; nothing is inferred from display name, email or Google claims.
Whitespace is normalized. Names accept 1–40 Unicode code points consisting of
letters/marks, internal spaces, hyphens and apostrophes, with at least one letter.
Invalid input is rejected rather than truncated. Existing accounts start empty.
The owner-only profile PATCH preserves omitted values, clears blank strings and
rejects null. Public profiles still use the existing display-name contract.

The notification DTO contains only event ID/title/city, optional chosen first
name, contribution count and publication/expiry times. It contains no account ID,
profile link, display name, email, avatar, biography or recommendation reason.
Multiple contributions to an event show a count, never a list of names.

Opening **Live activity** shows up to 20 event groups and real Overview links with
browsing return context. New rows wait behind **Show N new updates** while reading;
withdrawals, name clearing and expiries apply immediately. Reconnects show current
activity quietly. **Hide live activity** stores only a browser-local preference;
**Show live activity** remains reachable. Storage failure uses page memory.
Signed-in **Pause all notifications** suppresses display and streaming. Preferences
are checked at entry/focus/BFCache and every 30 seconds while visible. Neither
pause nor hiding disables ordinary aggregate count refreshes or participation.

An interrupted connection clears attribution immediately and retains counts as
stale. A 30-second heartbeat watchdog detects silent stalls. Reconnect uses jittered
backoff up to 60 seconds; hidden/offline pages stop streaming. Without EventSource,
JSON polling runs every 15 seconds while visible. Names/bodies are never stored
in browser storage. Native forms, links and Community remain usable without JS.

## Storage and delivery

Schema 16 adds nullable `accounts.activity_first_name` and reference-only
`live_recommendation_activity`. First-ever publication inserts eligibility in the
recommendation transaction only when enabled. Withdrawal removes it atomically.
Retries, edits, republishing and publications made while disabled never create
another contribution. Existing anti-bumping recommendation markers remain durable.

Projection joins active recommendations, accounts and stored event snapshots in a
consistent SQL transaction; it needs no provider call. Database UTC controls expiry.
Missing/invalid event metadata is suppressed and invalid stored names become null.
Groups sort by newest remaining original contribution, then event ID; expiry is
the earliest remaining contribution expiry. Cleanup deletes at most 500 expired
rows per replica per minute, including when there are no subscribers.

Each replica shares activity polling across its subscribers (at most once per two
seconds), with latest-state single-slot buffers. Counts batch the union of requested
IDs into queries of at most 101 IDs. Recommendation and Interested totals are
authoritative replacements; private Interested choices contribute only to totals.
No save counts or named interest activity are published. Count updates preserve
viewer selections, privacy controls, drafts and collection order. Confirmed writes
invalidate older callbacks and trigger fresh reads.

## HTTP and operations

- `GET /api/v1/community/activity`: bounded snapshot, no filters/pagination.
- `GET /api/v1/community/activity/stream`: SSE `activity.snapshot`, optional
  `counts.snapshot`, `heartbeat` every 15 seconds, and content-free `unavailable`.
- `GET /api/v1/events/activity-counts`: repeated `event_id`, at least one.

Both count inputs accept at most 101 distinct provider IDs and 32768 query bytes;
unknown parameters/invalid IDs are 400. Unknown valid IDs have zero totals.
Responses use `private, no-store`; SSE additionally uses `no-transform` and
`X-Accel-Buffering: no`. No CORS is added. SSE has `retry: 5000`, no `id` or replay
cursor, and ignores Last-Event-ID. Pre-header failures use problem JSON (400,
429 with Retry-After, or 503); post-header failures send `unavailable` and close.
See the [OpenAPI contract](../internal/api/openapi.yaml) for schemas/examples.

`LIVE_ACTIVITY_ENABLED=false` is the default. Enabling requires accounts and
MariaDB at startup. Use one value across replicas. `LIVE_ACTIVITY_MAX_STREAMS`
defaults to 256 (1–4096); `LIVE_ACTIVITY_STARTS_PER_MINUTE` defaults to 60 per trusted
client IP (1–600). Streams have five-second write deadlines and stop on cancellation
and application shutdown before SQL closes. Operational logs contain connection
totals, poll timing and failure messages, not names or payloads.

Follow the [deployment](deployment.md#live-community-activity) drain/migrate/grant/
start sequence. Disabling leaves recommendation writes usable; re-enabling exposes
only still-eligible recent entries, without backfill. Related guides:
[accounts](accounts.md), [recommendations](event-recommendations.md),
[persistence](persistence.md), [UI](ui-components.md).

## Verification

Local validation on 2026-10-15 passed: `go test -race ./...` with isolated real
MariaDB fixtures, `go vet ./...`, the generated asset/binary build, and final Docker
`make check`. OpenAPI lint passes with only the existing two health-probe warnings.
The styling detector reports existing provider-font/token advisories, with none
introduced by the live region. Desktop and 320px captures were inspected.

Real MariaDB tests cover concurrent first publication, rollback, grouping, name
clearing/invalid-name fallback, private-interest aggregate semantics, withdrawal,
reactivation, expiry, disabled publications and separate-pool delivery within five
seconds. HTTP tests cover public projection, query validation, SSE framing,
capacity, shared polling and cancellation.

The opt-in browser journey uses two replica-local hubs and production routes:

```sh
MARIADB_TEST_ADDR=127.0.0.1:3307 \
LIVE_ACTIVITY_BROWSER_SCRIPT=../../scripts/live-activity-browser.cjs \
PLAYWRIGHT_MODULE=/path/to/node_modules/playwright \
go test ./internal/persistence -run TestMariaDBLiveActivityBrowser -count=1 -v
```

Its 49 checks cover privacy in actual DOM, quiet reconnect, buffered updates,
withdrawal focus, hide/pause, offline, storage denial, JSON fallback, desktop/tablet/
320–390px, enlarged text, forced-colors keyboard focus, automated accessibility,
Overview/Back continuity and fresh counts without losing private controls or drafts.
Fake-clock transport fixtures also exercise silent heartbeat stalls and monotonic
expiry despite a mismatched visitor wall clock. This is browser
emulation, not a physical-device or manual screen-reader claim. Validate the actual
deployment proxy's flush/heartbeat behavior, multi-replica load and shared-IP limits
before enabling. The [specification](design/live-community-activity.md) retains the
broader manual and rollout acceptance checklist.

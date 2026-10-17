# Live community activity

**Status:** Delivery baseline implemented; revised toast/navigation UI planned.
Manual/deployment acceptance remains open.
**Date:** 2026-10-15; presentation requirement revised 2026-10-17.
**Scope:** Sitewide live public recommendations, refreshed event counts, and
explicit first-name-only notification attribution.

The original delivery baseline was default-off. Default behavior and recipient
controls are extended by [live activity filters](live-activity-filters.md).
The revised [interface requirements](#interface-and-notification-controls) below
supersede the implemented above-content disclosure: use top-right toasts and a
separate **Happening Now** navigation destination. See the
[current guide](../live-community-activity.md) for implemented behavior.

## Outcome and agreed decisions

Visitors currently using Ticketopia can discover an event because another person
recommended it moments ago. The update opens the event Overview and preserves the
visitor's browsing return context. Guests and signed-in visitors receive the same
public activity; the initial audience is sitewide, across cities.

- Start with **public recommendations** and aggregate recommendation/Interested
  counts for events already on screen.
- Show only an explicitly supplied **first name** in an activity notification.
  The user selected a dedicated optional field rather than deriving a name from
  the existing display name. Use **Someone** when it is absent.
- Deliver through Server-Sent Events (SSE), backed by shared MariaDB state so a
  write on one app replica reaches visitors connected to another.
- Deliver new activity as bounded, dismissible **top-right toast notifications**.
  Add a **Happening Now** navigation link for deliberately browsing recent public
  activity; do not reserve an activity section above ordinary page content.
  Group recommendations for the same event and preserve the existing Community
  collection's anti-bumping order.

Examples, with illustrative content:

> Alex recommended Jazz Night at Blue Note · just now
>
> 3 people recommended Saturday's Food Festival · 2 minutes ago
>
> Someone recommended Sunday at the Museum · just now

This is live in-app public activity, separate from the private reply inbox and
on-sale reminder jobs. Private saves/follows, named Interested activity, Helpful,
discussion text, email/push delivery, attendance, and personalized feeds are
outside this first implementation. Public Interested announcements can be a
separate future slice with their own audience disclosure and revocation handling.

## Original implementation baseline

- `internal/recommendations` owns endorsement rules; HTML and JSON handlers in
  `internal/api/recommendations.go` share that service.
- `internal/persistence/recommendations.go` retains a unique account/event row
  through withdrawal. `setOnce` distinguishes a genuinely first publication from
  reactivation using `first`; its returned `created` flag includes both and must
  **not** be used alone to decide whether to announce.
- `recommendation_feed_events` preserves original event ordering.
  `recommendation_activity` contains private operator measurements, not a public
  activity feed. Neither becomes the notification payload.
- `accounts.Account` has an editable display name but no separate first name.
  Google sign-in currently requests `openid email` and reads no given-name claim.
- `views/layouts/base.templ` owns the shared shell. Existing event-selection,
  return-state and mutation controllers own their respective interactions.

Keep the Go service/repository boundaries and native routes/forms. A focused
`internal/activity` service should own bounded public projections, snapshots and
replica-local stream distribution; persistence implements its repository.

## First-name-only attribution

### Data and author controls

Add nullable `activity_first_name` to accounts and an optional, owner-only field
to the existing profile read/PATCH contract. Add **First name for live activity
(optional)** to Profile & privacy, with a preview and this explanation:

> Live recommendations show this first name. Leave it blank to appear as Someone.
> Your email and last name are not included in live notifications.

- Omitted PATCH field preserves the value; a blank string clears it. Follow the
  existing strict profile decoder and reject JSON null or unsupported fields.
- Normalize surrounding/repeated whitespace. Accept 1–40 Unicode code points,
  letters/combining marks, internal spaces, hyphens and apostrophes; require a
  letter. Reject address/URL syntax, digits, control/bidi-format characters and
  invalid encoding. Show a field error rather than truncating or guessing.
- Support user-supplied compound given names and non-Latin names. This field is
  their chosen first name, not an inferred or verified legal identity.
- Existing accounts start empty. Never backfill from display names, email local
  parts, surnames, Google claims or existing public-profile text.
- Changing/clearing the field updates currently visible activity on the next
  snapshot without creating a new announcement or changing publication order.

The recommendation composer must explain both audiences before Publish:

> Recommendations are public on the event, in Community and on your profile.
> Live activity uses your chosen first name, or Someone.

Existing event/profile recommendation attribution still follows its current
display-name contract. The reduced attribution applies to activity toasts,
Happening Now and their API, not a promise of anonymity across the public site.

### Server-side projection

Use a dedicated allowlisted activity DTO with only `actor_first_name` (nullable)
for attribution. It must never serialize an Account, PublicProfile or existing
recommendations.Item. No surname, display name, email fragment, account ID,
profile link, avatar, bio or recommendation reason belongs in this payload.

Omit those values at the server boundary; CSS blur, masking, hidden markup and
client-side splitting are not redaction. The same rule covers initial HTML,
JSON/SSE, accessible names, titles/data attributes, browser storage and activity
logs. Rendering uses escaped text. A missing/invalid stored name becomes null,
which the client renders as **Someone**. Do not log name-bearing payloads.

## Activity eligibility and lifecycle

| Transition | Live activity | Event counts |
| --- | --- | --- |
| First-ever public recommendation after activation | Add one eligible contribution | Refresh total |
| Retry or edit an existing recommendation | No new contribution/announcement | Refresh if needed |
| Withdraw recommendation | Remove its live contribution | Refresh total |
| Republish a withdrawn recommendation | No new live contribution | Refresh total |
| Change/clear activity first name | Reproject current label | Unchanged |
| Set/remove Interested, private or public | No named activity | Refresh aggregate Interested count |
| Change interest visibility | No activity | Preserve aggregate total |
| Save/unsave, Helpful or private follow | No activity | No live count of these actions |

Eligibility is limited to the latest **10 minutes**, using database UTC time.
Activation never announces old endorsements. A first publication made while the
feature is disabled is not announced later through reactivation.

Group eligible, still-active contributions by event. For one contribution show
the safe first name or Someone; for multiple contributions show **N people
recommended [event]**, with no list of names. N counts eligible contributions in
this window, not all-time endorsements, unique humans, or currently online users.
Use the newest remaining contribution's original publication time for ordering
and relative time. Return at most **20 event groups**, with event ID as tie-breaker.
No user/account quota or automatic sanction is introduced by presentation grouping.

Withdrawal removes a contribution from future snapshots; a group disappears when
empty. Recheck current recommendation/account eligibility while projecting. A
future deletion/visibility policy must invalidate live activity through this same
boundary. Already observed text cannot be recalled from a person's memory.

## Persistence and replica delivery

### Transactional activity projection

Add a small `live_recommendation_activity` table keyed by `(account_id, event_id)`,
with `published_at`, `expires_at`, appropriate foreign keys and an indexed expiry/
publication access path. Store references and times only, not identity text or
copies of event/recommendation content.

1. In the existing first-publication transaction, insert the activity row only
   when `first` is true and the feature is enabled. Recommendation and eligibility
   commit or roll back together, including native-form and API writes.
2. Make withdrawal transactional: update the recommendation and delete its live
   row together. Reactivation never inserts another row. The retained recommendation
   marker is the lasting anti-repeat record even after activity cleanup.
3. Read activity joined to active recommendations, accounts' optional first names
   and stored event snapshots in a consistent database snapshot. No Ticketmaster
   call is needed. Missing/unreadable event metadata suppresses that group rather
   than inventing an event title or URL.
4. Exclude expired entries on every read. Run bounded, idempotent cleanup in small
   batches; multiple replicas may safely clean the same expired range. Size/index
   the aggregate query against a populated ten-minute activity window.

Each replica polls the shared public activity projection at most once every
**2 seconds** while it has subscribers. Broadcast the resulting snapshot to local
connections. Queries are shared across subscribers, not run once per browser.
Joined name changes and withdrawals propagate even when no new recommendation is
published. Send only changed snapshots after the initial one.

The delivery model is a **current, bounded view**, not a permanent notification
log. Full replacement snapshots make reconnect/restart/replica switching simple:
send the latest state, not a backlog of announcements. An action withdrawn before
a poll need not appear at all. No durable per-recipient inbox or message broker is
needed for this slice. Target visible updates within **5 seconds** under healthy
local/integration load; this is not a guarantee during outages or overload.

### Aggregate counts

Refresh recommendation and Interested totals for explicitly requested, currently
rendered event IDs every **15 seconds**, and on initial connection/reconnection.
Use batched SQL reads shared across each replica's active subscriptions; chunk
the union of event IDs into bounded queries. Never scan all events per tick.

Interested totals keep the existing public-count semantics, including private
choices. Send numeric snapshots on this fixed cadence, with no actor, visibility,
per-action timestamp or change event for private interest. Counts are not an
anonymity guarantee. Never publish save counts or named private activity.

Clients replace authoritative totals rather than incrementing deltas. Count
updates do not alter viewer-specific selected state, privacy settings, drafts or
collection ordering. Keep confirmed local mutation state authoritative over older
in-flight snapshots; request fresh counts after confirmation and discard stale
response generations. A failed refresh preserves the last known value as stale,
not zero. Retain existing batching and bounded refresh behavior after load-more.

## HTTP contract

These routes are implemented in `internal/api/openapi.yaml`, including examples,
limits, SSE event schemas and failure behavior.

| Method and path | Contract |
| --- | --- |
| `GET /api/v1/community/activity` | Current public snapshot of up to 20 event groups; no historical pagination |
| `GET /api/v1/community/activity/stream` | SSE activity snapshots and optional visible-event count snapshots |
| `GET /api/v1/events/activity-counts` | Batched public recommendation/Interested totals for supplied event IDs |

Both count routes accept repeated `event_id`, deduplicated to at most **101**
valid provider-scoped IDs, with a bounded total query length. The count-only route
requires at least one ID; the stream can omit IDs. Reject unknown parameters,
invalid IDs and excessive input with the existing structured 400 errors. Unknown
valid IDs have zero counts, matching existing public-interest semantics. The
activity snapshot route accepts no filters in v1; all-city scope is explicit.

Illustrative `activity.snapshot` data (event IDs/titles are examples):

```json
{
  "as_of": "2026-10-15T18:30:00Z",
  "items": [
    {
      "event_id": "ticketmaster:example",
      "event_title": "Jazz Night at Blue Note",
      "city": "London",
      "actor_first_name": "Alex",
      "recent_recommendations": 1,
      "latest_recommended_at": "2026-10-15T18:29:50Z",
      "expires_at": "2026-10-15T18:39:50Z"
    }
  ]
}
```

For a multi-person group `actor_first_name` is null. `expires_at` is the earliest
expiry among its contributions, so a client knows when the group needs refreshing.
URLs are constructed from validated event IDs using existing safe event-route
helpers. JSON reads and SSE share the same service and DTOs.

- `activity.snapshot`: complete replacement of the recent activity list,
  including an empty list after all entries expire or are withdrawn.
- `counts.snapshot`: `{as_of, items:[{event_id, recommendation_count,
  interested_count}]}` covering the requested IDs; no viewer flags.
- `unavailable`: a content-free message when a live read fails after headers
  have been sent; clear activity, mark counts stale and reconnect with backoff.
- `heartbeat`: `{as_of}` every **15 seconds**, observable by the browser's
  EventSource listener even when activity is unchanged. SSE comments alone cannot
  drive the client watchdog because EventSource does not expose them.
- SSE uses `text/event-stream`, UTF-8 JSON data and `retry: 5000`. It deliberately
  has no replay cursor or `id` fields. Ignore `Last-Event-ID`; reconnect always
  sends current snapshots.

Public reads require no login and expose no personalized payload. All responses
are `Cache-Control: private, no-store`; streaming additionally disables proxy
buffering/transformation and compression that delays flushing. Do not add public
CORS access or credentials in URLs. Read/validation failures before streaming
use the existing problem response conventions: 400, 429 with Retry-After for
request-rate limits, or 503 for unavailable capacity/storage/feature.

Start with **256 concurrent streams per replica** and a separate connection-start
limit of **60 per minute per trusted client IP**, using the existing client-IP
policy. Expose bounded configuration and tune from load tests, including shared-IP
visitors; busy streams fail promptly rather than occupy unbounded goroutines.
Use a latest-state bounded subscriber buffer, a **5-second write deadline**,
request cancellation, and explicit shutdown cancellation so slow clients cannot
block others or extend the current HTTP drain deadline.

Close/reconnect when the subscribed event-ID set changes, debouncing rapid
selection/load-more changes. Maintain one stream per document and discard callbacks
from old connections. On return from a hidden tab, BFCache or offline state, clear
stale activity and establish a fresh connection. Use backoff with jitter, bounded
at **60 seconds**, and reset after a healthy connection. Without SSE support use
the JSON snapshots every **15 seconds** while visible, with the same stale rules.

## Interface and notification controls

**Revised requirement — 2026-10-17; not yet implemented.** Replace the shared
above-content disclosure with two connected surfaces: transient activity toasts
and a dedicated Happening Now page. Ordinary page content starts directly, without
an activity panel, collapsed summary, or reserved activity space above it.

**Mode:** Operate. Extend the existing Manrope/plum/light-stone, flat event-first
shell from [DESIGN.md](../../DESIGN.md); retain existing navigation thresholds.

### Top-right activity toasts

- Show new eligible recommendations in a viewport-anchored **top-right** stack,
  outside document flow, for guests and enabled signed-in visitors throughout the
  app. Keep it below any compact header and inside safe-area edges. On narrow
  screens, fit within the available width with readable wrapping and clear controls.
- Show at most **three toasts**, or **one on compact screens**. Group repeated
  recommendations for the same event. Coalesce bursts instead of building a backlog;
  Happening Now is the place to browse the current bounded view.
- Each toast shows the safe first name or grouped count, event title, city and
  relative time, with a real event Overview link, **Dismiss**, and a **Happening
  Now** link. Preserve the current browsing origin/return context for event links.
- Auto-dismiss after **8 seconds** of visible, non-interacting time; suspend the
  timer while hovered or keyboard-focused. Dismissal affects only the toast, not
  the recommendation or its eligibility on Happening Now. A new contribution can
  update a visible event toast without continually resetting its lifetime.
- Initial connection, reconnect, navigation and filter changes establish a quiet
  baseline. Only subsequently observed new publications produce toasts; name edits,
  count refreshes, withdrawals and expiry never do. Deduplicate by event and
  original contribution time. Do not replay missed activity as a toast backlog.
- Do not show duplicate activity toasts while the visitor is on Happening Now;
  that page uses its explicit new-update control. Use one activity stream per
  document, shared with the existing count subscriptions.
- Keep navigation, dialogs and the focused control unobscured; defer a toast when
  the available space or an open dialog/keyboard would block the current task.
  Do not truncate essential text to force it into the stack.

### Happening Now navigation and page

- Add a real **Happening Now** navigation link to **`/happening-now`**, available
  to guests and signed-in visitors when the feature is available. In the default
  desktop Browse group, place it after Community. Add the labeled destination to
  the intermediate rail and compact navigation too; accommodate wrapping/enlarged
  text and preserve the existing destinations and customization preferences.
- Use **Happening Now** as the page heading, with **Public recommendations from the
  last 10 minutes** explaining what visitors can see others doing. This is recent
  public activity, not an online-user count or a broadcast of private actions.
- Show the current filtered snapshot of up to **20 event groups**, with event
  title, city, safe attribution, relative time and real Overview links. Preserve
  the page's filter/scroll/return context when opening an event and coming back.
- For new activity while reading, offer
  **Show N new updates** rather than automatically reordering rows under the reader.
  Apply withdrawals, name redactions and expiries promptly even while new rows are
  waiting. Replays and non-publication changes do not create a new-update indicator.
- Put **Filter live activity**, the confirmed scope summary, and toast visibility
  controls on this page; retain equivalent account preference controls. Confirmed
  filters apply to both the page and toasts. Do not put filter editors inside toasts
  or restore an above-content activity section elsewhere.
- The native page renders a current snapshot at request time, clearly labeled with
  its update time and a **Refresh** link. Links and applicable native filter forms
  work without JavaScript; SSE adds live updates. Keep Community as the separate
  existing recommendation/discussion destination with its anti-bumping ordering.

### Preferences, accessibility and states

- Offer **Hide activity toasts** and a reachable **Show activity toasts** control
  on Happening Now; carry forward existing browser-local hide choices.
  Remember the choice in browser-local storage; storage failure falls back to
  page memory. Store preference/deduplication markers only, never activity names or
  notification bodies. Hiding clears toasts and closes background activity delivery
  outside Happening Now; it does not remove the navigation link or prevent browsing
  that page. Ordinary event counts and participation remain usable.
- Honor a signed-in recipient's existing **Pause all notifications** setting by
  suppressing automatic activity display and streaming. Account live-disable also
  remains authoritative. Happening Now stays reachable and shows the applicable
  paused state and resume/settings controls; visiting it never silently re-enables
  notifications. Guests use local toast controls.
  Recheck preferences at page entry, focus/BFCache and at most every 30 seconds
  while visible; a saved preference applies immediately in that document. Explain
  the effect in Preferences without changing email permission or reply frequency.
- Do not create per-user unread records or add these entries to the reply inbox.
- No sound, flashing, focus stealing or forced navigation. Use one debounced polite
  status message for new activity, not an announcement of every count. Keep keyboard
  focus stable; if a focused toast/row is dismissed or removed, restore focus to its
  prior usable control or the page's update control only if it has not already moved.
- Use wrapping titles/names, 44px actions, visible outline focus, forced-colors
  support and reduced-motion behavior. Essential information stays browsable on
  Happening Now for its eligibility window after a toast disappears.

| State | Toasts | Happening Now |
| --- | --- | --- |
| Connecting | No toast | Quiet connection status; no fabricated activity |
| Connected, empty | No toast | “No recent recommendations. Explore Community.” or explicit filtered no-matches state with Reset |
| Connected, populated | New publications only | Current event groups and explicit update control |
| Interrupted/unavailable | Clear toasts; no repeated error toasts | Clear attribution immediately; “Live activity is reconnecting” and native Community link; retain stale counts |
| Silent stalled connection | Clear toasts | A **30-second heartbeat watchdog** clears activity and reconnects |
| Locally hidden | No toasts | Current activity remains browsable; offer Show activity toasts |
| Account paused/live-disabled | No toasts or activity stream | Explain pause/disable and provide applicable resume/settings control |
| Feature disabled | No enhancement | Omit nav link; direct entry explains unavailability and links to Community |

Expire or refresh a group in both enhanced surfaces at its server-supplied expiry,
even when the page buffers new updates or a toast's dismiss timer is suspended.
Derive remaining lifetime from server `as_of` and monotonic elapsed time rather
than assuming the visitor's wall clock is correct.
A reconnect obtains the authoritative current list before
showing any names. Keep recommendation composers and drafts mounted during all
live updates. Live activity never reorders Discover, Saved, Interests, Radar or
the existing Community collection.

## Configuration and implementation slices

Introduce `LIVE_ACTIVITY_ENABLED`, default **false**, requiring enabled accounts
and MariaDB. Validate dependencies at startup; include it in `.env.example` and
deployment documentation when implemented. Use one setting consistently across
replicas. Runtime shutdown stops activity workers/streams before closing SQL.
Operational logs cover connection totals, poll failures and timing without actor
names or payloads; a separate operational dashboard is not part of this slice.

Use the next migration version available at implementation time. Include the
account column, activity table, indexes, runtime grants and startup probes in the
normal drain/migrate/reconcile/start workflow. Do not reserve or change the current
schema version in this specification. Disabling delivery leaves recommendation
writes usable. Re-enabling shows only still-eligible recent entries quietly; it
never backfills publications made while disabled or replays expired activity.

1. [x] **Identity:** owner-only first-name storage, validated profile API/form,
   explicit audience copy, safe activity projection and empty-value fallback.
2. [x] **Durable activity:** transactionally record first publications/remove
   withdrawn eligibility; bounded grouping, expiry and batched count reads.
3. [x] **Delivery:** shared per-replica polling, JSON/SSE contracts, bounded
   subscribers, reconnect/cleanup/shutdown and feature configuration.
4. [x] **Original web baseline:** shared shell disclosure, first-name-only rows,
   pause/hide, grouping, in-place counts and Overview/return integration.
5. [ ] **Revised presentation:** replace above-content activity with top-right
   toasts, Happening Now navigation/page, shared filters and preserved controls.
6. [ ] **Verification/docs:** checks below, implemented guides/OpenAPI/history,
   generated assets and release checklist updates after verified delivery.

## Acceptance and verification

See the [current feature guide](../live-community-activity.md) for implementation,
configuration and local verification. Original delivery checks remain applicable;
the revised presentation needs its own checks below. Physical devices, manual
screen readers and target proxy/multi-replica load still require rollout verification.

### Meaningful regression coverage

- Prove that full display names, last names, email/local parts, bio, account IDs
  and profile URLs never enter notification JSON/SSE/HTML/attributes/log records.
  Cover absent names, email-like values, compound/non-Latin names and malicious
  markup/control input. Inspect actual serialized responses and browser DOM.
- Exercise guest, owner and another-user recipients. Private saves/follows and
  private/public interest actions produce no named activity. Counts match existing
  visibility rules, including private Interested in aggregates only.
- Against real MariaDB, verify commit/rollback, concurrent first publication,
  duplicate requests, edits, withdrawal, reactivation, expiry and activation with
  existing data. A duplicate or reactivation never produces a new live row.
- Connect clients to two app instances sharing MariaDB; publish on A, observe on
  B within the healthy target. Repeat for withdrawal/name clearing, process restart,
  reconnect to another replica and disconnected-period changes.
- Verify shared SQL query counts for subscribers watching the same event IDs and
  bounded batches as the distinct-ID union grows. Cover event-ID limits, read
  consistency under concurrent writes, slow-client isolation, cancellation,
  heartbeat/watchdog, graceful drain and unavailable/disabled responses.
- Browser fixtures: snapshot replacement and duplicate suppression, fresh counts
  without changing private controls, explicit grouping/update controls, withdrawals
  while focused, offline/reconnect/BFCache, pause/hide/storage failure, SSE fallback,
  and event Overview/Back continuity without draft loss.
- Revised presentation: no above-body activity section or reserved gap; top-right
  placement, bounded bursts, timed/manual dismissal and focused/hovered lifetime;
  quiet baselines and no duplicate toasts on Happening Now. Exercise its nav link
  on every layout, native page/Refresh, shared filters, local-hide versus account
  pause, prompt redaction/expiry in both surfaces, and event/Back continuity.
- Check desktop/tablet/320–390px, 200% text, keyboard/automated accessibility,
  forced colors and reduced motion. Manually exercise a screen reader and real
  phone; quiet updates must not interrupt composing or obscure navigation.

### Delivery checks

Run affected package/SQL tests during implementation, then `go test -race ./...`,
`go vet ./...`, OpenAPI lint and relevant browser journeys for the completed
cross-package feature. Regenerate templ/scripts/CSS through the documented npm
workflow and retain committed artifacts. Run `make check` after final asset/build
or repository-reading test changes. Validate actual proxy streaming/heartbeats
and multi-replica behavior in the target deployment before enabling the feature.

Publish a current feature guide only once implemented; link the account,
recommendation, persistence, UI and deployment changes. Check delivery milestones
only after their user-facing behavior passes verification. This document alone
does not complete any implementation or release-verification item.

# Default-on, filtered live activity

**Status:** Parts 1–4 and the toast/Happening Now presentation checkpoint implemented
and locally verified. Parts 5–6 remain planned.
**Date:** 2026-10-16; presentation requirement revised 2026-10-17.
**Extends:** [Implemented live activity](../live-community-activity.md) and its
[original specification](live-community-activity.md).

## Recommendation

Make live activity available by default, with **Everywhere · All event types ·
All events** as the initial view. Let recipients narrow it using public event
facts and their own private connections. Present new activity as **top-right
toasts**, with a separate **Happening Now** navigation page for browsing the feed;
replace the above-content disclosure. Follow the revised
[interface contract](live-community-activity.md#interface-and-notification-controls).
Preserve first-name-only attribution, the ten-minute eligibility window, twenty
event groups, explicit page updates, authoritative counts and quiet reconnects.

The performance design is **one shared recent-event index per replica**, reusable
filter results, and incrementally refreshed private membership sets. Filtering
the existing global top twenty is incorrect. Running the whole feed query once
per browser is too expensive. Filter the complete eligible candidate window and
apply the twenty-group limit last.

Implement city/type/entity/personal filters first. Radius filtering is a defined
extension after stored event-coordinate coverage and geographic tests are ready.
No new named activity type, ranking algorithm, notification inbox, email or push
delivery is needed for this extension.

## 1. Default-on behavior and existing pauses

Separate three controls:

1. **Deployment availability:** keep `LIVE_ACTIVITY_ENABLED=false` as an operator
   kill switch. Recommended new unset behavior: enable when accounts and MariaDB
   are enabled; otherwise leave the enhancement unavailable. Explicit `true`
   without those dependencies remains a startup error. All replicas resolve the
   same value, including the recommendation writer's eligibility decision.
   Update `.env.example` to explain the dependency-aware default; do not make its
   account-disabled discovery setup fail by inserting an unconditional `true`.
   Existing explicitly configured `false` values remain off until an operator
   changes them; a new default does not rewrite deployment configuration.
2. **Recipient preference:** new `live_activity.enabled` defaults to true for
   guests and signed-in accounts. Enabled means top-right toasts for new activity
   while visible, with quiet initial/reconnect snapshots and a separate Happening
   Now page. It does not add an activity section above ordinary page content.
3. **Overrides after migration:** local hide and account-wide notification pause
   suppress automatic toasts/background activity delivery. Local hide still allows
   deliberate browsing on Happening Now; account pause/live-disable shows that
   page's paused state. Existing browser-local hides survive the UI replacement;
   account pauses are reset only by the original migration specified below. A person
   can pause again at any time; later upgrades must respect that choice.

### Agreed migration: enable notifications for existing accounts

**Product decision:** existing accounts, as well as new accounts, should start
this extension with account notifications enabled and live activity enabled.

- Change the database/application default for new accounts to
  `notifications_paused=false` and `live_activity.enabled=true`.
- In the one-time upgrade migration, set `notifications_paused=false` for **all
  existing accounts**, including previously paused accounts, and initialize their
  new live preference as enabled with the all-events filter defaults.
- Apply this as a versioned data migration during the normal drained-write upgrade,
  not a startup, login, page-entry or recurring background update. Once migration
  completes, a later pause or live-disable choice must survive restart, migration
  reruns, reconnect and deployment of subsequent parts.
- The current boolean has no reliable history of whether it was defaulted or
  selected by a person. This decision intentionally resets both cases; do not infer
  intent from `updated_at`. If provenance is recorded, label this reset as migration
  policy, not a user action, and distinguish subsequent explicit preference changes.

Clearing the global pause also allows already-configured reply notifications and
email reminders to resume under their existing permissions, frequencies and
scheduling rules. Email permission and subscription records remain independent;
the migration does not enroll accounts in a channel or reset delivery history.
Explain the enabled state and the available pause/live-disable controls in account
preferences. Browser-local hiding can still suppress toasts on a particular device
even when that account's notification preference is enabled. The presentation
checkpoint does not repeat this migration or reset any later choices.

## 2. Filter semantics

All filters refer to the **event**, never the recommender's location or identity.
Changing filters changes the recipient's view, not publication eligibility.

| Dimension | Choices | Recommended default |
| --- | --- | --- |
| Location | Everywhere; selected city + country; later radius | Everywhere |
| Event type | Selected catalog categories; later genres | All types |
| Personal connections | All events; any selected connection sources below | All events |
| Artists | One or more exact provider-scoped artist IDs | Any artist |
| Venues | One or more exact provider-scoped venue IDs | Any venue |

**Composition:** OR within a dimension, AND across dimensions. For example:

> Berlin AND (Music OR Arts) AND (Saved OR Interested OR Followed artist)

An artist list matches any listed artist; a venue list matches any listed venue.
Selecting both lists means an artist match **and** a venue match. No unrestricted
Boolean-expression language is exposed. Empty public lists mean no restriction.
The UI labels personal sources **Match any of these connections** to avoid implying
that an event must be both saved and marked Interested.

### Personal connection sources

- `saved`: the recipient currently saves the event.
- `interested`: the recipient currently has an Interested choice, private or public.
- `followed_artists`: at least one of the event's artists is currently followed.
- `followed_venues`: at least one of the event's venues is currently followed.

The existing product follows artists, venues and discussions; it has no generic
“follow event” record. Label these actual relationships explicitly. An eventual
`followed_discussions` source must say **Events with conversations I follow** and
have its own specification; it is not an alias for following an event.

`connections.mode=any` requires at least one source. `mode=all_events` requires an
empty source list. No matching personal choices means an honest empty feed, never
a silent fallback to all events. Personal filtering requires authentication;
guests get a sign-in explanation, not access to another account's selections.

Private saves/follows/interest remain private. A match only selects an already
public recommendation for this recipient. It does not create an announcement,
change Interested visibility, or expose a saved/followed flag in the public DTO.

### Location contract

- First release uses the current Community interpretation: first venue's city and
  country with the documented per-field event-place fallback. Use the same stored
  projection and literal equality, not substring/wildcard searches.
- Require city **and** two-letter country for a city filter. Normalize whitespace
  and country case consistently with existing filters. City matching follows the
  existing database collation. This is a city-label match, not a precise municipal
  boundary; equal city names within a country remain a documented limitation.
- **Use browsing city** copies the currently selected city/country into the live
  filter after confirmation. It does not silently follow later navigation or
  inferred IP location. The user can choose another city without changing Discover.
- Missing location matches Everywhere only. Never invent coordinates or request
  browser geolocation simply because live activity is enabled.

### Radius extension

Use a chosen place center, with 25 / 50 / 100 km options initially. A canonical
place ID resolves to stored coordinates; no geocoder or provider call belongs in
the streaming path. Precise browser location is not required by this design.

Project the first venue's valid coordinate **pair**, falling back to an event-place
pair only when that pair is absent; never combine latitude and longitude from
different sources. Validate ranges and use WGS84 great-circle distance, inclusive
at the radius boundary. Missing coordinates are excluded from radius views with
a visible coverage explanation. Venue IDs match the full venue list independently.

Use an in-memory bounding-box prefilter on the already bounded candidate index,
then exact distance. Handle the antimeridian, poles and longitude wrapping. A
spatial database index is justified only by measured workloads that outgrow this
candidate index. Do not approximate final membership by geohash/cell equality.
Canonical place resolution and coverage checks must ship before enabling radius;
the first release must reject unsupported radius requests rather than ignore them.

## 3. Persisted preference model

Use a focused, versioned owner-only preference resource, independently of email
permission and reply frequency. A proposed full representation:

```json
{
  "version": 1,
  "enabled": true,
  "filters": {
    "location": {"mode": "city", "city": "Berlin", "country": "DE"},
    "category_ids": ["catalog-category-id"],
    "artist_ids": [],
    "venue_ids": [],
    "connections": {"mode": "any", "sources": ["saved", "followed_artists"]}
  }
}
```

The category ID is illustrative, not an invented catalog entry. Worldwide default
uses `location:{"mode":"all"}`, empty ID lists and `connections.mode=all_events`.

- Bound category IDs to 16, artist IDs to 16 and venue IDs to 16; deduplicate and
  sort before hashing. Use existing provider-ID validation. Resolve labels outside
  the hot path using existing lookup services and their authentication contracts.
- Reject unknown versions, fields, nulls, invalid enums, duplicate JSON keys,
  conflicting location fields and oversized input. Use an 8 KiB preference body
  bound. Preserve the prior confirmed preference on any error.
- Signed-in preferences sync through MariaDB. Guests may store this small public
  filter configuration and enabled/hide preference locally. Never store names,
  feed bodies, private relationship lists or private membership bitmaps in browsers.
- Account settings become authoritative after sign-in. Do not automatically upload
  guest filters or overwrite saved account settings. Offer an explicit reuse action.
- Use a preference revision/ETag and conditional replacement to avoid one tab
  silently overwriting another. No-op saves retain the revision.

## 4. HTTP boundaries

These are **proposed** additions. Update `internal/api/openapi.yaml` only when
implementing them, with examples, schemas, errors and SSE events.

| Route | Contract |
| --- | --- |
| `GET /api/v1/community/activity` | Public event-fact filters; no recipient personalization |
| `GET /api/v1/community/activity/stream` | Same public filters and optional visible-event count subscriptions |
| `GET /api/v1/me/community/activity` | Authenticated recipient's confirmed filters and current matching public snapshot |
| `GET /api/v1/me/community/activity/stream` | Same private filter scope, SSE and optional counts |
| `GET /api/v1/me/live-activity-preferences` | Owner-only versioned configuration and ETag |
| `PUT /api/v1/me/live-activity-preferences` | Full idempotent replacement; existing cookie CSRF rules and `If-Match` |
| `GET /api/v1/events/activity-counts` | Existing public aggregate semantics; feed filters do not affect counts |

Public reads accept a bounded `filter` JSON query object containing only location,
category IDs, artist IDs and venue IDs; omission means the current unfiltered view.
Limit decoded filter JSON to 4 KiB and the entire query to the existing 32768 bytes.
Retain repeated `event_id` exclusively for the existing count subscription, with
101 distinct IDs maximum. Reject private connection fields on public routes.
Canonicalization is server-side; caller-supplied cache keys are not trusted.

Private feed reads use stored preferences, not account IDs or private ID lists in
URLs. Support an optional expected `preferences_revision`; stale revisions return
409 before streaming so the client reloads its settings. After headers, emit a
content-free `settings.changed` control event with the new revision and close.
The client clears activity, fetches owner settings and reconnects quietly. A
disabled/paused private view returns an explicit paused state, not an empty-success
claim that no matching recommendations exist.

Keep `activity.snapshot`, `counts.snapshot`, `heartbeat` and `unavailable` from the
current contract. Activity items keep their dedicated first-name-only DTO; do not
append private matching explanations or relationship flags. A private settings/
pause envelope is a separately allowlisted owner-only response, not actor data.

All responses remain `private, no-store`; streaming also disables buffering and
transformation. Native EventSource uses same-origin cookies. API clients may use
normal authorization headers; never credentials in URLs. Personal routes return
401 on missing/expired credentials, not a broadened public response. Conditional
preference writes use 412 for a stale ETag and 428 when the precondition is absent.

Treat stream authentication as a bounded lease, not a one-time permanent check.
Batch revalidation of active credentials at most every 30 seconds and close
immediately at known expiry; local logout/revocation invalidates local subscriptions
immediately. Cross-replica credential revocation is bounded by that 30-second check.
On account switching, discard old callbacks, private caches and stale preferences.

## 5. Efficient delivery architecture

### A. Build the full eligible candidate generation once per replica

Retain `internal/activity` as the service and `internal/persistence` as its repository.
The repository returns an internal candidate generation distinct from the public
twenty-item DTO. While subscribed, run one shared projection cycle at most every
two seconds, using database UTC and a consistent read transaction.

Read all eligible event groups, active recommendation/account eligibility, safe
first-name attribution, ordering/expiry times and event metadata revisions. Do
not select the global top twenty first. Group before applying recipient predicates,
so a filtered event still reports all its eligible contributions, not just those
from people connected to the recipient. A person unfollowing an artist changes
their view, not a recommendation's eligibility or anyone else's aggregate.

Read lightweight columns and indexed facets, not whole event JSON on each tick.
Reuse normalized public metadata keyed by `(event_id, data_as_of)`; load changed
facts in batches in the same read snapshot. Name changes must reproject even if
the event metadata revision is unchanged. Never reuse name-bearing output across
a failed poll or beyond contribution expiry.

Build inverted event sets for city/country, category, artist and venue. For a
canonical public filter, intersect those sets, walk the shared sorted candidate
order and take the first twenty matches. Identical public filters share the match
result and encoded snapshot. Filter list order/duplicates cannot create new cohorts.
Advance the content generation only when relevant content changes; `as_of` alone
is not an invalidation. Give candidate IDs stable bitmap slots independently of
display ordering, and clear/reversion slots before reuse. A reorder or first-name
edit must not force rebuilding every account's relationship membership.

### B. Personalization is membership, not a second feed

For each **active account**, retain bitmaps over the bounded candidate event IDs
for saved, interested, followed-artist and followed-venue matches. Multiple tabs
for one account reuse those bitmaps. Allocate/load only sources that its active
filters need. Combining source bitmaps with the public event predicates is cheap;
private relationship IDs never leave the server.

Maintain a monotonic `activity_membership_revision` per account. Bump it in the
same transaction as an actual save/unsave, interest set/remove, artist follow/
unfollow or venue follow/unfollow. Idempotent retries do not bump it. A change to
interest visibility alone does not change membership. Keep preference/auth revision
tracking separate so a display-name or email edit does not invalidate every match.
Audit all native/API mutation paths and transaction lock order when adding revision
writes. Reuse the repository's bounded deadlock retries; never replay an uncertain
commit or hold an account-wide lock while fetching provider data.

- Poll revisions for the union of active personalized accounts in chunks of 128
  on the shared two-second cycle, not once per connection.
- Recompute only changed/new accounts, newly eligible candidates, and candidates
  whose artist/venue metadata changed. Removals/expiry delete their bitmap entries.
- Match owner chunks against candidate chunks of at most 256 event IDs, using
  indexed relationship joins and `UNION ALL` branches for needed source types.
  Reduce duplicates to source bits; never multiply a group's recommendation count.
- Do not load an account's complete historical saved/interested/followed collections
  just to discover a few recent matches. Do not probe each account/event pair with
  a separate SQL query. Use the existing composite keys and candidate-scoped joins.
- Load each work batch in a consistent snapshot with its membership revisions.
  If an account revision changes during multi-batch assembly, discard/retry that
  account's assembly. Publish a private view only when its required membership
  generation is complete. A local mutation invalidates it immediately.
- Artist/venue joins must verify the candidate metadata revisions they used.
  If facets changed after the public generation was built, rebuild those candidates
  rather than combining newer relationship matches with older event facts.

Across replicas, revision polling delivers save/follow/removal changes without a
broker. Target the existing five-second healthy update latency for changed filters
and membership too. During rebuild, old membership is not silently treated as
current: clear the affected view and show refreshing/unavailable if necessary.
Public cohorts can continue when a private membership read fails.

### C. Scope caching and work correctly

Public match cache key: canonical public-filter digest + candidate generation.
Personal match key: account/auth scope + canonical full-filter digest + candidate
generation + membership/preference revisions. Never key personalized output only
by city/category or a client-provided owner identifier.

Use one document stream, coalesced requests, latest-state single-slot subscriber
buffers and per-view change detection. Reuse encoded payloads only for identical
authorized views. Snapshot/fallback HTTP reads use the same coalesced generation
builder rather than creating a second unbounded polling path. A cold snapshot read
may initialize one shared generation; it must not start a permanent worker without
subscribers. Bound direct-read starts as well as SSE starts, including 15-second
fallback clients, with documented 429/Retry-After behavior.

Send an already-valid warm activity view immediately on subscription; it need not
wait for the next two-second poll or initial count read. Counts can follow in their
own frame. An initial private view still requires a confirmed account/preference/
membership scope, and an old or failed candidate generation cannot be served merely
to meet a latency target.

Keep the ordinary 15-second count cadence and batched union of visible event IDs.
A changed filter or joining subscriber must not reset everyone else's count timer
or trigger rereads for all subscriptions. Coalesce initial counts for joining IDs;
membership matching never affects public count semantics or viewer-selected state.

Complexity is not literally constant: public projection scales with active
contributions/events, membership work with changed accounts/candidates, and fanout
with connections. Database work must not scale linearly with duplicate tabs or
identical public filter combinations. Benchmark those dimensions separately.

### D. Explicit resource bounds and overload behavior

Initial proposed budgets, to validate rather than assume from available RAM:

| Resource | Starting bound |
| --- | --- |
| Streams | Existing 256 per replica; current start-IP limit remains 60/minute |
| Internal recent candidates | 10,000 event groups; query cap + 1 to detect overflow |
| Projection/membership/encoded-view caches | 64 MiB combined accounting budget per replica |
| Extra snapshot-only cohorts | 128, evicted after 30 seconds without reuse |
| Membership batches | 128 owners × 256 candidate IDs maximum per work unit |
| Concurrent activity SQL jobs | 2 per replica, within the existing SQL pool budget |
| Pending rebuild work | 64 coalesced work units; do not spawn a goroutine per pair |
| Writes/heartbeat/watchdog | Existing 5 / 15 / 30 seconds |

Include internal facet sets and temporary generation assembly in memory accounting;
an entry-count cap alone is insufficient. The cache budget is not a total RSS cap.
Keep only current/in-progress generations and evict unreferenced owner/cohort data.
A membership rebuild beyond the queue/query/memory budget returns unavailable for
that scope; retry with bounded backoff. Existing full-window overflow makes the
affected projection unavailable, with an observable reason, rather than publishing
a misleading truncated/empty personalized feed. Do not fall back to per-user SQL
polling or silently widen filters. Query deadlines also bound aggregate CPU work:
`LIMIT` alone does not bound rows scanned by `GROUP BY`.

Sustained full-window overflow is a trigger to design measured shared geographic/
category partitions with complete cross-partition merge semantics. It is not a
reason to increase the cap indefinitely or quietly switch to global-top-N filtering.

Three 2 GB replicas at 256 streams each offer a configured ceiling of **768 live
tabs**, subject to load distribution. This is not a tested capacity guarantee.
All three replicate the shared read workload; they do not divide one global poll.
Four GB per replica does not justify increasing streams without measurement.

## 6. Persistence and extension points

Use the next migration version available at implementation time, with normal
drain/migrate/reconcile/start and runtime probes. Do not reserve a version here.

- Add a versioned live preference record with monotonic revision and default-on
  representation, plus account membership revision and pause-provenance tracking.
- Reuse `event_snapshots.community_city/community_country` and
  `event_search_facets(event_id,kind,value)` / `(kind,value,event_id)` for matching.
  Saved/Interested and artist/venue follows already have account/resource keys.
- Add a lean public event projection only for facts absent from efficient columns
  (for example title and validated primary coordinates). Update it atomically with
  accepted snapshots/search facets, under existing event-first lock order. Older
  snapshots must not overwrite newer filter facts. Backfill from stored metadata,
  without provider calls or activity eligibility backfill.
- Candidate reads, cache revisions and private joins must use the same authoritative
  snapshot metadata. Do not mix a newer Discover catalog revision with an older
  activity title/location. Preserve the existing snapshot freshness guard.
- Add indexes only after EXPLAIN/ANALYZE and populated-window benchmarks. Do not
  index arbitrary JSON fields or every possible combination of recipient filters.

Keep typed extension points inside `internal/activity`: a normalized filter model,
public event predicates, and an allowlisted registry of private membership sources.
Each source declares its authentication requirement, repository batch reader and
mutation/revision triggers. Wire only compiled, tested source types; no runtime
plugin system or user-supplied SQL/expression engine is needed.

Later genres, radius or followed-discussion matching require their own validated
fields, indexing/coverage policy, canonicalization version and acceptance tests.
Changing activity kinds requires separate audience/eligibility policy; it is not
just another filter enum. Older clients retain all-events behavior when filters
are omitted. Unknown fields/versions fail explicitly rather than broadening scope.

## 7. Recipient experience

**Revised requirement — 2026-10-17; implemented locally 2026-10-18.** Extend the
existing Manrope/plum/light-stone shell with top-right activity toasts and the
**Happening Now** navigation destination at `/happening-now`. Follow the
[toast/page contract](live-community-activity.md#interface-and-notification-controls)
for placement, responsive navigation, dismissal, accessibility and native fallback.
Remove the live section above ordinary page content. Happening Now owns the
deliberate recent-activity browsing experience; suppress duplicate toasts there.

On Happening Now, show a confirmed summary such as **Berlin · Music · Saved or
followed artists** and a **Filter live activity** disclosure with location, event
types, supported personal sources/artist/venue pickers, **Apply** and **Reset
filters**. Keep equivalent account preference controls; do not embed editors in
toasts. Both surfaces use the same confirmed filters and one document stream.
Essential labels wrap; no notification counts imply online users.

Reset means Everywhere / all types / all events, not clearing a pause or showing
hidden toasts. Keep unsaved filter edits local; one Apply commits them and establishes
the new stream generation. Coalesce rapid changes and discard old callbacks.

On Happening Now, preserve empty/reconnecting/paused states and add **No recent
recommendations match these filters** with an explicit reset action. Never widen
automatically. Matching failures retain an unavailable state, not a zero-match
claim. A filter/membership/metadata change quietly replaces the matching view;
it must not turn old recommendations into new toasts or new-update indicators.
Deduplication uses original contribution times across filter switches in the document.

Applying filters cannot remount event composers, erase drafts, reorder collections,
steal focus or change their selected participation state. Withdrawals, actor-name
redactions and expiry still apply to visible toasts and buffered page rows. Use the
existing Overview/return helpers; Happening Now provides a native snapshot/Refresh
and filter forms, while Community remains independently useful without JavaScript.

## 8. Verification and performance gates

Correctness and privacy:

- Global rank 21+ can become rank 1 in a filtered feed; compare optimized output to
  a simple complete-window reference implementation across combined filters.
- OR within / AND across dimensions; any-connection semantics; empty lists;
  invalid combinations; unknown location; identical city names across countries;
  category and provider-ID case rules; multi-artist/multi-venue duplicates.
- Actual guest/owner/other-account JSON, SSE, DOM, browser storage and log checks:
  chosen first names only; no private membership, account IDs or preference leaks.
- Save/interest/follow changes, metadata corrections and pause/settings changes
  on replica A affect B/C; withdrawals/redactions/expiry remain prompt with buffered
  rows, cached views, process restarts and changed credentials. No-op writes stay
  idempotent. Transaction rollback cannot leave a revision ahead of its state.
- Default-on guests/new accounts; one-time enabling of previously paused and
  unpaused existing accounts; preserved browser-local hides; pauses made after the
  migration survive restart/reruns/later parts. Verify independent channel permissions
  and delivery history, disabled operator flag, missing dependencies, cross-tab
  settings conflicts, native forms and fallback.
- Shared filters on toasts and Happening Now, quiet filter changes, page update
  buffering, no above-content activity section, reachable navigation on all layouts,
  toast dismissal/overflow/focus and native page refresh. Preserve existing hides
  and later account pauses through the presentation checkpoint without a new reset.
- Radius, when implemented: same point, exact boundary, invalid/missing pairs,
  antimeridian/poles and location coverage; never a live geocoder call.

Load and resource tests must use **three real app instances and shared MariaDB**,
with CPU/database/proxy resources recorded alongside 2 GB container memory limits:

1. 768 connections distributed evenly, then deliberately skewed; repeat with
   shared-IP clients and controlled reconnect bursts. Do not mistake per-IP 429s
   for stream capacity or disable the limiter for the reported deployment test.
2. Many tabs sharing a filter/account versus all-distinct filters/accounts. Assert
   query counts and cache reuse, not only response time. No per-browser provider calls.
3. Recent windows of 100 / 1,000 / 10,000 groups, multiple contributions per group,
   and overflow. Include long titles and large lineups, not only tiny synthetic rows.
4. Sparse and dense saved/followed matches; large historical collections; cold-start
   membership rebuilds; continuous new events; mass unfollows; metadata corrections.
5. Normal browsing and recommendation writes during live load, SQL timeouts,
   disconnected/slow clients, hidden tabs, fallback clients and graceful shutdown.

Proposed acceptance targets under the agreed healthy workload: p95 committed
publication/removal/membership-to-visible latency ≤5 seconds; p95 warm filter
Apply-to-current-view ≤1 second; shared projection p95 ≤250 ms; warm in-memory
filter selection p95 ≤10 ms. Set a measured cold-start target before rollout;
overload must fail promptly, not accumulate work. Keep representative browse/write
p95 latency within 10% of its no-live-load baseline, and memory within the explicit
activity budget. These are test gates, not claims of current measured performance.

Expose bounded metrics for streams, cohorts, candidates, cache bytes/hits, query
count/time, rebuild queue, update latency, 429/503s and overload reasons. Never use
account IDs, city strings, entity IDs, filter hashes or actor names as metric labels.
Validate actual proxy flushing, mobile/keyboard/200% text, automated accessibility,
forced colors, reduced motion and manual screen-reader behavior before default-on
rollout. Keep a documented operator kill switch and preserve preferences on rollback.

## 9. Working parts and milestones

Each part ends in a **runnable, demonstrable checkpoint**, with backend and browser
behavior connected. Complete and verify one part before building on it. These are
useful stopping/review points, not backend-only foundations awaiting a later UI.

- [x] **Part 1:** Existing/new accounts have notifications enabled; live preferences
  and pause controls work on the current live feed.
- [x] **Part 2:** City and event-type filters work on the complete recent window.
- [x] **Presentation checkpoint:** Top-right toasts replace the above-content
  disclosure; Happening Now navigation/page exposes the same filtered activity.
- [x] **Part 3:** Specific artist and venue filters compose with city/type filters.
- [x] **Part 4:** Saved/Interested/followed-artist/followed-venue personalization works
  across replicas.
- [ ] **Part 5:** The combined feature meets measured load, resilience and rollout
  gates and is ready for production.
- [ ] **Part 6, optional:** Stored place centers support tested radius filtering.

Complete the presentation checkpoint after Parts 1–2 and before Part 3; preserve
their verified delivery/filter behavior. Parts 3–5 then build on the preceding
checkpoint. Part 6 follows Part 5 and requires the coordinate-coverage foundation.

### Checkpoint contract

At the end of every part:

- The app builds and runs with the features completed so far. Existing native
  browsing/forms and the operator kill switch still work.
- Expose only completed filter capabilities. Unsupported non-default filter values
  fail explicitly; do not accept a setting that the stream silently ignores.
- The stated demo succeeds through production routes/UI against an isolated local
  database. Record the check results and any remaining rollout-only work.
- Add meaningful regression tests for the part's behavior. Run affected checks
  during development and the repository's broader checks for substantial/shared
  changes. Regenerate relevant artifacts and complete required Docker validation.
- Update implemented OpenAPI/guide sections and feature history for that part only.
  Check its milestone after its exit criteria pass; later parts stay unchecked.

Parts 1–4 are working local/staging checkpoints. Keep production's explicit kill
switch off during development of this extension; default-on production rollout is
Part 5. The Part 1 migration is a real one-time preference change wherever applied,
so apply it to production in that final coordinated rollout, not as an early
schema-only deployment. This specification does not itself change a running system.

### Part 1 — Default-on accounts and usable notification preferences

**Completed locally — 2026-10-16.** Schema 17, conditional owner preference APIs,
native controls and authenticated all-events delivery are connected. Real MariaDB
tests verify one-time reset/reruns, later pause/disable persistence, independent
channel/subscription/receipt preservation, owner conflicts and credential lifecycle.
The production-route browser demo upgrades paused/unpaused v16 identities and
passes 64 checks, including native/JavaScript-disabled controls, fallback, hiding,
account switching and automated accessibility. Full real-MariaDB race suite, vet,
OpenAPI lint, generated build and Docker validation pass. See the
[implemented guide](../live-community-activity.md#default-on-preferences-part-1).
Production migration/default-on rollout, proxy/load, real-phone and manual
screen-reader gates remain Part 5; no production deployment was performed.

**Outcome:** guests and new/existing accounts can use the current unfiltered live
feed automatically, and can pause or disable it through working controls.

**Deliver:**

- Dependency-aware deployment default and explicit kill switch from section 1.
- One-time existing-account notification reset, new-account defaults and initialized
  live preferences, with the default all-events representation and revisions.
- Owner-only preference read/write contracts, conditional saves, native settings
  form, enabled/pause explanations and browser synchronization.
- Working authenticated all-events stream/snapshot scope, credential lifecycle and
  paused-state handling. Reuse the existing public projection at this checkpoint.
- Existing local hide/show, quiet initial connection and count refresh behavior.

**Working demo:** upgrade fixtures containing both paused and unpaused accounts;
sign in as either and see live activity without an enrollment step. Pause again,
restart, and verify it stays paused. A new account and a guest also see the enabled
collapsed region. Disabling the deployment flag removes the enhancement.

**Exit checks:** real migration/rerun tests, post-migration pause persistence,
unchanged channel-permission/subscription/delivery records, owner/other-account
authorization, preference conflicts, session changes and browser hide/pause/fallback.

### Part 2 — Shared candidate index, city and event-type filtering

**Completed locally — 2026-10-16.** Schema 18, complete-window shared projection,
stable bitmap slots, canonical/encoded public view reuse, bounded overload behavior,
public filters and conditional account preferences are connected to the shell and
native account controls. The production-route demo reveals Berlin music behind
25 newer London events and resets to the global view. Its 25 browser checks cover
guest persistence, account sync, native Reset, fallback, Overview/Back, mounted
composer/draft and private participation preservation, keyboard and accessibility;
the existing 64-check live journey also passes. Real MariaDB regressions, the full
race suite plus final affected-package checks, vet, OpenAPI lint, generated build
and final Docker validation pass. SQL projection p95 is 6.7 / 15.1 / 131.3 ms for
100 / 1,000 / 10,000 synthetic groups; overflow fails closed. The
[implemented guide](../live-community-activity.md#verification) records resource
bounds, reproducible baselines and the existing enlarged-text Community pagination
limitation. Three-replica load, proxy, physical-device and manual screen-reader
gates remain Part 5; no production deployment was performed.

**Outcome:** a visitor selects a city and categories and sees the correct recent
matches, including events outside the original global top twenty.

**Deliver:**

- Full-window lightweight candidate generations, required normalized public facts,
  stable bitmap slots, shared public-filter cache and bounded overload handling.
- Public filter validation and owner preference application for location/category.
- Native filter disclosure with Everywhere/city-country, category choices, Apply,
  Reset, saved filter summary and an honest no-matches state.
- Guest-local and account-synced filter preferences; immediate warm-view delivery,
  quiet filter changes and preserved Overview/Back, drafts and participation state.

**Working demo:** populate more than twenty globally newer events outside Berlin,
plus an older eligible Berlin music event. Applying Berlin + Music reveals that
event; resetting restores the all-events view. A second tab with the same public
filter reuses the shared computation.

**Exit checks:** complete-window reference comparisons, category/city normalization,
unknown locations, metadata correction, invalid filters, query-sharing assertions,
candidate/memory caps, and responsive/keyboard filter and return/draft journeys.
Record a populated-window benchmark baseline before adding personalization.

### Presentation checkpoint — Top-right toasts and Happening Now

**Completed locally — 2026-10-18.** Top-right bounded toasts, all navigation layouts,
the native Happening Now snapshot/Refresh/filter page and shared confirmed filters
are connected. Production-route browser journeys pass 65 presentation checks and
30 filter/count/draft checks against isolated MariaDB, including native forms,
pause/disable/hide, account switching/revocation, dismissal/focus/hover timers,
buffering/redaction/expiry, fallback and Overview/Back. Whole-page accessibility,
responsive/enlarged-text checks, the real-MariaDB race suite, vet, Docker validation and generated build
pass. See the [verification record](../live-community-activity.md#verification).
No migration or production preference reset was added; Part 5 rollout gates remain.

**Outcome:** visitors notice new public recommendations through top-right toasts
while browsing, and choose Happening Now in navigation to inspect recent activity.

**Deliver:** replace the shared above-content disclosure, add the native page and
desktop/rail/compact navigation link, relocate shared filter/visibility controls,
and implement bounded accessible toasts under the revised interface contract.
Reuse existing projections, streams, preferences and count behavior.

**Working demo:** load Discover with no activity block above its content; another
visitor's new recommendation produces a top-right toast. Dismiss it, open Happening
Now from navigation and find the still-eligible event. Apply a city/type filter and
verify both surfaces share it; event Overview and Back preserve browsing context.

**Exit checks:** revised presentation acceptance from the original specification,
native snapshot/filter/Refresh, all navigation layouts, quiet reconnect/filter
changes, one-stream reuse, burst/dismiss/focus behavior, and preserved pause/hide,
redaction/expiry, counts and drafts. Update current guides and generated assets
only when implemented and verified; this checkpoint does not reset preferences.

### Part 3 — Specific artist and venue filters

**Completed locally — 2026-10-18.** Exact full-list entity facets compose with
city/type predicates before the twenty-group limit, with canonical shared views
and snapshot-consistent metadata corrections. Both filter editors have authenticated
catalog pickers, selected labels, independent clears, native draft searches and
conditional Apply/Reset. Guest public IDs persist without storing names or changing
lookup authorization. No migration or preference reset was added.
The production-route demo passes 29 browser checks against isolated MariaDB;
existing filter and presentation journeys pass 30 and 65 checks. Mixed-dimension
reference/cache tests, real-MariaDB persistence/correction regressions, the full
race suite, final affected-package checks, vet, OpenAPI lint, generated build and
Docker `make check` pass. See the [verification record](../live-community-activity.md#verification).
Parts 4–6 and production rollout remain planned.

**Outcome:** visitors can narrow the working feed to chosen artists or venues and
combine those choices predictably with location and event type.

**Deliver:**

- Exact provider-scoped artist/venue predicates and canonical ID-list cache keys.
- Real pickers using existing authorized lookup contracts, selected labels, clear/
  reset actions and persisted selections; artist/venue facts stay out of poll-time
  provider requests.
- Multi-artist/multi-venue matching without duplicate rows or inflated recommendation
  counts; metadata revision changes update affected filter results.

**Working demo:** select an artist, then a venue; the feed shows matching events for
that artist at that venue. Select a second artist to demonstrate OR within artists,
then clear the venue to demonstrate independent dimensions.

**Exit checks:** mixed-dimension reference comparisons, provider-ID validation,
multi-entity duplicate suppression, metadata correction, lookup failure recovery,
preference persistence and shared-cache reuse for reordered/duplicate selections.

### Part 4 — Private connection filters with incremental matching

**Completed locally — 2026-10-19.** Schema 19 adds transactional membership
revisions without resetting preferences. Candidate-scoped, snapshot-consistent
joins and incremental per-account source bitmaps power authenticated snapshots and
streams, with bounded rebuilds and private cache separation. Both editors provide
native/enhanced connection controls, guest explanations and quiet empty/matching
views. The separate-pool/two-service production-route demo passes 25 browser checks;
the existing filter/count/draft and presentation journeys pass 30 and 65 checks.
Real MariaDB rollback/concurrency/replica tests, membership reference/reuse tests,
sparse/dense baselines, the full race suite, vet, OpenAPI lint, generated build and Docker `make check`
pass. See the [verification record](../live-community-activity.md#verification).
Production migration, three-replica load and manual rollout gates remain Part 5.

**Outcome:** signed-in visitors can choose Saved, Interested, followed artists and
followed venues, and see membership changes arrive across replicas without a page
reload or exposing their private choices.

**Deliver:**

- Transactional membership revisions on every relevant native/API mutation path.
- Candidate-scoped batched matching, per-active-account membership reuse, revision/
  metadata consistency checks, source composition and bounded rebuild scheduling.
- Authenticated filtered snapshots/streams and matching owner preferences, with
  public/personal cache separation and expiry/revocation/account-switch handling.
- Working **Match any of these connections** controls, guest sign-in explanation,
  zero-match state, and quiet view replacement when membership changes.

**Working demo:** on replica A save or mark an event Interested, then observe its
existing public recommendation appear for that account on replica B. Unsave/remove
interest and watch it leave when no other selected source matches. Repeat with
artist/venue follow and unfollow; another account's view is independent.

**Exit checks:** real transaction rollback and concurrent-write tests, cross-replica
revisions, OR-source correctness, owner/other-account isolation, actual payload/DOM/
storage/log privacy checks, multi-tab query reuse and sparse/dense membership
benchmarks. Confirm counts, private controls and unsubmitted drafts stay intact.

### Part 5 — Performance, resilience and production readiness

**Outcome:** the combined feature is measured under the intended three-replica
deployment and is ready for a coordinated default-on rollout.

**Deliver:**

- Operational metrics, bounded caches/work queues and coalesced SSE/fallback reads
  verified against the full section 8 workload and latency/resource targets.
- Three 2 GB replicas with shared MariaDB/proxy, up to 768 connections, uneven
  balancing, shared-IP starts, cold rebuilds and concurrent ordinary app traffic.
- Stream/fallback recovery, overload, redaction/expiry, slow-client isolation and
  graceful drain; tune limits from results rather than RAM alone.
- Completed guides/OpenAPI/generated assets, migration/grant/default configuration
  instructions, and an operator kill-switch rollback procedure.

**Working demo:** run the connected multi-replica load scenario, publish/change/
withdraw recommendations and alter personal filters while browsing and writing
normally. Demonstrate bounded failure and recovery when a replica or SQL read fails.

**Exit checks:** measured gates from section 8; full race suite, vet, OpenAPI lint,
relevant browser journeys and final `make check`; actual proxy heartbeat/flushing,
real-phone and manual screen-reader checks. Record results and any failed gates.
Once these pass, perform the coordinated production migration and default-on
rollout. Do not mark production deployment complete from local tests alone.

### Part 6 — Optional radius filtering

**Outcome:** visitors can choose a stored place center and a supported radius to
see nearby events without requiring device location permission.

**Deliver:** canonical place centers, coordinate-pair projection/backfill, coverage
reporting, bounding-box plus exact-distance predicates, radius preference/API
validation and working radius controls integrated with the earlier filters.

**Working demo:** switch between 25 / 50 / 100 km around a selected place and show
events entering/leaving at known distances. An event with unknown coordinates is
excluded with the documented coverage explanation.

**Exit checks:** boundary/antimeridian/polar/missing-coordinate cases, corrected
venue coordinates, complete-window reference comparisons, query/cache budgets,
no poll-time geocoding/provider requests, responsive accessibility and renewed
performance gates for geographic workloads.

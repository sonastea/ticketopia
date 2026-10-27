# Live community activity

Live activity is a sitewide view of public recommendations from the last
ten minutes. Guests and enabled signed-in visitors receive new recommendations as
top-right toasts and browse the current filtered view on **Happening Now** at
`/happening-now`, a Community tab. Ordinary pages have no above-content activity section. Community
and other collections keep their existing order.

Parts 1–4 of the [default-on filtering extension](design/live-activity-filters.md)
add default-on preferences and complete-window city/event-type/artist/venue and
private connection filtering. Radius remains unsupported and is explicitly
rejected. Production rollout remains Part 5.

## Default-on preferences (Part 1)

With accounts and MariaDB enabled, an unset `LIVE_ACTIVITY_ENABLED` enables the
enhancement. Explicit `false` remains the operator kill switch; explicit `true`
without both dependencies fails startup. Existing deployment settings are not rewritten.
Keep production explicitly off until the coordinated rollout and performance gates.

Schema 17 resets **all existing accounts**, including previously paused accounts,
to notifications enabled once. New accounts also start unpaused with live activity
enabled. Version 1 starts with Everywhere / all event types / all events. The current
implementation persists version, enabled, normalized filters and revision on the account.
The migration labels pause provenance `migration_policy`, new accounts use `default`,
and subsequent explicit pause saves use `user`. Restart, login and migration reruns
never repeat the reset. Email permission, subscriptions and delivery history are
untouched; configured replies/reminders can resume under their existing rules.

**Preferences → Live activity** has its own native conditional save. **Pause all
notifications** overrides live enablement; **Hide activity toasts** remains device-local
and survives the upgrade. Signed-in settings are authoritative, never uploaded from
guest storage. No feed bodies or private account state are stored in browser storage.

- `GET /api/v1/me/live-activity-preferences` returns version, enabled, filters,
  revision, notification pause and an owner-scoped ETag.
- `PUT` replaces version/enabled/filters with `If-Match` and normal cookie CSRF.
  Missing preconditions return 428, stale/foreign ETags 412; no-op saves retain
  revision. The 8 KiB body rejects unknown fields/versions, nulls and duplicate keys.
- `GET /api/v1/me/community/activity` and `/stream` use stored preferences;
  optional `preferences_revision` mismatch returns 409. Disabled/paused views
  return `{state:"paused",preferences_revision:…}`, not an empty feed.
- Streaming retains the public snapshot/count events and adds content-free
  `settings.changed` with the new revision, then closes. Clients clear and reload
  quietly. Settings and credentials are checked in shared 128-credential batches
  every two seconds, with a 10-second whole-sweep deadline. Failed checks close
  with `unavailable`; known credential expiry and local revocation close immediately.
  Cross-replica revocation is bounded by 30 seconds. Authentication failure never
  broadens an already signed-in document to guest activity.

## City and event-type filters (Part 2)

On Happening Now, open **Filter live activity**, choose Everywhere or a city with its two-letter
country, and select catalog event types. **Apply** commits one change; **Reset
filters** restores Everywhere / all types without enabling a disabled feed or
clearing a pause/hide. Categories match any selection; location and categories
compose with AND. No matches gives an explicit empty state, never a wider feed.
The summary shows confirmed settings; unsaved changes stay in the editor. Edits
made while an Apply is pending, or before a background refresh, are kept rather
than replaced by the confirmed filters, with a note that they still need Apply.

Guests retain only public filter settings in `ticketopia-live-filters-v1` local
storage. Account settings win on sign-in; **Reuse this browser’s filters** copies
them into a draft for explicit Apply. Account changes sync through MariaDB and
conditional revisions. Preferences has equivalent native/JavaScript-disabled
filter controls. Happening Now also renders a native snapshot, timestamp, Refresh
link and guest GET/account conditional POST filter forms without JavaScript.
Native guest selections are URL-scoped; JavaScript shares confirmed public filters
with browser-local toast settings. **Use browsing city** copies an explicitly
selected Discover city/country remembered in this tab, after confirmation. It does
not infer location or track later navigation.

City matching uses Community's authoritative first-venue, per-field event-place
fallback. MariaDB Unicode collation weights preserve literal equality (including
case/accent equivalence and trailing spaces), never substring/wildcard matching.
Country is uppercased; city whitespace normalized. Equal city labels in one
country cannot be distinguished. Missing locations match Everywhere only.
Category IDs are case-sensitive catalog IDs, capped at 16 before deduplication,
sorted before cache keys. Unknown connection sources and radius requests fail explicitly.

Schema 18 adds JSON filter storage and a stored title projection that follows the
winning snapshot automatically, including backfill. It does not reset enabled or
pause choices. Candidate queries read lightweight titles, Community location
weights, metadata revisions and indexed category/artist/venue facets in one consistent read.
All eligible groups are ordered before recipient matching; the twenty-group limit
is applied last. Slots survive reorders/name edits; rebuilt bitmaps clear removed
slots before reuse. Public views share canonical city/category/artist/venue cache keys and
encoded snapshot payloads. Generation fingerprints hash bounded candidates one
at a time rather than retaining a second name-bearing copy of the full window.

Snapshots and streams coalesce through one generation builder per replica, at
most once per two seconds. Valid warm subscriptions receive activity before count
work. Joining count subscriptions read only their IDs without resetting the ordinary
15-second cadence. No provider calls occur in the streaming path; each new city
scope resolves its database collation key once and reuses the bounded key cache.
Bounds: 10,000 candidate groups (cap+1 detects overflow), 64 MiB conservative
candidate/index/assembly/view accounting, two concurrent activity SQL jobs, 64
pending external projection readers (the delivery worker reads outside that queue), and at most 128 extra snapshot-only cohorts beyond
active stream cohorts, with 30-second idle expiry and earlier LRU eviction under
pressure. A candidate's title/location/key facts are bounded to 64 KiB; exceeding
that fails the projection rather than truncating an essential label. This is not
an RSS cap. Repeated city keys and category/artist/venue IDs are interned within and
across generations, and per-candidate, per-reference and per-key costs are calibrated
against retained heap (a test keeps accounting at or above measured heap, at most
3× it). A 10,000-group window with 1–12 artist lineups is charged about 11 MiB. A failed, overflowing or over-budget generation is unavailable,
never truncated or silently widened;
generation failures log bounded reasons without filter/actor values. Expiry is
per view: one group's expiry never makes the generation (and every stream on the
replica) unavailable. Each cached view carries its earliest selected expiry and is
reselected in memory at that boundary, so the next matching group takes the slot.

## Private connections (Part 4)

Signed-in visitors can select **Saved**, **Interested**, **Followed artists** and
**Followed venues** under **Match any of these connections** on Happening Now or
account Preferences. No selection means all events. Any selected source may match;
public city/type/artist/venue dimensions still compose with AND. Both private and
public Interested choices qualify. A match only selects an existing public
recommendation: it never publishes a relationship or changes its visibility.
Guests see a sign-in explanation. No personal choices travel in feed URLs or
browser storage. Native Apply/Reset and conditional JSON writes share validation.

Schema 19 adds `accounts.activity_membership_revision`, without resetting any
notification choices. Actual save/interest/artist-follow/venue-follow additions
and removals bump it in the same transaction. Retries/no-ops and Interested
visibility-only changes do not bump it. Event/catalog writes retain existing lock
order and bounded deadlock retries; uncertain commits are never replayed internally.
Committed local changes invalidate local subscriptions; other replicas batch-poll
the active account union on the two-second cycle. New joins do not reset that clock.
A local invalidation closes that account's private streams with the content-free
`membership.changed` event, never stale membership. The browser clears the view and
reconnects quietly after 250–750 ms instead of the 5-second-and-doubling backoff it
uses after `unavailable`.

Membership uses server-only source bitmaps over stable candidate slots, shared by
an account's tabs. Only needed sources are allocated. Changed accounts rebuild;
new/corrected candidates update incrementally, while reorders/name changes reuse
membership. Reads use at most 128 owners × 256 candidates per SQL work unit, indexed
source joins and `UNION ALL`, checking account revisions inside repeatable-read
snapshots and rechecking them after assembly. Followed-artist/venue joins also
verify candidate metadata revisions, since they read event facets; Saved/Interested
match by event ID alone, so metadata corrections cannot fail their assembly.
They never load historical collections or run one query per account/event pair.
Personal requests queued for the projection gate join the current assembly, so a
cold start or reconnect burst of distinct accounts shares owner batches instead of
repeating every candidate batch once per account; queued owners beyond the member
cap are skipped and retry on their own turn rather than failing the current request.
Private cache keys include owner, source selection and membership generation;
public cohorts remain separate. Matching precedes the twenty-group limit and does
not change public contribution counts or the first-name-only item DTO.

Rebuilds run behind the shared projection gate, at most 64 work units per sweep
with a two-second deadline, reading up to two candidate batches at once within the
shared two-SQL-job limit. A rebuild larger than one sweep resumes: batches applied for
the same account revision stay done, and only an account change restarts it.
While a replica rebuilds an account that already had a complete view for a newer
revision, and the rebuild is progressing without SQL errors, its open streams keep
that last complete view for up to 10 seconds instead of closing — the same staleness
as the revision poll. New snapshots and streams, cold accounts, SQL failures and
longer rebuilds are unavailable as before; local changes use `membership.changed`. The 64 MiB accounting
includes membership bitmaps, candidate revision maps and an 8 MiB assembly reserve;
at most 384 owner scopes are retained, with 30-second unused expiry. Over-budget,
failed or inconsistent private views are unavailable, never broadened or reported
as zero matches. Public views continue after private matching failures. Existing
auth leases, pause/disable, counts, account switching and quiet replacement rules
apply; old recommendations entering through a membership change are not new toasts.

## Specific artists and venues (Part 3)

Happening Now and account Preferences share artist/venue name-search pickers,
selected labels, independent Clear actions and Reset filters. Select matches and
press **Apply** once. Artist selections match **any** listed artist; venue selections
match **any** listed venue. Both lists together require an artist **and** venue
match, also satisfying city and event type. All event artists/venues participate,
not just the primary venue; repeated entities never multiply rows or counts.

Each list accepts at most 16 exact, case-sensitive `ticketmaster:SOURCE_ID` values
before deduplication. Canonical sorted lists share encoded views across tabs and
reordered/duplicate requests. Empty lists are unrestricted. The existing snapshot
freshness guard updates facets atomically; metadata corrections replace matching
views quietly and older snapshots cannot restore stale matches. Entity facets and
bitmaps count toward the existing memory budget. No migration is added.

Lookup preserves the existing authenticated catalog contract. Signed-in visitors
search using `/api/v1/artists` and `/api/v1/venues`; corresponding `/{target_id}`
reads resolve selected labels using the shared catalog cache and provider budget.
All return `private, no-store`. Page label resolution has a three-second overall
deadline; failed names retain their exact IDs. Search failures preserve selections
and offer retry. No lookup occurs during activity projection/polling. Guests see
a sign-in explanation and can retain, clear and apply public entity ID filters;
sign-in never automatically uploads those browser settings. Names and search/feed
responses are not persisted in browser storage.

Without JavaScript, authenticated Search submits the filter draft without saving
it or changing its conditional revision; checked results are committed only by
Apply. Unchecking selections clears them. Both native editors retain selected
IDs across searches, and stale Apply still returns a conflict. Native guest GET
filters accept repeated `live_artist_ids` and `live_venue_ids`.

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

Happening Now is a tab in Community (after Recommendations and Conversations),
not a main navigation destination: during the pilot its ten-minute window is
usually empty. Main navigation marks Community current there; toasts and
Preferences also link to it. It shows up to 20 event groups with real Overview
links and browsing return context. Groups are compact cards with an initials
circle (the chosen first name's initial, the count for groups, or an empty
circle for Someone). On the server render, artwork, the ticket-stub date chip
and the date line come from one bounded, provider-free read of those events'
stored snapshots (`discovery.Service.StoredEvents`, at most 50 IDs). The live
DTO is unchanged, so cards added live use fallback art until the next render,
and no recommendation reason is shown, because next to "Someone" it could
identify the author. Scope summary, filters and Refresh share one compact bar.
Empty views lead with a large display line. An unfiltered empty view explains the window
and links to earlier Community recommendations. New rows wait behind **Show N new updates**;
withdrawals, name clearing and expiry apply immediately. No duplicate toasts appear
there. A filtered empty view offers Reset filters explicitly.

Elsewhere, new publications show at most three toasts, or one below the compact
navigation threshold. Each offers an event link, Happening Now and Dismiss. Toasts
expire after eight visible non-interacting seconds, suspended during hover/focus.
Repeated contributions update a visible event without resetting its lifetime. Bursts
are coalesced without a backlog; insufficient space, dialogs and focused controls
defer display. Dismissal never removes the event from Happening Now. Initial,
reconnect, filter-change and navigation snapshots are quiet. One debounced polite
status announces availability, without reading every count or stealing focus.

**Hide activity toasts** and **Show activity toasts** live on Happening Now and
preserve the existing `ticketopia-live-hidden` browser preference. Hiding closes
background activity delivery elsewhere; deliberate page browsing stays available.
Storage failure uses page memory. Signed-in **Pause all notifications** or live-disable
suppresses activity and streaming even on Happening Now, which explains the state
and links to preferences. Reset filters never changes pause/enable/hide choices.
Preferences are checked at entry/focus/BFCache and every 30 seconds while visible.
Neither pause nor hiding disables aggregate count refreshes or participation.

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
the earliest remaining contribution expiry. Views exclude a group from that instant
(conservative database time), so a partially expired group's count, time or name
is never shown; the next projection returns it with its remaining contributions.
Cleanup deletes at most 500 expired
rows per replica per minute, including when there are no subscribers.

Each replica shares activity polling across its subscribers (at most once per two
seconds), with latest-state single-slot buffers. Counts batch the union of requested
IDs into queries of at most 101 IDs. Recommendation and Interested totals are
authoritative replacements; private Interested choices contribute only to totals.
No save counts or named interest activity are published. Count updates preserve
viewer selections, privacy controls, drafts and collection order. Confirmed writes
invalidate older callbacks and trigger fresh reads.

## HTTP and operations

- `GET /happening-now`: native page, owner preferences or bounded public form-query
  filters; shares the snapshot start limiter and `private, no-store`. Disabled
  deployments retain an explanatory page and omit navigation/enhancement.
- `POST /me/live-activity-preferences`: existing cookie-CSRF and revision-checked
  native replacement; an exact `/happening-now` return target returns to the feed.
- `GET /api/v1/community/activity`: bounded filtered snapshot, no pagination.
- `GET /api/v1/community/activity/stream`: SSE `activity.snapshot`, optional
  `counts.snapshot`, `heartbeat` every 15 seconds, and content-free `unavailable`.
- `GET /api/v1/events/activity-counts`: repeated `event_id`, at least one.

Both count inputs accept at most 101 distinct provider IDs and 32768 query bytes;
unknown parameters/invalid IDs are 400. Unknown valid IDs have zero totals.
Public activity reads also accept one `filter` JSON object, at most 4 KiB decoded,
with optional `location`, `category_ids`, `artist_ids` and `venue_ids` (16 IDs per list).
Omission preserves the default view. Private reads use confirmed stored filters,
not URL lists. Snapshot and stream starts share the IP limit; 429 includes
`Retry-After: 60`. Counts remain independent of feed filters.
Responses use `private, no-store`; SSE additionally uses `no-transform` and
`X-Accel-Buffering: no`. No CORS is added. SSE has `retry: 5000`, no `id` or replay
cursor, and ignores Last-Event-ID. Pre-header failures use problem JSON (400,
429 with Retry-After, or 503); post-header failures send `unavailable` and close.
See the [OpenAPI contract](../internal/api/openapi.yaml) for schemas/examples.

`LIVE_ACTIVITY_ENABLED` defaults on only with accounts and MariaDB; explicit
`false` disables it. Use one resolved value across replicas. `LIVE_ACTIVITY_MAX_STREAMS`
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

**Part 5 in progress — 2026-10-20:** bounded operational telemetry, shared fresh
joining-count reuse and a real three-container/nginx load fixture are implemented.
Load testing also fixes inactive-account membership re-entry and bounds retired
private-view cohorts when memberships change.
Profiled improvements make warm expiry checks constant-time and reuse immutable
facets by metadata revision while always refreshing attribution.
See the [performance and rollout record](live-activity-rollout.md) for commands,
measurements, failed/outstanding gates and the coordinated kill-switch procedure.
The production-readiness milestone remains unchecked until all exit gates pass.
The optimized rich 1,000-group / 256-account / 768-stream scenario passes, including
SQL recovery and drain. The full 10,000-group scenario now also passes its 10%
ordinary-traffic gate (browse 1.040×, write 1.053×), measured from an in-network
probe; the earlier 37.4% failure came from the host-side probe path. A real-MariaDB
resilience/privacy stress test found and fixed expiry-driven stream interruption,
stale worker failures after recovery and private rebuild livelock under metadata
corrections. Race, vet and Docker checks pass; retain the explicit production kill
switch while the remaining workload/manual rollout gates are open.
On 2026-10-23 the 40-minute soak (P1.3) passed on a Linux (WSL2) host: write 1.016×
(95% CI 1.003–1.026), browse 1.030×. It was accepted as representative on the
stated assumption that production is similar or better. The Mac/Colima VM still
fails (1.108×). HTTP count reads stay uncached so a read after a committed write is
always fresh.

**Part 4 — 2026-10-19:** real MariaDB tests cover concurrent/idempotent saves,
all four source mutations, visibility-only no-ops, actual writer failure/rollback,
separate-pool revision delivery, owner isolation and mismatched metadata rejection.
Reference tests cover OR-source/AND-public composition, complete-window selection,
thirty duplicate readers sharing work, incremental metadata refresh, reorder reuse
and private failure without breaking public views.
`scripts/live-activity-connections-browser.cjs` passes 25 production-route checks
using two services with separate SQL pools: source additions/removals reach both
recipient tabs, owner independence, quiet updates, actual SSE/DOM/storage privacy,
guest boundaries, native Apply/Reset, 1440/390/320px, 200% text, forced-colors keyboard
focus and whole-page axe. Existing filter/count/draft and presentation journeys
pass 30 and 65 checks. The scoped style detector reports no findings; captures and
the check record are local under `.impeccable/review/live-connections/`.
The full real-MariaDB race suite, final affected-package regression, vet, OpenAPI
lint (two existing health-probe warnings) and generated asset/binary build pass.
Docker `make check` also passes, validating the filtered publishing build context.

The candidate-scoped saved-membership baseline (one owner, 256 candidates, 20
samples) averages 4.53 ms for one match and 5.29 ms for 256 matches on the same
Apple M1/local MariaDB environment described below, without race instrumentation.
These small sparse/dense baselines do not establish production capacity. Reproduce
with `MARIADB_TEST_ADDR=127.0.0.1:3307 go test ./internal/persistence -run
TestMariaDBMembershipBenchmark -count=1 -v`. Use the browser fixture command below
with `LIVE_ACTIVITY_BROWSER_SCRIPT=../../scripts/live-activity-connections-browser.cjs`.
Three-replica load/proxy, physical devices, manual screen readers and production
migration/default-on rollout remain Part 5.

**Part 3 — 2026-10-18:** isolated MariaDB verifies artist/venue composition beyond
the global top twenty, full venue lists, duplicate suppression, case-sensitive IDs,
durable canonical/no-op preferences and metadata corrections across connections
with stale snapshot protection. Complete-window reference comparisons also verify
mixed dimensions and shared encoded views for reordered/duplicate lists.
`scripts/live-activity-entities-browser.cjs` passes 29 production-route checks:
artist → venue → second artist → independent clear, lookup failure/retry, native
search/Apply/Reset, no-op Reset of drafts, account/guest persistence, authenticated
lookup boundaries, DTO/storage privacy and JSON fallback. Whole-page axe,
1440/390/320px, 200% text and keyboard/forced-colors/reduced-motion checks pass.
The existing filter/count/draft and presentation journeys pass 30 and 65 checks.
The real-MariaDB full race suite, final affected-package race checks, vet, OpenAPI
lint (only the two existing probe warnings), generated build and Docker `make check` pass. Screenshots
and the entity check record are local under `.impeccable/review/live-entities/`.
Run the entity script with the same browser fixture command below, substituting
its filename for `live-activity-browser.cjs`. Three-replica load/proxy, physical
devices and manual screen-reader verification remain Part 5 rollout gates.

Presentation checkpoint (2026-10-18): 65 production-route/browser checks across
two replica-local hubs and isolated MariaDB cover bounded toasts, dismissal/focus,
hover/lifetime/grouping, quiet reconnects, page buffering, redaction/expiry,
native guest/owner forms, preserved hide/pause/disable, account switching/revocation,
fallback, navigation and Overview/Back. The updated city/type journey passes 30
checks, including matching toast filters, complete-window results, native Reset,
unchanged mounted drafts/private participation, and independent authoritative counts.
Whole-page axe, 1440/1050/390/320px navigation, 200% text and forced-colors/reduced-motion
checks pass. Screenshots are in `.impeccable/review/live-activity/` and `live-filters/`.
The full real-MariaDB race suite, vet, generated asset/binary build and Docker
`make check` pass.
The styling detector reports only pre-existing provider-font and token advisories.
Local screenshot inspection completed; an independent finish-review attempt was
blocked by the review service's usage limit and is not recorded as a sign-off.
Physical devices, manual screen-reader, proxy and three-replica load remain Part 5;
this checkpoint adds no migration and does not change production configuration.

Part 2 local checks (2026-10-16) exercise a Berlin music recommendation older than
25 globally newer London events, reset, normalized city/country, literal wildcards,
accent/case collation, case-sensitive categories, metadata corrections, persisted
canonical no-ops, complete-window reference comparisons, stable slots, shared
reads/encoded views, candidate/memory/cohort caps and SQL overflow. The browser
fixture uses production routes and isolated MariaDB; its 25-check filter journey covers
guest/account scope, native Reset, fallback, Overview/Back, composer identity/drafts,
private selected participation, keyboard focus and automated accessibility. The
existing 64-check live-activity journey also passes, including pause/disable,
credential changes, hide/reconnect, count updates and JavaScript-disabled forms.
The full real-MariaDB race suite and final affected-package race checks, host vet,
generated asset/binary build, OpenAPI lint and final Docker `make check` pass.
OpenAPI retains the two existing health-probe warnings; the scoped styling
detector reports no findings. Part 2 is a locally verified checkpoint.

Desktop and 320px filter captures are in `.impeccable/review/live-filters/` locally.
Filter controls wrap at 200% text and retain forced-colors focus/reduced-motion
behavior. An existing Community **More recommendations** pagination button
overflows at 320px with 200% root text; the enlarged-text assertion is scoped to
the changed live region. Physical-device/manual screen-reader and deployment
proxy/load verification remain Part 5.

Populated-window baseline: Apple M1, darwin/arm64, local MariaDB 12.3.3 Docker
service with no per-container memory/CPU limit, no proxy, one app reader. Fixtures
use one contribution per group, two cities/categories and short synthetic titles;
these are development measurements, not the three-replica load gate. In-memory
selection averages 3.7 / 7.0 / 7.2 microseconds at 100 / 1,000 / 10,000 groups;
index construction averages 0.13 / 1.28 / 12.1 ms, allocating approximately
59 kB / 478 kB / 4.41 MB per generation. Benchmarks report Go `ns/op`, not p95.
SQL projection p50/p95 over 20 samples was 6.2/6.7 ms at 100 groups,
14.3/15.1 ms at 1,000, and 117.4/131.3 ms at 10,000. The 10,001-group case
returned unavailable. Other local checks ran during fixture preparation; these
numbers do not establish isolated production capacity or the larger-lineup gate.

Reproduce SQL projection/overflow and in-memory baselines with:

```sh
MARIADB_TEST_ADDR=127.0.0.1:3307 LIVE_FILTER_BENCHMARK=true \
  go test ./internal/persistence -run TestMariaDBLiveFilterBaseline -count=1 -v
go test ./internal/activity -run '^$' -bench BenchmarkCandidate -benchmem
```

For the filter browser demo, use the command below with
`LIVE_ACTIVITY_BROWSER_SCRIPT=../../scripts/live-activity-filters-browser.cjs`.

Part 1 local checkpoint (2026-10-16): real MariaDB upgrade/rerun tests cover both
previous pause values, durable subsequent pause/disable, new-account defaults and
unchanged subscription/envelope/reminder/receipt records. Production-route tests
cover owner isolation, ETag conflicts/no-ops, unsupported filters, explicit pauses,
cross-pool settings/revocation, local revocation and known expiry.
The browser fixture upgrades real v16 paused/unpaused identities before sign-in.
Its 64 checks include guest/upgrade defaults, native and JavaScript-disabled saves,
stale-tab conflicts, local hide persistence, pause/disable sync, authenticated JSON
fallback, account switching, revoked sessions and the deployment kill switch.
Axe reports no violations in the live region or preferences page; desktop and
320px settings captures were inspected. Proxy/load, physical-device and manual
screen-reader verification remain Part 5 rollout work.
The full real-MariaDB `go test -race ./...`, `go vet ./...`, generated `npm run build`,
OpenAPI lint and Docker `make check` pass. Lint retains only the two existing
health-probe warnings. The styling detector reports no findings in the changed UI.

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

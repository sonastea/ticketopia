# Aligned backend and frontend delivery checklist

Status: All-category discovery, responsive navigation, basic event previews/
detail pages, the MariaDB connection/migration/identity foundation, and opt-in
Google accounts/profiles/private preferences, durable private saves, Interested
with privacy controls/counts/public-opt-in activity, and public recommendations are
implemented, along with event questions/replies/Helpful and conversation browsing.
Basic private reporting/moderation and reliable scheduled event history are implemented;
broader persistence remains planned.
The [root goals](../../README.md#goals) track user outcomes; this checklist maps
those outcomes to shared backend capabilities and their web UI. Check a task
only when its behavior has been implemented and verified. API availability alone
does not complete a UI outcome, and a cache does not complete durable storage.

## MVP release boundary

The updated release combines location-based discovery across **all Ticketmaster
event categories** with event-centered community participation. Category-aware
discovery, account/profile/preferences, private saves, Interested, and public
recommendations, discussions/Helpful, and basic reporting/moderation are implemented.
The [design guidelines](../design-guidelines.md) own screen/navigation behavior.
Section numbers below group capabilities; they do not force reminders/ranking
to ship before community work.

**MVP:** nearby/upcoming discovery, event details, authentication/profiles and
privacy controls, private saves, Interested, public recommendations with optional
reasons, event root posts/threaded replies/Helpful reactions, simple city/category
community browsing, and usable desktop/tablet/mobile layouts. Retain basic private
reporting and owner moderation before the first public community pilot.

**Retained subsequent milestones:** artist/venue follows, personalized radar,
on-sale/change reminders and digest, external notification delivery, Going/Went
and structured post-show prompts. Budgeted scheduled observation history is now
implemented, independently opt-in. Private conversation follows and in-app reply
notifications with frequency controls are [implemented](../followed-discussions.md).
Minimal durable events/accounts/community records and verified backup/restore
are MVP foundations; the full ingestion scheduler is not a dependency of posting.

**Future explorations:** direct messaging, real-time chat, advanced recommendation
algorithms, personalized social activity feeds, managed groups/large-scale
community tooling, map/list discovery, games, and venue analysis. Category/location
scopes and explicit endorsements provide useful recommendations without these.

## 1. Find an event worth considering

Aligns with [personal radar, step 1](personal-radar.md#small-goals-one-at-a-time)
and the reusable API goal. Current behavior: [discovery guide](../discovery.md).

- [x] Backend: Fetch music events and embedded artist/venue/classification metadata;
  retain source IDs, URLs, event/sale dates, statuses, images, and optional prices.
- [x] Backend: Fetch the music genre/subgenre catalog for discovery filters.
- [x] Backend: Reuse filter-aware cached reads across clients, coalesce concurrent
  misses, populate detail entries from searches, and retain stale data on failure.
- [x] Backend: Bound pagination, timeouts, request rate, and per-process daily
  budget; respect provider cooldowns and validate inputs before fetching.
- [x] Backend: Publish event list/detail and genre JSON reads with freshness,
  structured errors, and an OpenAPI contract.
- [x] Frontend: Search by city/country, dates, keyword, and genre; paginate while
  preserving filters and distinct event occurrences.
- [x] Backend: Resolve approximate IP city hints with cached, bounded lookups and
  explicit proxy trust; prefer the visitor's chosen or remembered city.
- [x] Frontend: Label the active city, allow changing it, remember a manual choice,
  and ask for a city when none is available instead of fetching worldwide events.
- [x] Frontend: Show local event dates, separate public-sale dates, venues,
  advertised/unknown prices, event status, and the correct ticket link.
- [x] Frontend: Handle empty, loading, error, and stale results; support keyboard
  navigation, small screens, and functional search/pagination without JavaScript.
- [ ] Product: Verify relevance with three people in one well-covered pilot city.
- [x] Backend: Expand beyond the Music segment; expose category metadata/filtering
  with an all-categories option and compatible genre behavior, preserving existing IDs.
- [x] Frontend: Offer category selection and appropriate non-music metadata;
  preserve city/date defaults, source links, and explicit unknown values.
- [x] Verification: Exercise multiple available categories, category-specific and
  empty results, existing music filters, and non-music events without artists.

### All-category discovery: testable slices

1. [x] Shared discovery/API: expose the category/genre catalog, remove the implicit
   Music restriction, preserve genre-only music links, and bind categories to
   cache keys/cursors. Verify with provider fixtures and JSON route tests.
2. [x] Web search: add category-aware genre controls and neutral event metadata.
   Verify category switching, ordinary GET forms, empty results, and detail links.
3. [x] Regression gate: exercise category-specific pagination and return context,
   catalog outages, music compatibility, and desktop/mobile browser journeys.
   Run race tests, vet, and OpenAPI lint before checking the root milestone.

Focused commands and manual checks: [discovery verification](../discovery.md#verification-by-slice).

## 2. Keep reliable event and application history

Aligns with [MariaDB persistence](../design/database.md), reminder change
detection, and later [venue insights](../ideas/venue-insights-and-discovery.md).

- [x] MVP foundation: Connect to MariaDB through `database/sql` and
  `github.com/go-sql-driver/mysql`; choose migration tooling and run versioned
  migrations as a serialized deployment step with separate migration credentials.
- [x] Backend foundation: Provide a minimal durable event/provider repository with
  case-sensitive identity, atomic concurrent upserts, category references, and
  cache-independent metadata; verify against real MariaDB. See [setup/recovery](../persistence.md).
- [x] Backend: Persist events, artists, venues, provider-ID mappings, and dated
  observations with first/last-seen and change times.
- [x] Backend: Schedule city/date collection within the request budget; record
  coverage, failures, and meaningful changes without erasing the last good data.
- [x] Backend: Add durable budget/refresh coordination for MariaDB-backed workers
	and hosts; retain quota and collection coverage receipts. See [event history](../event-history.md).
- [x] Backend/frontend: Connect the durable catalog to existing Discover/API,
  with SQL filtering/sorting/pagination, coverage-aware reuse/fallback and durable
  on-demand collection outside configured cities; verify partial/outage/quota/restart paths.
- [x] Backend: Budget targeted detail reads for omitted known events, without
  inferring cancellation from absence or 404.
- [x] Operations: Log collection coverage/stale scopes/failures, detail backlogs,
  provider cooldowns and active-window quota consumption in all MariaDB deployments.
- [ ] Backend: Add operational dashboards/metrics for cache hits, external calls,
  quota usage, and collection coverage.
- [ ] Backend: Back up and verify restoration of application state and jobs.
- [x] Frontend: Add an event detail page with useful metadata and clear change,
  freshness, cancellation, and postponement information from durable observations.
- [x] Frontend foundation: Read current event metadata, venue, status, prices,
  source freshness, and ticket links on a dedicated event page using cached reads.
- [x] MVP save foundation: Atomically upsert durable event/provider identity and
  last-known snapshots before bookmarks; retain saved events without provider/cache
  data. See [private saves](../saved-events.md).
- [x] MVP community foundation: Preserve conversations without provider/cache data.
  Basic event details must ship without waiting for historical change detection.
- [x] MVP recommendation foundation: Preserve public endorsements and their event
  snapshots across restarts/provider loss without merging save or interest state.
- [ ] Deployment: Provision shared MariaDB with `mariadb-operator`; configure
  persistent database volumes, primary routing, Secrets/TLS, connection limits,
  backups, and the selected database availability topology independently of app replicas.
- [ ] Verification: Exercise concurrent writes and read-after-write behavior across
  three app replicas, pod restarts/rolling updates, migration serialization, and
  backup restoration; rehearse failover if database HA is enabled.

## 3. Save, follow, and find personal matches

Aligns with [personal radar, steps 2–3 and 5](personal-radar.md#small-goals-one-at-a-time)
and [cross-client continuity](../design/radar-api.md#shared-backend).

- [x] Backend: Establish accounts, verified web/API credentials, ownership, and
  durable location, interest, time-zone, and notification preferences.
- [x] MVP Backend: Provide own/public profiles, private-by-default future interest
  visibility, and private category preferences; see [accounts](../accounts.md).
- [x] MVP Backend: Provide owner-only bookmark reads/writes.
- [x] MVP Frontend: Provide sign-in with local task return, profile/public-view
  preview, privacy-default controls, and private preference forms.
- [x] MVP Frontend: Provide a private Saved collection.
- [x] MVP Backend: Add idempotent save/unsave APIs backed by durable bookmarks.
- [x] Backend: Add private artist/venue follow APIs and search backed by cached
  provider or stored metadata; see [follows](../follows.md).
- [x] Frontend: Provide artist/venue search, follow controls and a private collection.
- [x] Backend: Rank [radar matches](../radar.md) with understandable reasons.
- [x] Frontend: Provide a radar with match explanations alongside follows,
  profile/preferences and the saved list.
- [x] Verification: Save an event, leave, return, and find it again; confirm the
  same preferences and choices are available to another authenticated client.

## 4. Receive useful reminders

Aligns with the [reminder promises](personal-radar.md#reminder-promises).

- [ ] Backend: Persist reminder settings and idempotent notification jobs; handle
  time zones, unknown on-sale times, changed dates, cancellation, and delivery retries.
- [ ] Backend: Deliver public on-sale email, meaningful event changes, and a weekly
  discovery digest; enforce pause/preferences before each delivery.
- [ ] Frontend: Offer reminder lead times, pending/sent/failed state, pause/stop
  controls, and email/digest preferences with clear expectations.
- [ ] Verification: Deliver a timely reminder while the app is closed, prevent
  duplicate/obsolete messages, and prove preference changes affect future jobs.

## 5. Participate in positive event communities

Aligns with [all core community goals](event-communities.md#small-goals-one-at-a-time).
Build backend/API and UI together for each small outcome:

- [x] Backend: Persist Interested independently from bookmarks, recommendations,
  and later attendance states; default identity visibility to private.
- [x] Frontend: Let people toggle interest, control its visibility, and distinguish
  their own selected state from aggregate counts. See [event interest](../event-interest.md)
  for own collections, public-opt-in participants/profile activity, and verification.
- [x] Backend: Persist one public recommendation per user/event with optional
  reason; support editing/withdrawal and paginated city/category discovery.
- [x] Frontend: Offer Recommend as a secondary action, disclose public attribution,
  edit/withdraw, and browse public event/profile and city/category recommendations.
  See [recommendations](../event-recommendations.md) for API/UI and verification.
- [x] Verification: Exercise native/enhanced writes, failures/drafts, public privacy,
  paginated return context, offline persistence, compact/200% text, and accessibility.
- [x] Frontend: Browse recent event conversations alongside recommendation discovery.
- [x] Backend: Persist comments, replies, edits/removals, and positive Helpful
  reactions, enforcing ownership and preserving removed reply context.
- [x] Frontend: Provide readable threads, composing/replying/editing/removal, and
  adding/removing Helpful reactions.
- [ ] Later Backend: Persist followed discussions and preference-aware activity jobs.
- [ ] Later Frontend: Follow/unfollow conversations and surface useful activity updates.
- [x] Backend: Support private reports, authorized moderator review, decision
  records, reporter acknowledgment, and appropriate author outcomes.
- [x] Frontend: Provide private reporting/status and a moderator review workflow.
  See [moderation](../moderation.md) for durable roles, bootstrap, privacy and verification.
- [ ] Later Frontend: Offer post-show Went choices and reflection prompts using the
  same participation and conversation services.
- [ ] Later: Add distinct Going/Went records and visibility controls, without
  redefining MVP Interested or implying verified attendance.
- [ ] Verification: Observe a useful question, reply, and Helpful acknowledgment;
  exercise reporting/moderation before the first public community pilot.

## 6. Responsive discovery-to-discussion experience

Discovery and full event/conversation context now share one responsive selection
flow, with dedicated expansion routes and verified fixture participation journeys.
Draft/history continuity is browser/tab-local, not cross-device synchronization.

- [x] Frontend: Responsive global navigation, image-led discovery, and selected
  event metadata previews with dedicated event pages on compact screens.
- [x] Frontend: Encode selection/section in URLs; reconstruct on reload, preserve
  context on resize, cancel stale loads, and isolate preview errors with retry.
- [x] Verification: Check desktop/tablet and 320/390px discovery/event views,
  browser Back, loaded-result restoration, direct section links, no-JavaScript
  navigation, keyboard focus, long titles, 200% text, and automated accessibility.

- [x] Frontend: Wide three-column discovery with global nav, results, and selected
  event discussion; selection does not require replacing the discovery page.
- [x] Frontend: Expand an event to a dedicated detail page and a conversation to
  a shareable full discussion/thread view with persistent event context.
- [x] Frontend: Adapt intermediate widths to two columns or a dedicated view
  based on readable content widths, with collapsible global navigation.
- [x] Frontend: Single-column mobile discovery, persistent bottom navigation,
  compact search/filter access, event sections, and keyboard-safe thread composing.
- [x] Backend/UI: Paginate posts/replies independently; batch card aggregates,
  fetch only selected context, isolate failures, and reject stale selection loads.
- [x] Frontend: Keep filters, event/thread identity, action state, return scroll,
  focus, and drafts coherent across navigation, resizing, and authentication.
- [ ] Verification: Complete discovery, Save, Interested, Recommend, post/reply,
  Helpful, and return journeys on desktop, tablet, and phone, including 320px,
  zoom, keyboard/screen reader, Back/direct links, and loading/empty/error states.
- [x] Verification: Confirm as owner, another user, and guest that saves/private
  interest identities stay private while counts/public endorsements remain distinct.

Chromium fixture journeys cover desktop/tablet/390px/320px/200%-text participation,
keyboard focus, touch, reduced keyboard viewport, automated accessibility and
native fallback. The broader verification item stays open for manual screen-reader
and physical-device/cross-browser checks; no public-pilot certification is claimed.

## Completion rules across milestones

- Extend the OpenAPI contract alongside each implemented endpoint.
- Keep HTML and other clients on shared services and the same identity/visibility rules.
- Verify persistence and restart behavior for durable features, plus loading,
  empty, error, accessibility, and responsive behavior for their UI.
- Update the matching feature guide/history and root checklist after delivery.
- Keep [games](../ideas/discovery-games.md) and advanced venue/price analysis as
  optional explorations after the core discovery/reminder/community outcomes.

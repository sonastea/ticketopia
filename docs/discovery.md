# Ticketmaster event discovery

Status: Implemented for the web UI and public read-only JSON API. Delivery of
later personal/community features is tracked in the [aligned checklist](goals/delivery.md).

The [responsive UX guide](design-guidelines.md) describes the discovery shell,
selected-event previews, dedicated event pages, and all-category discovery.
[Interested](event-interest.md) and [public recommendations](event-recommendations.md)
provide independent participation; [event discussions](event-discussions.md)
add public questions/replies/Helpful. [DESIGN.md](../DESIGN.md) records the
implemented visual system.

## Fetch once, reuse across clients

`internal/discovery` owns Ticketmaster requests, normalization, and caching.
HTML and JSON handlers call the same service. The browser never receives the
Ticketmaster key and the server does not call its own HTTP API.

The listed public JSON endpoints support [conditional HTTP reads](cache.md#public-http-caching).
Discovery supplies taxonomy freshness deadlines from its cache records, including
retained-fallback status. Event validators are evaluated only after the normal
service path, so a 304 saves response bytes while preserving refresh/coverage work.

In MariaDB mode this service searches the durable public catalog first, reuses
fresh collection coverage, and retains last-good results during outage/quota
exhaustion. On-demand collection persists outside configured scheduler cities.
See [stored Discover and history](event-history.md#stored-discover-and-coverage-aware-reads)
for coverage, SQL indexes/keyword semantics, targeted detail refreshes and operator
logs. Database-free mode retains provider/cache behavior described below.

One search retrieves events with their artist, venue, classification, image,
advertised price, status, and sale metadata. Those results also populate event
detail cache entries, avoiding an external request per visible event. A separate
request fetches the category/genre/subgenre catalog for filter options. Categories
are Ticketmaster segments (including Music, Sports, Arts & Theatre, Film, and
Miscellaneous), fetched from the paginated classifications collection. Types and
subtypes are excluded. The bounded full-catalog read is cached for 24 hours with
seven-day retained fallback; incomplete catalogs do not replace successful data.
The legacy music-only genre endpoint remains available.
Artist and venue search/detail endpoints are future work; their references are
already included in events.

Set `TICKETMASTER_KEY` in the environment or optional `.env`. A missing key gives
an explicit unavailable response on a cache miss. Configure storage and retention
using the [cache guide](cache.md).

## Web UI

Open `/` to search by city, two-letter country code, event/performer/team keyword,
venue-local date range, category, and compatible genre. Searches default to
**All categories**. The default range is today (UTC) through 90 days
later. The web page uses a chosen city, a remembered city, or an approximate
IP-based city, in that order. When none is available it asks for a city before
fetching events. Use both city and country to narrow ambiguous place names.
See [location-aware discovery](location.md) for lookup caching, editable defaults,
and reverse-proxy configuration. Filtering is city-based, rather than a GPS radius.

Each occurrence has its own image-led row, even when titles match. Show dates and public
sale dates are labeled separately; ticket links use the event URL. Missing dates,
venues, classifications, and prices have explicit fallback labels. Category and
genre labels prefer the primary classification; missing classifications never
imply Music. The neutral Featuring section can contain performers or teams and
is omitted when no attractions are supplied. Prices do not establish fee
inclusion or ticket inventory.

Discover rows expose supplied performers/teams, separate ticket-sale information,
and a short description beneath the essential facts, using the full row width.
When public activity exists, they show visible question/tip and active recommendation
count links, with full excerpts and attribution reserved for event sections.
Events without activity add no empty community block or invented count.
Question counts select Discussion and recommendation counts select Community;
ordinary links open dedicated pages without JavaScript. Title and artwork share
the Overview destination. **Tickets on Ticketmaster** is directly visible beside
private Save; missing URLs have an explanation. Interested and on-sale reminders
remain in the event view, with interest removal retained in Interests.

In accounts-enabled MariaDB mode, `internal/eventactivity` reads only the current
results page (up to 100 event IDs) through two batched SQL queries, not one query
or provider call per row. Hidden/removed root text, withdrawn endorsements, private
saves, and private-interest identities never enter these previews. Read failures
leave event results usable with a local unavailable message, not guessed zeroes.
Previews refresh on a new page read; they are not a live activity feed. This HTML
enrichment does not change the JSON event API, discovery sorting, or cache keys.

Load-more keeps all filters and starts at page zero. MariaDB paginates all matching
stored results; database-free mode stops at Ticketmaster's paging cap.
With JavaScript it appends rows; without JavaScript
the link navigates to the next page. Search is a regular GET form. The page
includes loading, empty, error, and stale-result messages. Event results are read
before optional metadata so a catalog failure cannot replace successful results.
Unavailable catalogs retain selected filter IDs and offer all-category search;
retained catalogs show their stale state.

### Category and genre controls

Choose a category to enable its genre choices. Changing category clears the
previous genre; choosing All categories disables genre filtering. JavaScript
updates options locally from the rendered catalog, without another HTTP call.
Without JavaScript, apply the category first, then choose from its updated genres.
The HTML-only `genre_category_id` form field identifies the previous options so
the server can clear a carried-over genre. It is excluded from shared filters,
pagination, and event return links, and is rejected by the JSON API.

Existing `genre_id` links without a category still select Music. New category
choices and genres survive pagination, event selection, reload, and return links.
A new form submission starts at the first page.

### Responsive navigation and event details

- Wide screens (72rem+) have labeled left navigation, a primary event list, and
  a 21rem contextual preview. Intermediate screens (62–72rem) use a compact
  labeled rail and a 20rem preview; below 62rem the app uses bottom navigation
  and a single primary event/thread view on selection. Dedicated pages remain
  available for expansion and ordinary no-JavaScript links. The intermediate threshold leaves room for
  the navigation rail and readable results. Discovery caps at 120rem; other pages
  cap at 100rem. At 64rem of actual workspace, selected results/context divide 2:3,
  and discussion/community can sit side by side. The selected desktop preview fills
  the usable viewport height, scrolling internally for longer activity. At enlarged
  text sizes, the default preview takes at most 45% of the content width.
- Result lists respond to their own container (Discover results and the Saved,
  Interests and Radar pages), not the viewport: below 26rem artwork stacks above
  facts, from 26rem rows place square artwork (7.5–11rem) beside them, and from
  48rem an intrinsic grid shows two or three cards. A ticket-stub date chip sits
  on every row's artwork. The selected row keeps its geometry, with a 3px leading
  brand edge on a raised surface.
- One command bar joins location and search: **Events in {city}** (**Choose your
  city** before one is chosen) is a link that opens the native Filters disclosure
  and focuses City (with an accessible "change city" suffix), and the keyword
  segment is labeled for event, performer, team, or venue names, matching its
  semantics. The bar is one row when the results container reaches 34rem and
  stacks below that. The same GET form exposes country, dates, category,
  genre, and date-ascending/date-descending/name-ascending sorting. Inputs and
  disclosure remain usable without JavaScript.
- Date chips (**Tonight**, **This weekend**, **Next 7 days**) scroll sideways
  under the bar once a city is chosen. They are plain links. Each keeps every
  current filter, drops any cursor, and sets only `start_date`/`end_date` from
  `discovery.DatePresets`: the day itself; Friday–Sunday, starting today from
  Friday; and today plus six days. "Today" is the calendar day in the browsed
  city's zone, read from the results' event or venue time zone (UTC if none is
  known), so late evening in Los Angeles is not tomorrow's UTC date. The first
  matching chip carries `aria-current="true"`. Unit tests parse every preset URL
  through `ParseQuery`, and render tests parse the chip hrefs from Discover HTML.
- Event headers take their tint from artwork. When an event is cached or stored
  (`fetchPage`, detail reads and targeted refreshes), the provider adapter fetches
  each event's smallest supplied image once. Only HTTPS `*.ticketm.net` images are
  fetched, with no redirects, under a shared 1.5s batch deadline, four in parallel,
  512 KiB, 4 MP and a 4,096-entry URL memo. The adapter grid-samples about 4,096
  pixels, takes the most chroma-weighted hue family, and stores it in the event
  snapshot as `tint` (`#rrggbb` at OKLCH lightness .66, chroma ≤ .12). Grey
  artwork, timeouts and other hosts leave it empty and the page uses the brand
  hue. Rendering never fetches images; only strict lowercase `#rrggbb` reaches
  the inline `--event-tint`.
- The results heading shares its line with a short sort label. A single results
  line combines update time, any stale-fallback warning and incomplete coverage
  (`data-coverage`). It isn't a live region, so it isn't re-announced on each load.
  Save privacy is explained in the first enhanced Save confirmation per browser
  rather than above every list; event pages keep their own Save note.
- Event titles and images are real Overview links; the title is the row's only
  tab stop (the image link is `tabindex="-1"`/`aria-hidden`). Activity counts
  select the named community section directly. Rows omit the provider description
  (mostly policy boilerplate; Overview shows it in full), and Featuring omits
  performers already named in the title.
  Enhancement loads only selected context and keeps results alongside it from
  62rem, or hides results behind the primary view below that threshold.
  `selected_event`, `section`, `selected_thread`, `reply_to`, and independent
  `discussion_cursor` reconstruct context on direct entry/reload. These HTML-only
  parameters never enter discovery cache keys or the JSON event API.
- Event pages use `section=overview|discussion|community` and a validated local
  `return_to` URL. Overview shows supplied descriptions, venue information, prices,
  status, separate sale dates, freshness, and the Ticketmaster link. Community shows
  public recommendations and opted-in interest identities. Discussion loads recent
  questions and a public composer. Threads/replies load in context, with canonical
  dedicated routes for expansion/sharing. Overview, Questions & tips, and
  Recommendations use one shared set of real section links in panels and pages.
  Event pages open with a wide artwork band (2:1, capped at 22rem; a short strip
  when artwork is missing) and the title, date, venue and facts on a tinted
  surface overlapping it; panels keep Tickets and Save before artwork. Below 62rem,
  on viewports at least 32rem tall, Tickets and Save form a sticky bar above the
  bottom navigation. It stays in normal flow, ordered last so it settles at the
  end of the event, and JavaScript only measures the navigation's actual height.
  Description and practical notices are visible without disclosure. Interested/Recommend/reminders are grouped
  separately below content. **Event source & history** discloses only secondary
  provenance; stale warnings remain adjacent to essential facts.
  HTML-only `recommend=true` opens
  the composer in Community; it is not a discovery/API filter.
- Same-origin navigations opt into cross-document view transitions only when
  reduced motion isn't requested. Rows, the preview and event pages give artwork
  and titles per-event `view-transition-name`s (escaped provider IDs), so they
  move into place over 250ms; while the preview is open it owns the selected
  event's names. Browsers without support navigate normally. In-page preview
  loads aren't animated.
- Back restores previous selection; loaded result pages, focus, and scroll are
  retained in browser history across the event's in-place section links. A
  one-use, same-tab session-storage handoff carries the originating history entry;
  if storage is unavailable, a normal return
   link still preserves validated search filters and event/thread selection.
   Resizing retains the selected event/thread and drafts without adding history.
- Preview loads cancel obsolete requests, have bounded timeouts, and offer Retry
  and Open event on failure. A failed preview leaves the discovery list usable.
  Choosing an event focuses its preview heading on every layout; closing restores
  the result title. History restores keep focus where the browser left it.
  Successful compact context omits standalone expansion; desktop offers one
  **Open event page** link. Saved, Interests, and Radar return across event sections
  to the original collection cursor, position, and focused event.
- Opt-in [accounts](accounts.md) provide profiles/private preferences and
  [private saves](saved-events.md). `/saved` is an owner-only collection with
  durable last-known details; Save/Remove work from rows, previews and event pages.
  Disabled accounts explain configuration requirements. Interested has independent
  privacy/counts; `/community` browses public recommendations and links the separate
  `/community/discussions` collection. Recommend is a secondary link to
  the Community composer, with explicit public attribution and independent state.
- Detail reads can fall back to durable last-known snapshots when current provider
   data is unavailable. Public metadata never includes private bookmark state.

### In-place event sections

Discover and standalone event pages enhance the same real section links. Only
the section body changes; identity, Tickets/Save, navigation, participation, and
source/history remain mounted. Pending clicks keep the previous body labeled,
failures keep its URL with local Retry/native recovery, and success commits one
history entry with current-link and return-target updates. Back/Forward re-fetch
the requested section without pushing, restoring reading position and scoped drafts.
Failed history reads show a local requested-section error instead of stale content.

`GET /events/{event_id}` accepts `X-Ticketopia-Section: true` for an HTML section
region plus inert canonical shell metadata. Also send `X-Ticketopia-Panel: true`
for context presentation and selection-aware action returns; omit it for standalone
semantics. Both headers are in `Vary`, responses are `private, no-store`, and input
validation/status codes are preserved. The client rejects redirects, whole sign-in
documents, and mismatched event/section/mode/thread responses. Full pages/panels
remain available; the JSON API is unchanged.

Only the selected section's optional lists are fetched. Overview retains Recommend
capability without community/discussion reads. Explicitly selected conversations
retain their companion community. Fresh metadata reconciles permissions, facts,
warnings and CSRF without recreating unchanged controls. No private section HTML
is persisted. See the [journey contract](design-guidelines.md#discovery-to-discussion-journeys-and-shared-state).

### UI source and assets

Reusable shadcn-templ primitives live in `views/components/`, with app composition
in `views/layouts/` and `views/home/`. See the [component guide](ui-components.md).
`views/styles/app.css` owns the visual tokens and responsive rules. Its compiled
Tailwind output, browser enhancement, htmx, and licensed Manrope font are embedded
from `views/assets/` and served at `/assets/`; no runtime styling CDN is required.
Event imagery comes directly from the normalized provider image records and has
a fixed-ratio fallback when missing or broken.

Install build dependencies with `npm ci`. After editing components or styles, run:

```sh
npm run build
```

This generates templates, bundles component scripts, builds Tailwind v4 CSS, and
builds the binary. Restart the Go server after asset edits because browser files
are embedded in the binary.

## JSON API

| Method | Path | Result |
| --- | --- | --- |
| GET | `/api/v1/events` | Filtered events with pagination and freshness. |
| GET | `/api/v1/events/{event_id}` | One normalized event, fetched on demand if uncached. |
| GET | `/api/v1/events/{event_id}/history` | Public dated history, SQL-only; `before`/`limit` pagination. |
| GET | `/api/v1/genres` | Music genre/subgenre metadata and freshness. |
| GET | `/api/v1/categories` | Categories with compatible genres/subgenres and freshness. |
| GET | `/api/v1/openapi.yaml` | [OpenAPI 3.1 contract](../internal/api/openapi.yaml). |

See the [OpenAPI guide](openapi.md) for what the contract describes, how it is
embedded and served, and how to validate and keep it aligned with the handlers.

Event filters: `city`, `country`, `keyword`, `category_id`, `genre_id`, `artist_id`, `venue_id`,
`start_date`, `end_date`, `sort`, `limit`, and `cursor`. Empty filters are treated as
omitted. Unknown/repeated parameters are rejected. The catalog and contract routes
accept no query parameters.
JSON reads use the supplied filters; browser location defaults are applied by
the HTML handler before calling the shared discovery service.
HTML pages are forgiving instead: Discover (`/`), Community (`/community`) and
event pages (`/events/{event_id}`) answer unsupported keys, such as `utm_*` or
`fbclid` from shared links, with a `302` to the same URL without them. Supported
pairs keep their order and encoding, so repeated or invalid supported values still
return 400 and cache keys stay canonical.

- `category_id` accepts a raw category/segment ID from `/api/v1/categories` or
  `all`. Omitted/empty means all categories, except legacy genre-only searches
  imply Music (`KZFzniwnSyZfZ7v7nJ`). Explicit `all` plus `genre_id` is rejected.
  Use a genre from the chosen category; category/genre combinations follow
  Ticketmaster matching and may return empty results. Catalog availability is
  not required to run a JSON event search.
- Dates use `YYYY-MM-DD` and are inclusive venue-local dates. End must be on/after
  start and no more than 366 days later. Date filtering follows provider semantics
  and normally excludes events whose dates are still TBA/TBD.
- `limit` defaults to 20 and accepts 1–100. Cursors are opaque page tokens bound
  to the normalized filters and limit. Keep explicit dates and the same filters
  on subsequent requests; omitted defaults may change at midnight UTC.
  Category is part of both cache and cursor scope. Cursors issued before this
  expansion must restart; old music-only cache entries cannot appear as all-category results.
- `sort` accepts `date_asc` (default), `date_desc`, or `name_asc`, with identity
  tie-breaks in SQL. Cursors bind the sort too. Result sets can shift between
  refreshes; pagination does not pin a snapshot.
- Ticketmaster requires `size * page < 1000`. `next_cursor` is `null` at the end;
  `limited: true` means the cap prevented another page. `total` is the provider's
  total, which can exceed the accessible results. Narrow filters to continue.
- Event, artist, and venue IDs use `ticketmaster:SOURCE_ID`. They survive title
  changes and distinguish identically named events. Category and genre IDs are
  raw taxonomy IDs from `/api/v1/categories`; `/api/v1/genres` still returns only
  music genres. The existing `artists` array and `artist_id` filter retain their
  names/IDs for compatibility and represent provider attractions, including teams
  and performers. URL-encode path IDs when needed.
- Known instants use RFC 3339. Missing instants are `null`; local dates/times,
  time zones, and TBA/TBD flags are retained. Unknown prices are `null`; amount
  strings preserve the provider's decimal representation. `fees_included` is
  `null` when unknown.
- `meta.data_as_of` is the successful collection time. `meta.stale` identifies a
  retained/older result served after refresh failure or a request-budget cooldown.
  MariaDB lists use the oldest supporting collection/snapshot instant and include
  `meta.coverage` with `complete`, `partial`, `stale`, or `not_collected`; totals
  count stored matches, not unseen upstream events. Empty incomplete/older results
  are not authoritative. MariaDB detail responses add public `history` with
  first/last seen and meaningful changes; these are distinct from `data_as_of`.
- Errors use `application/problem+json`: 400 for invalid or provider-rejected
  filters, 404 for a missing event, and 503 for unavailable data. Upstream 429
  becomes 503 with `Retry-After`
  when there is no usable cached result. Provider bodies and API keys are not
  included in responses or provider-error logs.

Example:

```sh
curl 'http://localhost:8080/api/v1/events?city=Chicago&country=US&start_date=2026-10-02&end_date=2026-12-31'
curl 'http://localhost:8080/api/v1/genres'
curl 'http://localhost:8080/api/v1/categories'
curl 'http://localhost:8080/api/v1/events?city=Chicago&country=US&category_id=KZFzniwnSyZfZ7v7nE'
```

## External request budget

The [Discovery API documentation](https://developer.ticketmaster.com/products-and-docs/apis/discovery-api/v2/)
lists default limits of 5 requests/second and 5,000/day. Ticketopia starts at most
four external requests/second and permits 4,500 calls per window of
24 hours, starting with its first request. This leaves headroom for other uses
of the key. Set `TICKETMASTER_DAILY_BUDGET` to a
positive integer to fit your account. All resource types share this budget.
Cache hits consume no requests. There are no automatic HTTP request retries.
[Scheduled city/date collection](event-history.md) is separately opt-in and uses
the same budget, not another allowance.

Timeouts and provider failures pause new external requests for 30 seconds; 429
pauses for at least a minute. Longer `Retry-After` values (seconds or HTTP dates)
and Ticketmaster's millisecond `Rate-Limit-Reset` for an exhausted daily quota are
respected, up to 24 hours. A successful response with zero remaining quota is
still returned and cached. HTTP calls time out after eight seconds; shared cache
refresh work is bounded to ten seconds. Response bodies are limited to 8 MiB.

Database-free pacing, budget accounting, cooldowns and request deduplication are
**per process** and reset on restart. Shared KV reuses results but is not a lock
or shared budget. In **MariaDB mode**, provider calls use durable shared budgeting,
pacing and cooldowns, even if the history scheduler is disabled. Scheduled refresh
claims are also durable/fenced; cache-miss deduplication remains process-local.
All callers sharing a key must use the same database and compatible binary.
Other applications/database-free callers using that key are not coordinated.
See [reliable history](event-history.md) for configuration, receipts and limitations.

## Verification

`go test -race ./...` covers metadata fidelity, cache reuse, concurrent misses,
caller cancellation, refresh/retention, outage cooldowns, quota budgets, empty
results, negative caching, corrupted entries, cursor validation, and shared
HTML/API behavior. `go vet ./...` checks the Go code. Validate the contract with
`npx --yes @redocly/cli lint internal/api/openapi.yaml`.

Route tests cover HTML/API detail-cache reuse, selected URL reconstruction,
failure isolation, parameter separation, valid local return contexts, and truthful
planned destinations. Browser checks exercise live Chicago data at 1440, 1024,
390, and 320px; selection/sections, rapid switching, retry, Back/reload/resize,
appended-page restoration, city detection, no-JavaScript routes/pagination,
long-title reflow, 200% text, reduced motion, and automated WCAG A/AA scans.
Human relevance/usability pilot checkpoints remain open.

### Verification by slice

Run these deterministic checks without a Ticketmaster key; tests use local provider
fixtures, including empty results, sparse non-music events, and taxonomy outages.

1. **Shared discovery/API:**
   ```sh
   go test ./internal/discovery -run 'TestCategor|TestSearchPreservesMetadata|TestGenreCatalog'
   go test ./internal/api -run TestCategoryAPI
   ```
2. **Web filters and event display:**
   ```sh
   go test ./internal/api -run 'TestCategoryWeb|TestCategoryCatalogFailure'
   ```
3. **Regression gate:**
   ```sh
   go test -race ./...
   go vet ./...
   npx --yes @redocly/cli lint internal/api/openapi.yaml
   ```

Browser smoke check: choose Chicago/US, switch Music + Rock → Sports + Basketball
→ All categories, and confirm the old genre clears. Repeat with JavaScript disabled
(apply category before choosing genre). Load another page, open an event, reload,
and return to the same category/genre. Check desktop, tablet, 390px, and 320px.
Live verification covered all returned categories, category-specific pagination,
previews/details/return navigation, and legacy music links, with six zero-violation
automated accessibility scans and no horizontal overflow.

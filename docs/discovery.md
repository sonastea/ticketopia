# Ticketmaster event discovery

Status: Implemented for the web UI and public read-only JSON API. Delivery of
later personal/community features is tracked in the [aligned checklist](goals/delivery.md).

The [responsive UX guide](design-guidelines.md) describes the discovery shell,
selected-event previews, dedicated event pages, and all-category discovery.
Community participation remains planned. [DESIGN.md](../DESIGN.md) records the
implemented visual system.

## Fetch once, reuse across clients

`internal/discovery` owns Ticketmaster requests, normalization, and caching.
HTML and JSON handlers call the same service. The browser never receives the
Ticketmaster key and the server does not call its own HTTP API.

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

Load-more keeps all filters, starts at provider page zero, and stops at the end
or Ticketmaster's paging cap. With JavaScript it appends rows; without JavaScript
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
  and dedicated event pages. The higher intermediate threshold leaves room for
  the navigation rail and readable results. The full shell caps at 100rem.
- Location stays visible above search. **Change city** opens the native Filters
  disclosure and focuses City. The same GET form exposes country, dates, category,
  and genre. Ordering is explicitly date-first; no unsupported sorting choices are
  presented. Inputs and disclosure remain usable without JavaScript.
- Event titles and **Details** are real `/events/{event_id}` links. On larger
  screens enhancement loads only the selected event into the preview, keeping
  results in place. `selected_event` and `section` in the discovery URL reconstruct
  the preview on direct entry or reload; these are HTML-only parameters.
- Event pages use `section=overview|discussion|community` and a validated local
  `return_to` URL. Overview shows supplied descriptions, venue information, prices,
  status, separate sale dates, freshness, and the Ticketmaster link. Discussion
  and Community explain their current unavailable state without invented counts.
- Back restores previous selection; loaded result pages, focus, and scroll are
  retained in browser history across the event's full-page section links. A
  one-use, same-tab session-storage handoff carries the originating history entry;
  if storage is unavailable, a normal return
  link still preserves validated search filters. Resizing retains the selected
  event without adding history; narrow selected URLs show the preview in-shell.
- Preview loads cancel obsolete requests, have bounded timeouts, and offer Retry
  and Open event on failure. A failed preview leaves the discovery list usable.
  Keyboard users can tab to **Jump to event preview** on the selected row.
- `/saved`, `/community`, `/me`, and `/me/interests` are navigable availability
  pages. Save/Interested/Recommend controls are explicitly disabled and explained.
  These routes do not implement accounts, persistence, or community mutations.

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
| GET | `/api/v1/genres` | Music genre/subgenre metadata and freshness. |
| GET | `/api/v1/categories` | Categories with compatible genres/subgenres and freshness. |
| GET | `/api/v1/openapi.yaml` | [OpenAPI 3.1 contract](../internal/api/openapi.yaml). |

See the [OpenAPI guide](openapi.md) for what the contract describes, how it is
embedded and served, and how to validate and keep it aligned with the handlers.

Event filters: `city`, `country`, `keyword`, `category_id`, `genre_id`, `artist_id`, `venue_id`,
`start_date`, `end_date`, `limit`, and `cursor`. Empty filters are treated as
omitted. Unknown/repeated parameters are rejected. The catalog and contract routes
accept no query parameters.
JSON reads use the supplied filters; browser location defaults are applied by
the HTML handler before calling the shared discovery service.

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
- Provider ordering is date/name ascending. The upstream result set can shift
  between refreshes; this is not a snapshot cursor or a durable history.
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
  retained result served after refresh failure or a request-budget cooldown.
  These values are not durable first-seen/change-detection timestamps.
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
four external requests/second and permits 4,500 calls per process window of
24 hours, starting with its first request. This leaves headroom for other uses
of the key. Set `TICKETMASTER_DAILY_BUDGET` to a
positive integer to fit your account. All resource types share this budget.
Cache hits consume no requests. There are no automatic request retries or
background crawls.

Timeouts and provider failures pause new external requests for 30 seconds; 429
pauses for at least a minute. Longer `Retry-After` values (seconds or HTTP dates)
and Ticketmaster's millisecond `Rate-Limit-Reset` for an exhausted daily quota are
respected, up to 24 hours. A successful response with zero remaining quota is
still returned and cached. HTTP calls time out after eight seconds; shared cache
refresh work is bounded to ten seconds. Response bodies are limited to 8 MiB.

Rate pacing, budget accounting, cooldowns, and concurrent-request deduplication
are **per process** and reset on restart. Shared KV reuses stored results across
instances but does not provide distributed locking or a shared request budget.
Use the planned single-host deployment initially; distributed coordination and
durable scheduled collection belong to the next backend milestone.

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

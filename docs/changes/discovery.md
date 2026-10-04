# Event discovery changes

## Unreleased

### 2026-10-03 — All-category discovery, responsive event views, and city detection

- Deliver all-category discovery in three independently verifiable slices:
  category-aware shared discovery/API, web filters/non-music display, and regression
  checks. See [verification commands](../discovery.md#verification-by-slice).
- Add a cached category/genre catalog and `/api/v1/categories`; keep legacy
  genre-only links scoped to Music, preserve resource IDs, and isolate category
  cache/cursor scopes. Pre-expansion cursors require a new search.
- Add category-compatible genre options with JavaScript and ordinary GET forms,
  clear genres on category changes, and show neutral event/performer/team metadata.
- Verify provider fixtures, catalog outages, empty/sparse events, race/vet/OpenAPI,
  live Chicago categories, pagination, previews/return context, and six responsive
  accessibility views with no violations or horizontal overflow.

- Initialize the confirmed web product record and refactor discovery into the
  chosen cobalt/record-shop/mixtape direction, with shared components, local assets,
  responsive sidebar/rail/bottom navigation, and image-led event rows.
- Add real event pages and contextual previews using cached metadata, shareable
  selection/section URLs, Back/resize continuity, isolated errors, and retry.
- Expose truthful planned-feature destinations and participation states; retain
  regular GET search, no-JavaScript navigation, and load-more pagination.
- Verify race/vet checks, cached detail reuse, URL and return-context handling,
  responsive browser journeys, focus, reflow, and automated accessibility.
  See the [UI guide](../discovery.md#responsive-navigation-and-event-details),
  [visual system](../../DESIGN.md), and [broader UX scope](../design-guidelines.md).

- Add an explicit browser-side **Detect my city** action, using the browser's
  public IP with a 24-hour local cache, editable results, and a manual fallback.
  See [localhost location detection](../location.md#detecting-a-city-on-localhost).

### 2026-10-02 — Shared Ticketmaster discovery and metadata

- Fetch and normalize events with stable provider-scoped IDs, artist/venue and
  classification metadata, concert/sale dates, status, images, and optional prices.
- Fetch music genre/subgenre metadata and reuse cached reads across web/API clients.
- Add filtered event list/detail and genre endpoints, input validation, bounded
  cursor pagination, freshness, structured errors, and an OpenAPI contract.
- Replace title-grouped HTML with searchable, distinct event occurrences, accurate
  dates/ticket links, responsive filters, and loading/empty/error/stale states.
- Default the web experience to a chosen, remembered, or approximate IP-based
  city. Add an editable location label and a city prompt when detection fails,
  avoiding unscoped event requests. See [location behavior](../location.md).
- Cache city hints, bound lookup time/budget, and use explicit trusted proxy
  configuration; preserve shared city-level event caching and pagination filters.
- Verify cache call counts, concurrent misses, retention, failures, pagination,
  normalization, and web/API reuse; smoke-check live data and browser behavior.
- Align implemented discovery with upcoming backend and frontend milestones in
  the [delivery checklist](../goals/delivery.md).

See [discovery](../discovery.md) and [cache behavior](../cache.md) for current details.

# Event discovery changes

## Unreleased

### 2026-10-23 — Faster Discover rendering

- Memoize loaded time zones, which `time.LoadLocation` re-read from disk on every
  Discover search and render, and bounded `utils.CN` class-merge results. A
  20-event Discover render fell from 1.20 ms to 0.45 ms (local benchmark).
- Pause artwork tint requests for a minute after a transient batch failure, so an
  unavailable CDN no longer adds the 1.5s deadline to every uncached search.
  See [discovery](../discovery.md#responsive-navigation-and-event-details).

### 2026-10-23 — Quiet spotlight selection

- Replace the selected row's plum leading edge and shadow, whose rounded corners
  clashed with straight list separators, with a flat 35% pale-plum wash and a
  "Viewing" label after the date (space reserved, so nothing reflows). List
  separators beside the selected row turn transparent; cards tint their border.
- Rows gain a stone hover and flat press wash from their title or artwork links;
  forced colors frame the selection with `SelectedItem`.
  See [discovery](../discovery.md#responsive-navigation-and-event-details).

### 2026-10-23 — Tonight's events in US evenings

- Start the default Discover range on the browsed city's calendar day, not the UTC
  date, which is already tomorrow during a US evening. The city's zone is learned
  from its results into a bounded memo, so only a city's first search may refetch
  once. Explicit `start_date`, cursors and the `/api/v1/events` UTC default are
  unchanged. See [discovery](../discovery.md#web-ui).

### 2026-10-23 — Pagination loading placeholders

- While **Load more events** runs, show three `aria-hidden` placeholder rows in the
  result list's container layout. A highlight sweeps across them only without a
  reduced-motion preference; the "Loading events…" label stays the announced state.
  See [discovery](../discovery.md#web-ui).

### 2026-10-23 — Command bar, date chips, display face and artwork tints

- Merge location and search into one Discover command bar (**Events in {city}** ·
  search). Add sideways-scrolling **Tonight / This weekend / Next 7 days** links
  that set only `start_date`/`end_date` for the browsed city's day. Tests parse
  every chip URL.
- Add Bricolage Grotesque (OFL, self-hosted 65 KB Latin subset, `--font-display`)
  for page titles, the event title and date numbers only.
- Derive each event header's tint from artwork once, when events are cached or
  stored. The fetch is bounded and Ticketmaster-CDN-only; the value is normalized
  to OKLCH L .66 / C ≤ .12, stored as `tint` in the snapshot JSON (no migration)
  and documented in OpenAPI. The header mixes it 18% into white, which tests show
  stays AA for every hue, with a brand-hue fallback.
  See [discovery](../discovery.md#responsive-navigation-and-event-details).

### 2026-10-23 — Artwork-led discovery

- Lead rows with larger artwork and a perforated ticket-stub date chip on the
  image. The results container decides the layout: stacked when narrow, rows from
  26rem, and an intrinsic two-to-three-column card grid from 48rem (Discover,
  Saved, Interests, Radar), replacing the 40/62/86rem row-image steps.
- Mark the selected row with a 3px leading brand edge on a raised surface instead
  of a plum fill (Highlight in forced colors).
- Same-origin navigations use cross-document view transitions (250ms, reduced
  motion opts out); event artwork/titles share per-event names across rows, the
  preview and the event page.
- Event pages open with a wide artwork band and tinted identity surface. Below
  62rem, Tickets and Save form a sticky normal-flow bar above the bottom
  navigation on viewports at least 32rem tall.
  See [discovery](../discovery.md#responsive-navigation-and-event-details).

### 2026-10-23 — Quieter rows and screen-reader fixes

- Drop the provider-description excerpt from rows and hide Featuring performers
  already named in the title (rows and Overview).
- Make the row title the only tab stop, focus the preview heading after choosing
  an event on desktop as on compact, and stop announcing Radar coverage notes.
- Align the search label with venue matching, shorten its placeholder, say
  **Enter a city** before one is chosen, and replace "Country code" with
  "Country (optional, 2-letter code)" across forms.
  See [discovery](../discovery.md#responsive-navigation-and-event-details).

### 2026-10-22 — Compact Discover first screen

- Merge freshness, stale fallback and coverage into one non-live results line,
  keeping `data-coverage`; fix the repeated "Showing … Showing" stale copy.
- Put a shorter sort label ("Soonest first") on the results heading line and
  move Save privacy from above every result list into the first enhanced Save's
  confirmation in each browser (page memory if storage is blocked).
- At 390×844 the first event starts at 475px with a stale, partial-coverage
  fixture; the four-item bottom bar stays on one row at 320px.
  See [discovery](../discovery.md#responsive-navigation-and-event-details).

### 2026-10-22 — Shared links with tracking parameters

- Discover, Community and event pages redirect (`302`) away from unsupported query
  keys such as `utm_*`/`fbclid` instead of returning 400; `/api/v1` stays strict.
  See [discovery](../discovery.md#json-api).

Older entries: [discovery archive](archive/discovery/).

See [discovery](../discovery.md) and [cache behavior](../cache.md) for current details.

# Event discovery changes

## Unreleased

### 2026-10-27 — Touch-safe preview prefetch and clearer partial results

- A touch press starts its preview request only after a 100ms hold or on
  release (still before the click), so scrolls that begin on a result no longer
  reach the server: 12 of 12 sampled touch scrolls started one before, none
  after. Mouse and pen presses still start it at once.
- Uncollected dates summarize as **Results may be incomplete** rather than
  **Not collected yet**, which read as "nothing here" above shown results.
  See [discovery](../discovery.md#loading-performance).

### 2026-10-26 — Right-sized artwork, pressed prefetch and lighter long lists

- Artwork lists one crop's renditions in `srcset` with layout-derived `sizes`,
  so 2x phones take 305px thumbnails instead of 640px.
- Pressing a result prefetches its event page (speculation rules) or, on
  Discover, starts its preview request.
- Off-screen rows skip rendering once they have rendered at the current layout,
  keeping scroll restores and the selected-row correction exact.
  See [discovery](../discovery.md#loading-performance).

### 2026-10-25 — Server-rebuilt result history

- Stop storing result HTML in history. Load more mirrors the last page's cursor
  into the Discover URL (with an implicit city and dates spelled out, kept in
  step on same-document Back) as HTML-only `through=<cursor>`; the server renders
  every page through it, with current saved/interest state and CSRF, so reload
  and Back restore at once. Restores take whole pages up to 10 pages or 200
  events, drop overlapping rows, use the oldest page's time and batch private
  reads. A stale `through` on a whole page falls back to the requested page.
  See [discovery](../discovery.md#restoring-loaded-pages).

### 2026-10-24 — Selected row stays in view

- Opening the desktop preview centers the chosen row before paint, without motion,
  after cards reflow into rows. Closing or widening moves only an out-of-view row;
  visible rows never shift under the pointer. See [discovery](../discovery.md#responsive-navigation-and-event-details).

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

Older entries: [discovery archive](archive/discovery/).

See [discovery](../discovery.md) and [cache behavior](../cache.md) for current details.

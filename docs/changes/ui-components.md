# UI component changes

## Unreleased

### 2026-10-26 — Mobile polish

- Phone-width results keep a 5.5rem thumbnail beside the facts (about 370px per
  result instead of 600px); enlarged text still stacks.
- The floating navigation hides on downward scrolls and returns on upward ones
  (scroll-direction queries; other browsers unchanged), and the event Tickets bar
  drops into its place. Event section tabs stay on one swipeable row.
- Filter fields align despite wrapping labels and Sort no longer truncates;
  text disclosures share one chevron; search and country fields get keyboard
  hints; the header wordmark meets 44px.
- Add a favicon, apple-touch icon and web manifest (`minimal-ui`) from new SVG
  ticket-tile sources ([icons README](../../views/assets/icons/README.md)).

### 2026-10-26 — Interaction motion

- Sidebar collapse/expand and desktop preview open/close animate as 380ms
  same-document view transitions (the grid still switches instantly, so no
  per-frame workspace layout): the page takes its new layout at once while the
  rail or preview slides over it like a drawer.
- The signed-in Save control reserves the **Remove from Saved** width, so saving
  no longer pushes it onto a new row (seen at ~380–530px and in some desktop
  card widths).
- Native controls share a 160ms colour transition baseline, and borders now ease
  too (library buttons, actions, section tabs). Action feedback, inline errors and
  newly revealed controls fade in, toasts drop in, pending forms dim gradually and
  rail labels fade out. Found by a four-way transition audit.
- Section and conversation swaps fade in, Load more rows settle in, late result
  artwork fades in, and linked/just-written posts flash once. Rows surviving a
  Saved/Interests removal and reordered sidebar items/sections slide into place.
- Disclosures grow open and shut via `::details-content`; loaded previews settle
  in; collapsed-rail labels fade in; saved bookmarks and interested stars fill and
  pop when toggled. All motion is skipped under reduced motion (see the
  [guide](../ui-components.md#modern-css-conventions)). Verified in Chrome; WebKit
  opens disclosures instantly.

### 2026-10-25 — Browser main-thread work

- Stop loading the ~247 KiB component bundle on every page. Only native Select
  and Collapsible variants render, so the bundle is still built but not loaded;
  the bundle's lifecycle observer ignores text-only mutations.
- Draft restore observers (discussions, recommendations) and the live-activity ID
  observer react only to added/removed subtrees containing their targets. Live
  count snapshots index targets once and skip unchanged counts. Toast collision
  checks run only while a toast is shown.
- Draft writes to `sessionStorage` are throttled to one per 400ms while typing and
  flushed on submit, hide and pagehide. The discussion composer writes its
  viewport variable only when the viewport size changes.
- Interest controls re-enable after the confirmed write instead of waiting for
  the public-participant refresh, which now times out after 10 seconds.
- Results history snapshots serialize the list once, and only after Load more has
  extended it; first-page entries keep only scroll/focus. Back/forward-cache
  restores keep the live results instead of rebuilding them, removing a duplicate
  saved-state request.
- Placeholder shimmer uses a composited `translate`; sidebar collapse is instant.
  The compact header's wordmark now steps to the headline size under a
  `scroll-state(stuck: top)` query instead of shrinking on every scroll frame
  (design change: a threshold, not a continuous shrink). Root scroll anchoring is
  off so the step can't stall scrolling at 200% text; browsers without
  scroll-state queries keep a static header (see the
  [guide](../ui-components.md#modern-css-conventions)). Verified with CSS and
  event-context checks in Chromium and WebKit.

### 2026-10-23 — Finishing touches: floating navigation, warm text, scroll header

- Float the phone navigation as an inset pill above the bottom safe area: a soft
  white/pale-plum surface, a one-pixel border, a bordered current pill (Highlight
  in forced colors) and .75rem labels. Small phones use viewport-capped insets so
  enlarged labels stay whole. The event Tickets/Save bar sits above it as a card,
  now on one row (122 → 70px at 390px and 320px wide): narrow bars show
  **Tickets** and keep "Tickets on Ticketmaster" as the accessible name.
- Add `--muted-warm` (muted mixed 12% toward plum) for all secondary text and the
  library's muted foreground; it is AA on every app surface and event tint.
- Below 62rem, stick and shrink the compact header with a scroll-driven animation
  under `@supports` and no reduced-motion preference; otherwise it stays static.
- Snap destination empty-state headings to the headline size; record the 1.6rem
  wordmark as a logo exception. Let event-page facts wrap below the date marker
  instead of breaking words at 320px with 200% text.
- Fix two Safari section-navigation faults. A few seconds of scrolling saved the
  reading position every frame, exhausting WebKit's 100-writes-per-10-seconds
  history budget, so section links silently did nothing. Positions now save once
  scrolling settles, and a refused history write never aborts a navigation. Focus
  also now returns to a clicked section link, which WebKit never focuses itself.
- Bring browser checks up to date: the current stale-data copy, waiting out the
  cross-document view transition on no-JavaScript pages, and production's
  `no-store`/`Vary` headers on the read-only preview's event route.
- Verify Chromium (273) and WebKit (269) CSS checks, event-context checks (228,
  eight zero-violation axe views) in both engines, and an axe/overflow pass over
  48 views at 1440/768/390/320px, 200% text, forced colors and reduced motion.
  See [DESIGN.md](../../DESIGN.md#navigation) and the
  [component guide](../ui-components.md#modern-css-conventions).

Older entries: [UI component archive](archive/ui-components/).

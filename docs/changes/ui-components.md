# UI component changes

## Unreleased

### 2026-10-27 — Enlarged-text wordmark and offline nonce

- At 200% text on a 320px phone the header wordmark no longer wraps letter by
  letter (786px header, now 144px). The logotype and the header's padding and
  gaps stop growing at their default-text size on a 320px screen; the search
  action keeps scaling. Default-text layout is unchanged.
- The cached offline page drops its always-empty nonce attributes; a future CSP
  must allow its inline style and script by hash.

### 2026-10-26 — Search shortcut, filter sheet, calendar and offline page

- The sticky phone header ends with a search icon: on Discover it focuses the
  keyword field in place (keeping filters); elsewhere it opens `/#keyword`.
- Below 62rem, Filters opens as a native modal `<dialog>` bottom sheet inside
  the same GET form (Escape, close or backdrop; focus returns to its opener).
  Without JavaScript, or from 62rem, the fields stay inline.
- Overview adds **When** with an **Add to calendar** `.ics` download
  ([OpenAPI](openapi.md)) once the provider announces a date.
- A service worker shows a self-contained offline page when a navigation fails,
  and caches nothing else ([discovery guide](../discovery.md#ui-source-and-assets)).

### 2026-10-26 — Event page quick wins

- The venue address links to Maps: Google's universal link (the Maps app on
  Android, the web elsewhere), and Apple Maps on iPhone and iPad.
- A Share icon beside Back (page and preview) opens the share sheet with the
  canonical event link, or copies it. It is shown only where one of those works,
  and stays out of the Tickets bar so Save keeps its one row at 360–390px.
- "Before you go" notices written as inline " - " bullets display as a list;
  other prose, including stray dashes, stays a paragraph. Stored text is unchanged.
- Results warnings (last-known, partial, not collected, older) collapse to a
  one-line summary that opens into the full explanation; plain freshness stays a
  line.

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

Older entries: [UI component archive](archive/ui-components/).

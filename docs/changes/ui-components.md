# UI component changes

## Unreleased

### 2026-11-11 — Discussion feedback, slow reads, tabs, search and 404

- Unknown pages opened in a browser get a site page (404, or 405 for a form
  action's address) with a way back to Discover; API, MCP and other
  non-HTML requests keep Echo's JSON. See [OpenAPI](../openapi.md).
- The current-tab indicator is its own element and slides between tabs (240ms,
  `--ease-standard`): in place on event sections, and by cross-document view
  transition on Community and moderation tabs. Static under reduced motion or
  without JavaScript; Highlight in forced colours, where plain tabs drop their
  forced underline.
- A Discover search still loading after 300ms shows the shared pending ring on
  its submit button (cleared when Back restores the page from cache).
- Forced colours no longer ring every button and nav link twice: the resting
  `outline-hidden` outline is dropped, so focus is the only outline.

- Helpful, Publish, Save changes and Remove show the shared 300ms pending ring;
  a confirmed Helpful count rolls in from the direction it moved (a fade under
  reduced motion), and Helpful takes the participation hover/press washes.
- Event sections and conversations show a thin brand loading line (content
  stays at full contrast) only when a read outlasts 250ms. Reply lists clear their heading; error recovery actions sit below their
  explanation. See [DESIGN.md motion](../../DESIGN.md#buttons).

### 2026-10-29 — Aligned event card actions and versioned assets

- In the results card grid, event cards fill their row height and pin the action
  group to the bottom, so ticket and Save buttons line up across a row. Discover
  shows the activity preview before the actions. See [CSS conventions](../ui-components.md#modern-css-conventions).
- Page stylesheets and scripts link through `assets.Path`, so CSS and JavaScript
  changes reach browsers on deploy. See [browser assets](../cache.md#browser-assets).

### 2026-10-27 — Enlarged-text wordmark and offline nonce

- Follow-up review fixes: event return labels wrap beside Share; Community
  bylines wrap attribution below the avatar when needed. The header tagline
  responds to actual text-sized container space, avoiding wordmark crowding at
  640px with 200% text. Enhanced post arrivals retain reduced-motion and
  forced-colors markers without a URL hash. See [CSS conventions](../ui-components.md#modern-css-conventions).
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

Older entries: [UI component archive](archive/ui-components/).

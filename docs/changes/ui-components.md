# UI component changes

## Unreleased

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

### 2026-10-12 — Enlarged-text compact navigation

- Reflow full destination labels intrinsically, preserving targets and native links.
  Reserve actual sticky-bar/safe-area height in the shell instead of a fixed spacer.
- Preserve rail/desktop thresholds; add label/content-clearance browser regressions.
  See [dated flow priorities](../design/ui-flow-polish-2026-10-12.md).

### 2026-10-10 — Quieter event views and segmented navigation

- Establish “Local frequency, less interface” in the [design philosophy](../../DESIGN.md#design-philosophy):
  event-first hierarchy, restrained action styling, and one visual grammar per control.
- Replace event underline buttons with responsive segmented navigation, including
  an explicit forced-colors current state and real link semantics.
- Quiet participation toolbars, move row Overview navigation into the metadata
  disclosure, mute genre labels, and hide zero-interest counts until positive.
- Simplify the preview to one container and a labeled Back to results action.
- Preserve private/public labels, independent saves/interests, reminder access,
  and the discussion-first selection and return-state contracts.
- Verify Chromium/WebKit responsive fixtures, 200% text, long content, touch,
  forced colors, and reduced motion; verify live count updates from zero to one
  and back using mocked responses. Home package race tests, vet, and build pass.

Older entries: [UI component archive](archive/ui-components/).

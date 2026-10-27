# UI components

Ticketopia uses [shadcn-templ](https://shadcn-templ.com/docs/installation), with
the CLI pinned to **v2.0.0-beta.10** in `go.mod`. Component source is owned by this
repository under `views/components`; `components.json` configures installation
paths, the Nova base style, and the embedded script bundle.

## Build and develop

The shared shell's [live activity toasts](live-community-activity.md) sit outside
document flow. Happening Now owns the native recent snapshot, filter disclosure,
explicit updates and toast visibility controls. The standalone embedded
`views/assets/live-activity.js` shares one stream with numeric count updates,
preserves local hide/account pause and reconciles redaction/expiry in place without
remounting composers. `views/styles/app.css` owns logical spacing, responsive toast
bounds and shared-token focus/press treatment. Regenerate templ and CSS after edits.

The shared live-filter fields include native artist/venue search and checkbox
selections on Happening Now and Preferences. `views/assets/live-pickers.js` enhances
only lookup/selection; Apply still confirms the entire filter. Names remain in
document memory, IDs in preferences. Picker search fields use ordinary `.field`
styles; only selection/result rows use `.account-checkboxes` so text fields remain
shrinkable on compact/enlarged layouts.

Use Go 1.27+ and Node.js 24 with npm. The Node release line is declared in
`package.json` and `.nvmrc`; with nvm, run `nvm install` and `nvm use` first:

```sh
npm ci
npm run build
TICKETMASTER_KEY=your-api-key ./bin/ticketopia
```

The build bundles component JavaScript, generates templ Go code, compiles Tailwind
CSS 4.3.3, and builds the Go binary. The binary embeds all browser assets and can
run outside the repository. CSS, generated Go, the bundle manifest, and the hashed
JS bundle are checked in so `go run ./cmd/ticketopia` still works after checkout.
Regenerate and commit them together after source changes. `bin/` and `node_modules/`
are ignored. Air runs the same build and watches CSS/JS as well as Go and templ.

```sh
go tool shadcn-templ add textarea
npm run build
go mod tidy
go test -race ./...
go vet ./...
```

Import installed components using the module path, for example
`github.com/sonastea/ticketopia/views/components/button`. Render
`@button.Button(button.Props{Type: button.TypeSubmit}) { Search }` within a form.
The build still produces the script bundle, served at
`/assets/js/shadcn-templ-<hash>.js`, but no page loads it: pages render only the
native Select and Collapsible variants, which need no library JavaScript. Add
`@components.Scripts()` back to `views/layouts/base.templ` (once, in `<head>`)
before shipping a JavaScript-enhanced component.

## Theme and coverage

`views/styles/app.css` imports Tailwind v4 and the library's animation/style
utilities, scans the templ/component sources, and maps shadcn semantic colors to
Ticketopia's plum actions, light-stone navigation, dusty-rose dates, and ink palette.
Manrope remains self-hosted. Page composition, event rows, imagery, date indexing, breakpoints,
and route semantics remain app-owned; see [DESIGN.md](../DESIGN.md).

- Buttons cover actions, navigation items, section links, ticket links, pagination,
  disabled participation controls, and preview recovery.
- Input and Label cover visible fields. Hidden form metadata remains native HTML.
- Badge covers event sale statuses and unavailable-feature labels.
- Card, Alert, Empty, and Skeleton cover context, tickets, feedback, and loading.
- The library's Lucide icons replace the hand-authored SVG switch.
- The app-owned sidebar uses grouped navigation, a 14rem/4.5rem collapse state,
  and explicit reorder controls. Browser-local preferences and editing semantics
  are documented in the [responsive UX guide](design-guidelines.md#sidebar-preferences).
  Brand tokens are semantic (`brand`, `brand-dark`, `brand-active`, `brand-soft`);
  navigation and dates use distinct `nav-surface`, `nav-hover`, and `date-surface`.
  Pale-plum selection (`#ebdde8`), neutral hover stone (`#e6e0e5`), and warmer
  dusty-rose date paper (`#e8d2db`) are deliberately distinct. Rose remains
  exclusive to date markers; availability uses stone, and focus/actions use plum.
  Loading actions use plum text on pale plum to retain readable contrast.
- JavaScript preview states clone server-rendered component templates, keeping
  initial pages, fetched fragments, loading, errors, and retry consistent.
- Event actions use `home.EventActions`: pass sibling controls as children and
  keep dynamic save feedback outside its wrapping controls row. Do not place
  status/help text inside a form being aligned alongside other buttons; align
  controls at the top when wrapping labels make their heights differ. Reuse this
  separation for future inline action feedback instead of offsets or fixed heights.
  Within this row, quiet actions use transparent surfaces/borders and
  shared spacing; Save and Interested share pale-plum selected states and distinct
  guarded hover/flat press feedback. Unavailable controls retain muted styling,
  pending forms keep their plum state. On-sale reminder uses the same quiet
  control with a bell icon; named utility destinations stay lighter navigation links.
  Event-section links also wrap, and pagination action minimum widths are capped
  at the available width, rather than overflowing when text is enlarged.
  Filter-summary metadata can shrink and break long words without pushing its
  disclosure icon outside the control.
- Personal destinations use `layouts.AccountNav(current, permissions)` from
  `views/layouts/account_nav.templ`. Inputs are presentation-only; handlers still
  authorize reads/writes and supply current Safety/moderator visibility. Current
  family links remain outside the native secondary-family disclosure, avoiding
  account/home circular imports. Community/public-profile collections reuse
  `home.CollectionEventArt` and `home.CollectionEventHeading` for Overview identity
  and return context; these primitives never add participation controls.

Ordinary headings, content links, semantic landmarks, lists, and provider images
remain HTML; they are not interactive widget substitutes.

## Modern CSS conventions

Follow [Good CSS](https://good-css.com/skills/good-css/SKILL.md) and the repository's
[agent guidance](../AGENTS.md#modern-css) in the existing Tailwind/CSS architecture.
`views/styles/app.css` owns the reset, logical sizes/insets/spacing, OKLCH palette,
derived `color-mix(in oklch, ...)` states, and fluid heading tokens. Approved plum
selection and rose date colors remain distinct base palette roles; they are not
generated hover shades. Do not change vendored style utilities to implement app
conventions.

The application stays light regardless of OS preference, including its initial
color-scheme metadata. Library dark tokens use one `light-dark()` set; `.dark`
only opts a subtree into `color-scheme: dark`, not a shipped whole-app dark mode.
Tailwind's `hover` variant and app hover rules require both hover capability and
a fine pointer. Touch has flat pressed feedback; links remain selectable. Focus
uses real outlines, including forced-colors mode. Geometry transitions and animations opt into
`prefers-reduced-motion: no-preference` individually; there is no global rule
that zeroes durations, so new motion must carry its own guard.

Use parent-owned gaps and shrink-safe tracks. Page padding comes from the fluid
`--space-page-block`/`--space-page-inline` tokens (also the event page's
`--page-gutter`) rather than viewport steps, and reusable groupings respond to
their `results` or `workspace` container instead of the viewport. Keep behavioral shell thresholds
at 62rem/72rem synchronized with JavaScript. Heading sizes adapt with bounded,
rem-based `clamp()` tokens, while fields remain at least 16px to avoid iOS focus
zoom without disabling user zoom. Text wraps without global min-content-shrinking
`anywhere`; dates, prices, counts, and changing numeric facts use tabular figures.
Image wrappers reserve aspect ratios and clip without creating scroll containers.
Actual sidebar/preview scroll areas retain `overflow-y: auto`, stable gutters,
and contained overscroll. Textareas grow from 8rem to 24rem with `field-sizing`
where supported, retaining native scrolling and manual resizing otherwise.

Compact navigation is a floating pill with an intrinsic grid, .75rem labels and
44px targets. Under the sticky-header conditions it also hides on downward
scrolls: the root is a `page-scroll` scroll-state container, and
`scroll-state(scrolled: bottom)` translates the pill away (not while it holds
focus). The event action bar changes its sticky `inset-block-end` rather than
translating, so a bar that has settled in flow never overlaps what follows.
Disclosures relying on the UA marker (`.account-more`, `.follow-search`,
`.event-provenance`, `.account-token-details`) share a border-drawn chevron
instead, so WebKit and Chromium match and forced colours keep it. It reflows to two rows at 320px/390px with 200% text, keeping each
label on one unbroken line. Its sticky shell row and margins reserve its actual
height plus the `--nav-float-gap` above the bottom safe area, so page content remains
reachable without a guessed spacer; JavaScript only reports the height to the
event page's sticky action bar. Because media-query rem ignores enlarged page
text, JavaScript also sets `data-short-viewport` on the root below 32 actual
rem of viewport height, which keeps that bar in place. Side insets use physical safe-area margins. The
intermediate rail remains fixed and desktop navigation is unchanged.

Secondary text uses `--muted-warm`, derived from `--muted` and `--brand`; use it
rather than `--muted` for any new muted text. The compact header is a
`scroll-state` container: under `@container compact-header scroll-state(stuck: top)`
its content recentres and the wordmark steps to 1.25rem, so it lays out once on
sticking and once on unsticking rather than every scroll frame. The wordmark
changes `font-size`, not a scale: with enlarged text it wraps, and only the layout
change returns the sticky header's height to the reader. The same block sets
`overflow-anchor: none` on `html` and `body` (Chromium reads the root value from
`body`). Otherwise, at 200% text the 69px step would be compensated back below
the 1rem threshold, unsticking and regrowing the header so scrolling stalls. Placeholder
shimmer moves a clipped `::after` highlight with `translate`, which browsers composite,
rather than repainting a background position. Sidebar collapse switches its grid
tracks instantly; transitioning them would lay out the workspace on every frame.
Instead `app.js`'s `shellTransition()` wraps the switch, and desktop preview
open/close, in `document.startViewTransition()`, setting `data-shell-transition`
on the root for the latest transition. That names the sidebar, `#main` and the
preview and drops per-event names. `#main` is not animated, since its reflowing
results ghost under a crossfade; the rail and preview groups clip their
natural-size snapshots and slide over it. A preview open defers its DOM swap to
the transition callback, so it awaits that before applying the fetched panel, and
superseded opens/closes skip their swap via the selection generation.
Disclosures animate `::details-content` block size with a paint-only `clip-path`
(no layout change; .375rem below the edge stays visible for focus outlines).
Icon pops use the Web Animations API from `app.js`/`interests.js` only when state
flips in place, so server-rendered saved states do not animate on load; the same
applies to fading in reminder and visibility controls. Feedback, errors and toasts
that start hidden use CSS keyframe entrances instead (an animation restarts when
`[hidden]`'s `display: none` lifts), avoiding `transition` shorthands that would
replace a control's colour transitions. A low-specificity `:where()` colour
baseline near the global press rule covers native controls; any rule declaring
its own `transition` replaces it, so list colour properties there too.
`app.js` shares `enter(node, keyframes, duration)` for one-off entrances and
`flip(nodes, update)` for reorders: it measures, runs `update`, then animates
`translate` from the old positions, subtracting a moving ancestor's offset for
nested groups/items and restarting from in-flight positions on rapid repeats.
Collection removal runs its scroll compensation and focus handoff inside
`update`, so neither becomes motion. Entrances are limited to reader-initiated
changes (not history restores or post-write refreshes); posts use a transient
`data-arrived` attribute, alongside `:target`, for the arrival tint. Scroll-driven,
shimmer, disclosure, preview, view-transition and pop motion all sit under
`prefers-reduced-motion: no-preference` (scripts check the same query).

Modern features follow the existing Tailwind v4 browser baseline (Chrome 111+,
Safari 16.4+, Firefox 128+). `light-dark()` requires Chrome 123+/Safari 17.5+;
older browsers retain explicit light-token fallbacks, without library dark-mode
opt-in. Pretty wrapping, intrinsic-size animation, the scroll-state sticky header,
and textarea auto-sizing progressively enhance browsers that support them.

## Local adaptations

Copied component source is intentionally editable. Preserve these changes when
updating components, and review the registry diff before using `--overwrite`:

- `button/button.templ` uses `templ.URL` rather than `templ.SafeURL` so provider
  and caller-supplied links retain protocol sanitization.
  Its interaction defaults omit the library's pressed translation and blanket
  selection suppression, use color-only transitions, and retain a transparent
  outline rather than removing it. App CSS owns visible focus and press feedback.
- `select/native.templ` offers a themed native `<select>`. Category and genre
  remain keyboard-accessible ordinary form controls; changing category replaces
  compatible genre options, and both work without JavaScript.
- `collapsible/native.templ` offers native `<details>/<summary>` using the shared
  props. Browser form validation and Change city can reveal the filters, including
  without the library's JavaScript behavior.
- The theme preserves 44px actions, visible outline focus, flat border-only depth,
  readable disabled states, reduced motion, and the existing responsive shell.
- `empty/empty.templ` routes child-link hover through the guarded Tailwind `hover`
  variant instead of an unconditional arbitrary `a:hover` selector.
- Rendered components (`alert`, `badge`, `button`, `card`, `empty`, `input`) use
  logical inline and block utilities (block ones need Tailwind 4.2+). They clip with `overflow-clip`, name color-only transitions, and keep a
  transparent `outline-hidden` instead of `outline-none`.

- `baseui/lifecycle.js` rescans for component parts only when a mutation adds or
  removes an element; text and comment changes are ignored.

The upstream Select and Collapsible implementations and dependencies remain
available for future JavaScript-enhanced widgets. Use the native variants for
discovery's progressive-enhancement contract. New components must receive the
same theme and behavior checks before shipping.

## Verification

Run the build, race tests, and vet above. The focused rendering regressions in
`views/home/components_test.go` protect link sanitization and native form semantics.
For browser changes, check desktop, intermediate rail, mobile, and 320px layouts;
category resets; preview loading/error/retry/close; event-section navigation;
htmx pagination; keyboard focus; and search/pagination with JavaScript disabled.

For isolated shared-CSS regressions, run `go run ./scripts/css-preview` in one
terminal, then `PLAYWRIGHT_MODULE=/path/to/playwright node scripts/css-browser.cjs`
in another. The read-only server binds to localhost:18088, renders production
templates with long-content fixtures, and reads built assets from `views/assets`.
It needs no provider, database, account session, or credentials. Playwright is an
optional external test tool, not a shipping dependency. Set `CSS_SCREENSHOT_DIR`
to capture the batched desktop/rail/mobile/320px/enlarged-text layouts. Checks also
cover input floors, clipping/sticky scrolling, hover/press, reduced motion,
forced-colors focus, selectable links, bounded textareas, RTL, and native filters,
plus the floating navigation, the sticky header's wordmark step (including 200% text holding its scroll position), warm muted-text contrast,
and pagination placeholders (`/?more=true` renders a Load more action).
Set `CSS_BROWSER=webkit` and, if needed, `CSS_BROWSER_EXECUTABLE` to run the same
checks in WebKit; the default is Chromium.

For the event-first redesign, run `scripts/event-context-browser.cjs` against the
same read-only server. Set `BROWSER_ENGINE=webkit` for its WebKit pass, and
`AXE_SCRIPT=/path/to/axe.min.js` for Chromium accessibility checks. It covers
Overview, ticket access, named sections, long content, compact/desktop behavior,
confirmed save/reminder presentation, provider/account failures, and native links.
For real account writes and collection return continuity, use the isolated MariaDB
browser harnesses in [saved events](saved-events.md#verification),
[discussions](event-discussions.md#verification), [interest](event-interest.md),
[recommendations](event-recommendations.md#verification), and [Radar](radar.md).

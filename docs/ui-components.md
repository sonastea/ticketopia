# UI components

Ticketopia uses [shadcn-templ](https://shadcn-templ.com/docs/installation), with
the CLI pinned to **v2.0.0-beta.10** in `go.mod`. Component source is owned by this
repository under `views/components`; `components.json` configures installation
paths, the Nova base style, and the embedded script bundle.

## Build and develop

Use Go 1.27+ and Node.js 24 with npm. The Node release line is declared in
`package.json` and `.nvmrc`; with nvm, run `nvm install` and `nvm use` first:

```sh
npm ci
npm run build
TICKETMASTER_KEY=your-api-key ./bin/ticketopia
```

The build bundles component JavaScript, generates templ Go code, compiles Tailwind
CSS 4.1.18, and builds the Go binary. The binary embeds all browser assets and can
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
`@components.Scripts()` is included once in `views/layouts/base.templ`.
The script bundle is served at `/assets/js/shadcn-templ-<hash>.js`.

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
  Event-section links also wrap, and pagination action minimum widths are capped
  at the available width, rather than overflowing when text is enlarged.
  Filter-summary metadata can shrink and break long words without pushing its
  disclosure icon outside the control.

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
uses real outlines, including forced-colors mode. Geometry transitions opt into
`prefers-reduced-motion: no-preference`; vendored animations also respect the
existing reduced-motion safeguard.

Use parent-owned gaps and shrink-safe tracks. Keep behavioral shell thresholds
at 62rem/72rem synchronized with JavaScript. Heading sizes adapt with bounded,
rem-based `clamp()` tokens, while fields remain at least 16px to avoid iOS focus
zoom without disabling user zoom. Text wraps without global min-content-shrinking
`anywhere`; dates, prices, counts, and changing numeric facts use tabular figures.
Image wrappers reserve aspect ratios and clip without creating scroll containers.
Actual sidebar/preview scroll areas retain `overflow-y: auto`, stable gutters,
and contained overscroll. Textareas grow from 8rem to 24rem with `field-sizing`
where supported, retaining native scrolling and manual resizing otherwise.

Modern features follow the existing Tailwind v4 browser baseline (Chrome 111+,
Safari 16.4+, Firefox 128+). `light-dark()` requires Chrome 123+/Safari 17.5+;
older browsers retain explicit light-token fallbacks, without library dark-mode
opt-in. Pretty wrapping, intrinsic-size animation, and textarea auto-sizing
progressively enhance browsers that support them.

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
forced-colors focus, selectable links, bounded textareas, RTL, and native filters.
Set `CSS_BROWSER=webkit` and, if needed, `CSS_BROWSER_EXECUTABLE` to run the same
checks in WebKit; the default is Chromium.

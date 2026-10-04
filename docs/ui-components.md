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
Ticketopia's existing cobalt, apricot, paper, and ink palette. Manrope remains
self-hosted. Page composition, event rows, imagery, date indexing, breakpoints,
and route semantics remain app-owned; see [DESIGN.md](../DESIGN.md).

- Buttons cover actions, navigation items, section links, ticket links, pagination,
  disabled participation controls, and preview recovery.
- Input and Label cover visible fields. Hidden form metadata remains native HTML.
- Badge covers event sale statuses and unavailable-feature labels.
- Card, Alert, Empty, and Skeleton cover context, tickets, feedback, and loading.
- The library's Lucide icons replace the hand-authored SVG switch.
- JavaScript preview states clone server-rendered component templates, keeping
  initial pages, fetched fragments, loading, errors, and retry consistent.

Ordinary headings, content links, semantic landmarks, lists, and provider images
remain HTML; they are not interactive widget substitutes.

## Local adaptations

Copied component source is intentionally editable. Preserve these changes when
updating components, and review the registry diff before using `--overwrite`:

- `button/button.templ` uses `templ.URL` rather than `templ.SafeURL` so provider
  and caller-supplied links retain protocol sanitization.
- `select/native.templ` offers a themed native `<select>`. Category and genre
  remain keyboard-accessible ordinary form controls; changing category replaces
  compatible genre options, and both work without JavaScript.
- `collapsible/native.templ` offers native `<details>/<summary>` using the shared
  props. Browser form validation and Change city can reveal the filters, including
  without the library's JavaScript behavior.
- The theme preserves 44px actions, visible outline focus, flat border-only depth,
  readable disabled states, reduced motion, and the existing responsive shell.

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

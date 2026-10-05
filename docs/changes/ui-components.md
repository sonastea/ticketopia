# UI component changes

## Unreleased

### 2026-10-04 — Sidebar identity, preferences, and build toolchain

- Replace cobalt navigation with a plum/light-stone identity, pale-plum selection,
  dusty-rose date indexing, and consistent themed buttons, links, focus, and fallbacks.
- Harmonize date-marker paper with the plum/stone palette using a low-chroma
  rose tint instead of apricot; retain readable ink, date geometry, and behavior.
  Verify the refinement with a production build, 17 targeted browser checks,
  four zero-violation automated accessibility views, and a scoped **ship** review.
- Separate the pale semantic colors with more visible plum selection, neutral
  stone hover, and warmer dusty-rose date paper, retaining the primary brand and
  reading surfaces. Keep loading text plum on pale plum; see the
  [palette roles and contrast](../../DESIGN.md#colors).
  Use a one-step-lighter selection wash to keep muted text above WCAG AA.
  Verify the final palette with 35 targeted Chromium checks, seven zero-violation
  automated accessibility views, production assets, race tests, and vet.
- Add grouped desktop navigation, an explicit collapse/expand rail, and an editing
  mode for drag or keyboard reordering of sections and their destinations.
- Remember committed preferences in this browser, with reversible edits, safe
  storage fallback, and unchanged compact navigation and discovery behavior; see
  the [sidebar interaction guide](../design-guidelines.md#sidebar-preferences)
  and [visual system](../../DESIGN.md).
- Verify 68 browser checks, 11 zero-violation automated accessibility views,
  native drag and keyboard edits, storage failure/corruption, compact reflow,
  no-JavaScript search, production assets, race tests, and vet. Keep the empty
  bounded event preview keyboard-scrollable on short desktop screens.
- Confirm dependent genre resets and htmx/no-JavaScript pagination in five
  additional discovery smoke checks.
- Receive **ship** from an independent read-only finish review at the approved
  scope, with advisory-only detector findings and no material changes requested.
- Refresh `DESIGN.md` and its schema-v2 sidecar from shipped tokens and components,
  validating source colors, references, and documentation previews.

- Use Go 1.27 and Node.js 24 for asset/application builds; declare Node 24 in npm
  metadata and `.nvmrc`, and align the [component guide](../ui-components.md) with
  the [container build](../deployment.md).
- Verify the full Docker asset build, served UI/assets, Go race tests, and vet
  with the updated toolchains.

### 2026-10-03 — shadcn-templ migration

- Install and pin shadcn-templ v2.0.0-beta.10; vendor the UI component source,
  utilities, icons, and script dependencies with a single embedded bundle.
- Replace existing controls, status labels, context/ticket containers, feedback,
  and loading elements with themed components while preserving Ticketopia's
  identity, responsive layouts, native GET forms, and disclosure behavior.
- Render client-side preview states from shared templ components, and preserve
  URL sanitization in component links.
- Upgrade to pinned Tailwind v4 tooling with a reproducible npm build and Air
  integration. Keep generated assets checked in for direct Go execution.
- Document installation, theme mappings, and update-sensitive adaptations in the
  [component guide](../ui-components.md).
- Verify production builds, race tests, vet, native form/link regressions, live
  category resets, previews/retry, htmx and no-JavaScript pagination, and six
  responsive accessibility views with zero final violations or JavaScript errors.

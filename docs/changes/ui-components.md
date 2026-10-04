# UI component changes

## Unreleased

### 2026-10-04 — Build toolchain alignment

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

# Ticketopia repository guidance

## Scope and autonomy

- Preserve existing architecture, Go/package conventions, API and persistence
  boundaries, and unrelated working-tree changes. Avoid unrelated cleanup.
- Proceed without approval for task-scoped, reversible work: relevant reads and
  edits, formatting, generation of committed artifacts, local tests, builds,
  static checks, and Git status/diff inspection. Choose routine details locally.
- Ask only for unresolved decisions that materially affect the outcome, scope
  expansion, or destructive/irreversible actions, production/shared-service changes,
  or credential changes. Do not commit, push, or deploy without authorization.
- Delegate only when the user or applicable instructions explicitly request it.

## Targeted context

- Search affected source and guidance, not the whole repository. Implement once
  there is enough context for a safe change; reread only changed files or details
  that need rechecking.
- Load documentation only as relevant; `docs/README.md` is the feature-guide index:
  - `PRODUCT.md`: product behavior, requirements, and scope.
  - `DESIGN.md`: UI/design decisions; `docs/ui-components.md`: component conventions,
    local adaptations, and frontend build workflows.
  - `docs/persistence.md`: MariaDB, schema, migrations, and durable state.
  - `docs/openapi.md`: API contracts; keep the hand-maintained source
    `internal/api/openapi.yaml` aligned with handler/model changes.
  - `docs/deployment.md`: deployment/infrastructure work only.
- Consult goals, ideas, and proposed designs only for relevant planned requirements;
  distinguish plans from implemented behavior. Read `docs/changes/` and archives
  only when history matters, using targeted searches rather than bulk reads.

## Source and generated output

- Ticketopia commits generated artifacts. Work from source; normally do not read,
  manually edit, or reason from generated output when its source is available.
  Regenerate affected artifacts with source changes and retain them in the diff.
- Generated output includes `**/*_templ.go`, generated icon data/definitions,
  script bundles/manifests, compiled CSS, and generated asset embeds. Embedding
  alone does not make a source file generated (for example, `openapi.yaml`).
- For templ UI changes, edit `.templ` and run the pinned `go tool templ generate`
  as needed; never manually modify `*_templ.go`. Copied shadcn-templ component
  source is editable; preserve its documented local adaptations.
- Use `npm run generate` for component script bundling plus templ generation,
  `npm run css` when Tailwind inputs change (including template classes), and
  `npm run build` when a complete embedded-asset/binary build is needed.
- Inspect generated output only to debug generation, validate generated behavior,
  investigate a failure pointing into it, or when no source representation exists.

## Modern CSS

- Follow [Good CSS](https://good-css.com/skills/good-css/SKILL.md) when writing or
  reviewing CSS, Tailwind classes, or inline styles. Read only its task-relevant
  reference files; the local conventions below keep routine work self-contained.
- Prefer an intrinsic layout or one adaptive declaration over breakpoint ladders,
  extra wrappers, or JavaScript. Use Grid/Flexbox, `gap`, auto margins, intrinsic
  grids with shrink-safe minima, and container queries for reusable components.
  Preserve the shell's shared CSS/JavaScript thresholds and intentional layout
  changes; do not replace behavior breakpoints with fluid math.
- Use logical sizes, spacing, borders, alignment, and insets (`inline`/`block`,
  `start`/`end`), including Tailwind logical utilities. Keep physical directions
  only for genuinely physical geometry, safe-area environment variables, and
  unchanged vendored dependencies.
- Define palette colors in `oklch()`; achromatic colors use hue `none`. Derive
  hover/pressed shades, tints, and transparency with `color-mix(in oklch, ...)`.
  Preserve documented semantic palette roles and provider-brand exceptions.
  Keep light/dark pairs in one `light-dark()` token set and switch `color-scheme`,
  not token definitions. Ticketopia stays light by default; do not add automatic
  dark mode or a theme toggle without a product requirement.
- Tokenize fluid sizes with `clamp()` using rem bounds and a rem + viewport (or
  container) preferred value. Font maxima must not exceed 2.5 times their minima.
  Keep the existing role-based type hierarchy rather than inventing a new scale.
- Keep the defensive reset: shrinkable flex/grid items, inherited word wrapping,
  balanced headings, stable scrollbar gutter, and correct viewport units. Give
  fixed-size media/icons `flex: none`, preserve image aspect-ratio wrappers, and
  use tabular numbers for prices, dates, and counts, not all prose.
- Use `overflow: clip` for clipping only; retain `auto`/`hidden` for actual or
  programmatic scrolling and resizable controls. Prefer root `break-word` over
  global `anywhere`; never truncate essential text or disable browser zoom.
- Gate every hover rule behind `(hover: hover) and (pointer: fine)`, including
  Tailwind variants. Provide non-motion `:active` feedback, visible outline-based
  `:focus-visible` states, and 44px action targets. Never remove focus outlines;
  preserve forced-colors support and text selection on content and links.
- Name transition properties, use the shared easing tokens, and never use `all`
  or `ease-in`. Opt into movement/scaling only under
  `prefers-reduced-motion: no-preference`; unsupported enhancements must leave
  native forms, disclosure, scrolling, and navigation usable.
- Edit app-owned CSS in `views/styles/app.css`; keep vendored style utilities
  intact and adapt copied components deliberately. Rebuild compiled CSS and
  verify compact/desktop layouts, long content, enlarged text, touch feedback,
  keyboard/forced-colors focus, and reduced motion after shared styling changes.

## Validation and completion

- Use the smallest useful check during implementation: affected Go package/tests
  for localized changes; relevant generation and rendering checks for templ work.
  Keep feature-specific regression/integration checks for affected behavior.
- Broaden validation when shared behavior or package boundaries change. Use the
  full suite (`go test -race ./...`) and `go vet ./...` when appropriate for final
  validation of substantial changes, not after every small edit.
- Before committing changes to Docker/build inputs, asset generation, or tests
  that read repository files, run `make check` after the final relevant edits.
  It validates the working tree with the publishing Docker target, including
  `.dockerignore` filtering; host-only tests cannot catch omitted context files.
- Add tests for meaningful behavioral risks, not to mirror implementation or
  validate prose-only/trivial edits. Do not rerun successful checks unless relevant
  changes, failures, or unresolved concerns justify it.
- Review the final diff for scope and unintended changes, including which generated
  artifacts changed. Complete the requested work and relevant documentation; report
  changes, checks run, and skipped checks or blockers concisely and accurately.

## Documentation updates

- For notable feature changes, update the relevant guide and
  `docs/changes/<feature>.md`. Follow `docs/README.md#documenting-future-changes`
  for dated **Unreleased** entries, guide links, the 20-feature changelog index,
  and 100-line history/archive limits, naming, and date/release preservation.
- Update the root README goal checklist when delivering milestones; check items
  only when their user-facing behavior is implemented and verified.

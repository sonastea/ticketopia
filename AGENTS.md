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

## Validation and completion

- Use the smallest useful check during implementation: affected Go package/tests
  for localized changes; relevant generation and rendering checks for templ work.
  Keep feature-specific regression/integration checks for affected behavior.
- Broaden validation when shared behavior or package boundaries change. Use the
  full suite (`go test -race ./...`) and `go vet ./...` when appropriate for final
  validation of substantial changes, not after every small edit.
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

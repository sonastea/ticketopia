# CI performance changes

## 2026-11-20 — Unreleased

- Drop the GitHub Actions layer cache: it held no Go modules or build cache and
  re-uploaded per-commit layers each publish. The retained runner builder stays
  the only cache; a replaced runner builds cold.
- Cut race-suite time without fewer tests or weaker assertions: MCP servers share
  resolved tool schemas (SDK `SchemaCache`), API tests against a local fake
  provider shorten the discovery request interval (production keeps 250 ms), and
  the two CPU-only soak-analysis tests run in parallel. Local `make check`
  race/vet stage 87.7 s → 67.0 s. See the [follow-up](../ci-performance.md#follow-up-2026-11-20).

## 2026-11-16 — Unreleased

- Audit available Actions history and preserve per-runner BuildKit cache mounts.
  Use a fresh validation-run argument instead of a no-cache filter that also
  clears Go mounts; import/export remote layers once per publishing job. Keep full
  race tests, vet, static binaries, asset generation and privacy checks unchanged.
  See the [measurement report and merge-safety limitations](../ci-performance.md).
- Preserve pending main/tag runs with GitHub's queued concurrency mode instead
  of replacing the previous pending run; retain serialized publishing.
- PR validation and required-check enforcement remain owner-controlled blockers;
  no runner migration, protection change, publication or deployment was performed.

# CI performance changes

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

# Container deployment changes

## 2026-10-05 — Unreleased

- Document the planned `mariadb-operator` deployment for shared MariaDB persistence
  across three stateless application replicas, with independent database topology,
  primary routing, Secrets/TLS, migration sequencing, pool budgets, and recovery
  checks; see [planned MariaDB persistence](../deployment.md#planned-mariadb-persistence).
- Explicitly distinguish this direction from implemented container/probe behavior:
  no database runtime, migration command, or operator manifests are delivered yet.

## 2026-10-04 — Unreleased

- Added a multi-stage, non-root distroless container build with the full frontend
  asset pipeline, a build-input allowlist, and a bundled Go health-check executable
  at `/healthcheck`, built from root-level `healthcheck.go` and targeting the
  internal `/healthz` endpoint for Coolify/Docker process liveness.
- Align Docker builders and local toolchain declarations with Go 1.27 and Node.js
  24; keep npm dependencies pinned and document the same versions for development.
- Added dependency-free liveness/readiness endpoints, shutdown-aware readiness,
  an OpenAPI description, and SIGTERM handling for graceful container termination.
- Include runtime `SOURCE_COMMIT` in the structured `source_commit` startup log
  field, with an `unknown` fallback and no commit-dependent Docker build inputs.
- Added regression tests for probe responses, shutdown, checker failures, redirects,
  and deadlines; documented Docker/Coolify setup and native Kubernetes liveness
  (`/healthz`) and readiness (`/readyz`) probes without localhost-dependent behavior.
- Verified the Docker build, race tests, vet, and container smoke checks: embedded
  assets, checker exit statuses, Docker health, non-root/read-only operation,
  runtime certificates/timezones, and SIGTERM shutdown with exit code 0.
- Verified two runtime revisions using the same image, whitespace trimming, and
  missing/blank revision fallbacks, with readiness and graceful shutdown in each case.
- See the [container deployment guide](../deployment.md) for configuration and
  verification.

# Container deployment changes

## 2026-10-04 — Unreleased

- Added a multi-stage, non-root distroless container build with the full frontend
  asset pipeline, a build-input allowlist, and a bundled Go health-check executable
  at `/healthcheck`, built from root-level `healthcheck.go` and targeting the
  internal `/healthz` endpoint for Coolify/Docker process liveness.
- Align Docker builders and local toolchain declarations with Go 1.27 and Node.js
  24; keep npm dependencies pinned and document the same versions for development.
- Added dependency-free liveness/readiness endpoints, shutdown-aware readiness,
  an OpenAPI description, and SIGTERM handling for graceful container termination.
- Added regression tests for probe responses, shutdown, checker failures, redirects,
  and deadlines; documented Docker/Coolify setup and native Kubernetes liveness
  (`/healthz`) and readiness (`/readyz`) probes without localhost-dependent behavior.
- Verified the Docker build, race tests, vet, and container smoke checks: embedded
  assets, checker exit statuses, Docker health, non-root/read-only operation,
  runtime certificates/timezones, and SIGTERM shutdown with exit code 0.
- See the [container deployment guide](../deployment.md) for configuration and
  verification.

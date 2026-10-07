# Container deployment changes

## 2026-10-06 — Unreleased

- Add a root `.env.example` for all implemented features, with required provider,
  MariaDB/TLS, and Google OAuth placeholders, safe proxy/cache defaults, optional
  tuning, and migration-only credentials kept out of runtime injection. Document
  secret handling and deployment prerequisites in the [environment setup](../deployment.md#environment-template).
- Document optional runtime-only Google credentials, exact HTTPS callback,
  separately managed Kubernetes auth configuration, and the schema-v1 drain
  required before the account migration; see [deployment](../deployment.md#optional-google-accounts).
- Extend operator runtime grants for account/auth tables without enabling auth,
  broadening credential privileges, or claiming a cluster rollout.
- Update local/operator database images to MariaDB **13.0.2** stable, with warnings
  against treating a major-version image change as a verified existing-volume upgrade;
  see [server upgrades](../persistence.md#server-version-upgrades).

## 2026-10-05 — Unreleased

- Use `edge` with `imagePullPolicy: Always` in operator application/migration
  manifests, with private GHCR pull-secret references and amd64 scheduling.
  Document production-style staging, explicit rollout, and manual fix/rollback;
  cluster rollout and production readiness remain unverified. See the
  [rollout guide](../deployment.md#production-style-staging-rollout).
- Run container build tools natively and cross-compile target Go binaries, enabling
  amd64 images from ARM builders without emulated Go toolchain crashes. Document
  private GHCR publishing, digest pinning, and separate Kubernetes pull credentials.
- Publish the tested amd64 MariaDB-foundation image to private GHCR; verify non-root
  migrations/outage recovery, authenticated pulling, and denied anonymous access.
  Record its immutable reference in the [deployment guide](../deployment.md#amd64-images-and-private-github-container-registry).
- Add the clearer app tag `test-amd64-20261005` for the same tested image. Add a
  SHA-pinned self-hosted Linux/x64 workflow using Go 1.27 and Node 24 validation,
  repository-token GHCR publishing, `edge`/commit/release tags, and fail-closed
  private-package checks. Document runner/package access setup.
  See [CI setup](../deployment.md#github-actions-on-a-self-hosted-runner).
- Resolve disabled CGO, missing GCC, and password-required sudo failures by moving
  asset builds and Go validation into the Dockerfile's Go/Node builder. Its separate
  `validate` target enables CGO and uses included GCC/headers for race tests/vet,
  with fresh tests on every CI run and no host package installation. Preserve the
  full test suite by allowing root health-check tests into the build context, and
  retain the CGO-disabled, compiler-free runtime image. Document validation in
  [CI setup](../deployment.md#github-actions-on-a-self-hosted-runner).
  Verify local container race tests/vet, workflow lint, and the amd64 runtime build.
  Successful end-to-end GitHub Actions publication with this fix remains unverified.
- Simplify publishing by initializing isolated Docker configuration in the preflight and using Buildx initialization,
  removing the redundant preparation step and default Docker build inputs.
- Document and supply standalone `mariadb-operator` examples for shared MariaDB persistence
  across three stateless application replicas, with independent database topology,
  primary routing, Secrets/TLS, migration sequencing, pool budgets, and recovery
  checks; see [planned MariaDB persistence](../deployment.md#planned-mariadb-persistence).
- Add shell-free migration execution in the existing non-root image and bounded
  SQL readiness when persistence is enabled, preserving dependency-free liveness
  and the disabled discovery mode. Drain HTTP before SQL pool cleanup.
- Add local TLS MariaDB setup and image smoke checks for explicit migration,
  outage/recovery probes, app restart, redaction, and graceful shutdown; see the
  [foundation guide](../persistence.md). Operator examples have not been deployed;
  three-pod operation, database HA, and backup/restore remain unverified.

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

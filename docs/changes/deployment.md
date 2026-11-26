# Container deployment changes

[Older history](archive/deployment/).

## 2026-11-26 — Unreleased

- `AGENT_PUBLISHES_PER_HOUR` (default 20, 0–10000) sets the per-account limit
  on public actions by agents and API tokens; `0` lifts it for a test window.
  See [deployment](../deployment.md).
- Verified automated GHCR publishing from the self-hosted runner: the
  `publish-image.yml` workflow pushes the private image, and production deploys
  from it. Shared state across app replicas, backup restoration and failover
  move to a later high-availability goal; see the
  [delivery checklist](../goals/delivery.md#current-focus-use-refine-and-verify).

## 2026-11-11 — Unreleased

- Add [backups and restore checks](../backups.md): Coolify's daily MariaDB
  backups to Cloudflare R2 (14 kept), and `scripts/backup/verify-restore.sh`
  (`make backup-verify`), which restores the newest backup into a disposable
  MariaDB, checks freshness, schema, tables, accounts and table integrity, and
  reports to an Uptime Kuma Push monitor (or a healthchecks.io-style URL) so a
  failed or missing check alerts.

## 2026-11-10 — Unreleased

- Add one [resource profile](../deployment.md#resource-profile) for Coolify,
  Kubernetes and local Compose: the app at 1 CPU and 256 MiB with
  `GOMEMLIMIT=200MiB`, MariaDB at 2 CPUs and 2 GiB (3 GiB in the three-replica
  Kubernetes example) with a 1 GiB buffer pool, small per-session buffers and
  16 MiB temporary tables, and Redis at 0.5 CPU and 256 MiB evicting least
  recently used keys. Apply it to the Compose service and operator examples.

## 2026-10-08 — Unreleased

- Complete schema-11 Discover operator grants and check parity in CI; provide
  [secure Secret provisioning and a Cilium/Gateway handoff prompt](../kubernetes-bootstrap.md)
  without applying cluster changes or claiming external routing/TLS verification.

- Fix container validation after the saved-events workflow tests began reading
  sources omitted by the build-context allowlist. Include the root `Makefile`,
  local MariaDB setup wrapper, and runtime-check SQL without sending local
  configuration or credentials or changing the runtime image. See
  [build and validation inputs](../deployment.md#build-and-run).
- Reproduce the missing `Makefile` failure in Docker and verify container asset
  generation, binary builds, race tests, and vet on clean committed sources plus
  the allowlist fix. Database-backed integration checks remain skipped.
- Add local `make check` and repository guidance to validate the current working
  tree before committing relevant changes, catching build-context regressions
  with the publishing checks without another workflow or an installed Git hook.
  See [local validation](../deployment.md#build-and-run).

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

# Container deployment

Ticketopia ships a multi-stage [Dockerfile](../Dockerfile). The builder uses Go
1.27 and Node.js 24 to install pinned npm dependencies, bundle component scripts,
generate templ code, compile Tailwind CSS, and build both Go executables. Generated
assets are embedded, so the runtime does not need the source tree or Node.js.
Build tools run on the builder's native architecture; Go binaries are cross-compiled
for the requested target. This avoids emulating Go/Node during amd64 builds on ARM hosts.

The Docker tags track patch updates within these release lines. Local development
uses the same versions through `go.mod`, the npm engine declaration, and `.nvmrc`;
see the [development setup](../README.md#development).

The final distroless image contains CA certificates, timezone data, the application,
and its health-check executable. It runs as non-root UID/GID **65532**, listens on
**8080**, and has no shell, curl, or package manager. The checker is installed at
**`/healthcheck`** in the application container, not in a separate health-check
container. Its source is root-level [healthcheck.go](../healthcheck.go), and it
probes the internal **`/healthz`** HTTP endpoint by default.

## Build and run

Docker with BuildKit is required for build cache mounts:

```sh
docker build -t ticketopia:local .
export TICKETMASTER_KEY=your-api-key
docker run --rm --name ticketopia -p 8080:8080 -e TICKETMASTER_KEY ticketopia:local
```

To build assets and run Go race tests/vet with the container toolchain (including
GCC and C headers), without installing development tools on the host:

```sh
docker buildx build --target validate --no-cache-filter validate .
```

The validation target runs on the builder's native architecture. The final runtime
stage remains separate: normal image builds do not run tests or ship validation tools.

Open <http://localhost:8080>. The [.dockerignore](../.dockerignore) allowlists build
inputs and excludes local configuration, databases, dependencies, and binaries.
Secrets are not build arguments and `.env` is not baked into the image. Supply
environment variables at runtime; for local testing, `--env-file .env` is also
supported. Use the deployment platform's secret storage in production.

### amd64 images and private GitHub Container Registry

Authenticate locally with `docker login ghcr.io -u YOUR_GITHUB_USERNAME`, using a
classic PAT with `write:packages` for publishing. Never put the token in build args,
the repository, or image layers. Confirm an existing package is **private** before
pushing; new GHCR packages default to private, independently of repository visibility.

```sh
docker buildx build --platform linux/amd64 --load \
  --label org.opencontainers.image.source=https://github.com/sonastea/ticketopia \
  -t ghcr.io/sonastea/ticketopia:YOUR_TAG .
docker push ghcr.io/sonastea/ticketopia:YOUR_TAG
```

Verify package visibility and that anonymous pulls are denied after publishing.
Use the resulting digest for both application and migration Job images. For
Kubernetes pulls, create a namespace-local registry Secret with a separate,
pull-only classic PAT (`read:packages`) and reference it in **both** Pod specs:

```yaml
imagePullSecrets:
  - name: ghcr
```

Do not reuse a publishing/admin token as a cluster pull credential. The
[publishing workflow](#github-actions-on-a-self-hosted-runner) uses the repository's
`GITHUB_TOKEN` instead of a stored publishing PAT. Image publication does not deploy
the app or run database migrations.

Verified test publication (2026-10-05), built from the working tree rather than a
committed release: **`ghcr.io/sonastea/ticketopia:test-amd64-20261005`**.
This is the **Ticketopia app**, including its migration subcommand, not the MariaDB
server. The earlier `mariadb-foundation-amd64-20261005` tag remains a legacy alias
of the same image; the clearer tag was added without rebuilding or deleting it.
The package is private, its runtime platform is `linux/amd64`, authenticated pulls
succeed, and anonymous pull-token requests are denied. Immutable reference:

```text
ghcr.io/sonastea/ticketopia@sha256:4ce89d6fe7cfb4e774134d5a5eaefbe72a23034b2df386f7e3d4d9bb07e92664
```

### GitHub Actions on a self-hosted runner

[`.github/workflows/publish-image.yml`](../.github/workflows/publish-image.yml)
validates and publishes the app on pushes to `main`, pushes of `v*` Git tags, and
manual runs on those same refs. It does **not** run on pull requests or manually
selected feature branches. Only trusted source belongs on this runner; protect
`main` and release tags, and use a dedicated, preferably ephemeral runner without
production secrets. Docker daemon access is effectively root access to its host.

Before the first run:

1. Register a **Linux amd64** self-hosted runner for this repository under
   **Settings → Actions → Runners**. Its labels must include `self-hosted`, `linux`,
   and `x64`. Keep its runner software current (at least **2.327.1** for Node 24
   actions); Ubuntu 24.04 is a suitable host.
2. Install Git, Bash, CA certificates, tar/unzip, and
   Docker with the Buildx plugin. The runner account must be able to run Docker
   without interactive `sudo`. Allow outbound access to GitHub/GHCR, Go/npm
   downloads, and the Dockerfile's base registries. Go, Node, GCC, and C development
   headers need not be installed on the host: asset generation and validation use
   the Dockerfile's **Go 1.27 Bookworm** builder with **Node 24**, which includes GCC
   and C headers. The workflow does not run `apt-get` or require passwordless `sudo`
   for package installation.
3. In the [private package settings](https://github.com/users/sonastea/packages/container/package/ticketopia),
   open **Manage Actions access**, add `sonastea/ticketopia`, and grant **Write**.
   This package was first published locally, so repository-token access must be
   granted explicitly. Keep package visibility **private**, independently of the
   public source repository; do not enable public package access. The workflow
   fails before building if the token cannot see the private package.
4. Commit and push the workflow and intended app changes when ready. A run builds
   the **committed checkout**, not this machine's uncommitted working tree. No
   custom GHCR publishing secret is needed; the job requests `packages: write` for
   its short-lived `GITHUB_TOKEN`. Repository/organization policy must permit it.

The job builds embedded assets and Go binaries with the Dockerfile's asset pipeline,
then runs `go test -race -count=1 ./...` and `go vet ./...` in its `validate` target
before publishing `linux/amd64`. That target sets `CGO_ENABLED=1` and `CC=gcc` for
the race detector, independently of runner defaults. Buildx bypasses the validation
layer cache on each run, while reusing dependency and build layers. The runtime
image still uses CGO-disabled binaries and contains no compiler or test tools.
Real MariaDB integration tests are skipped without `MARIADB_TEST_ADDR`; this
workflow does not provision a database or pass database credentials into the
builder, nor does it run the local outage smoke script. Buildx uses GitHub Actions
layer caching, a job-specific temporary Docker config, and automatic registry
logout. Buildx handles Docker initialization; runner labels select Linux/x64.
Actions are pinned to reviewed commit SHAs. Package privacy and denied
anonymous pulls are checked after publication; the run summary records the
immutable image digest.

| Trigger | Published app tags |
| --- | --- |
| Push/manual run on `main` | `edge`, `sha-<full-commit-sha>` |
| Push/manual run on a `v*` tag | The Git tag (for example `v1.0.0`), `sha-<full-commit-sha>` |

Non-Docker-safe characters in Git tags are normalized by the metadata action;
use conventional version tags. `edge` tracks main and is not a stable release.
No `latest` tag is generated. Deploy by the digest in the run summary, using the
same digest for the app and its migration Job. Supply the matching Git commit as
runtime `SOURCE_COMMIT` if desired; CI labels do not set that environment variable.

GitHub runs exposed disabled CGO, missing GCC, and password-required `sudo` during
host validation. Validation now uses the container's compiler with CGO enabled;
local container race tests/vet and the amd64 runtime build passed on 2026-10-05.
A successful end-to-end GitHub Actions publication with this fix remains unverified.

The default cache is in memory. Add `-e KV_URL` after exporting a shared backend
URL, or configure it in the deployment platform. See [cache configuration](cache.md)
and [trusted reverse-proxy configuration](location.md#configuration). Within Docker
or Kubernetes, a separate Redis service needs its service hostname, not localhost.

## Source revision in startup logs

The `API started...` log includes a structured `source_commit` field read from
the runtime **`SOURCE_COMMIT`** environment variable. The full value is preserved
after trimming surrounding whitespace; missing or blank values log `unknown`
without preventing startup.

Coolify supplies `SOURCE_COMMIT` at runtime. For a manual Docker run, pass the
revision matching the image being deployed:

```sh
export SOURCE_COMMIT=YOUR_IMAGE_SOURCE_COMMIT
docker run --rm -p 8080:8080 -e TICKETMASTER_KEY -e SOURCE_COMMIT ticketopia:local
```

For example, the startup log contains:

```json
{"level":"info","component":"api","source_commit":"0123456789abcdef0123456789abcdef01234567","message":"API started..."}
```

This is runtime metadata only: no build argument, embedded revision, or Docker
cache invalidation is introduced. It identifies the source revision reported by
the deployment environment, not the image digest; keep it aligned with the image
when supplying it manually. Local development can also set it in `.env`.

## Health and shutdown

| Endpoint | Behavior |
| --- | --- |
| `GET /healthz` | `200` with `ok` when the HTTP server responds. No dependency checks. |
| `GET /readyz` | `200` with `ok` after initialization; `503` during shutdown or, with persistence enabled, unavailable/dirty/unsupported MariaDB (bounded check). |

Both responses are plain text with `Cache-Control: no-store`. Startup completes
before the server begins listening. On SIGTERM or SIGINT, readiness becomes false
and the server stops accepting connections, with up to **five seconds** to finish
in-flight requests. Allow a longer container termination grace period (Docker's
default stop timeout and Kubernetes's default 30 seconds are sufficient).

Probes never call Ticketmaster, geolocation, or the cache. Liveness never calls SQL;
enabled readiness uses `DB_READY_TIMEOUT` (default 500ms). An external API outage
should not cause restart loops, and a shared-cache failure can use the local
fallback. These probes indicate process health, not guaranteed event availability.

Liveness answers whether the process responds; readiness answers whether the
initialized application can accept traffic. Explicit MariaDB mode gates readiness
on the database/schema; disabled mode remains database-free. Optional caches and
external providers do not gate readiness.
Use distinct endpoints rather than changing their meaning for localhost requests.
Loopback requests do not inherently bypass authentication or rate limiting; if
middleware is added later, explicitly exempt these lightweight probe endpoints.

The Dockerfile checks liveness every 30 seconds, with a three-second execution
timeout, a ten-second start period, and three failures before marking unhealthy.
The checker has its own two-second request deadline, rejects redirects/non-200
statuses, and exits `0` on success or `1` on failure with a diagnostic on stderr:

```sh
docker exec ticketopia /healthcheck
docker exec ticketopia /healthcheck -url http://127.0.0.1:8080/readyz
```

Docker health status alone does not restart an unhealthy container; recovery is
the deployment platform's responsibility.

## Coolify

- Select the **Dockerfile** build strategy, with the repository root as context
  and the root `Dockerfile` as the build file.
- Set the application port to **8080** and configure runtime secrets/environment
  variables. No custom build or startup command is needed; the image supplies them.
- Use the image's health check. If configuring a command-based check in Coolify,
  use **`/healthcheck`**. It needs no shell operators. For an HTTP-based check,
  select **GET `/healthz` on port 8080**. For deployment readiness instead of
  process liveness, use `/healthcheck -url http://127.0.0.1:8080/readyz` or HTTP
  GET `/readyz`.
- Remove the old startup command that installed curl: the runtime has no `apt-get`
  or shell, and neither is necessary.

## Kubernetes

Kubernetes does **not** use Dockerfile `HEALTHCHECK` instructions. Configure native
HTTP probes instead; the kubelet calls the pod directly, without running curl or
the checker. The bundled checker remains useful for Docker and manual diagnostics.

This fragment belongs in the Pod spec (a Deployment's `spec.template.spec`):

```yaml
terminationGracePeriodSeconds: 30
containers:
  - name: ticketopia
    image: YOUR_REGISTRY/ticketopia:YOUR_TAG
    ports:
      - name: http
        containerPort: 8080
    startupProbe:
      httpGet:
        path: /healthz
        port: http
      periodSeconds: 2
      timeoutSeconds: 3
      failureThreshold: 30
    livenessProbe:
      httpGet:
        path: /healthz
        port: http
      periodSeconds: 10
      timeoutSeconds: 3
      failureThreshold: 3
    readinessProbe:
      httpGet:
        path: /readyz
        port: http
      periodSeconds: 5
      timeoutSeconds: 3
      failureThreshold: 3
```

Supply `TICKETMASTER_KEY` and any cache credentials through Kubernetes Secrets.
Startup probes protect initialization; liveness failures restart the container;
readiness failures remove it from Service traffic. The current image can run with
a read-only root filesystem, dropped capabilities, and no privilege escalation.

A shared cache does not coordinate external request budgets across replicas:
Ticketmaster and geolocation counters/deduplication are per process. Account for
that before scaling pods; see [discovery budgets](discovery.md#external-request-budget)
and [geolocation limits](location.md#ip-lookup-and-caching).

### Planned MariaDB persistence

Status: The **connection/migration/identity foundation is implemented**; deployed
multi-pod operation, broader persistence, HA, and backup/restore remain planned.
The [persistence guide](persistence.md) covers all configuration, local MariaDB,
failed-DDL recovery, rolling-update compatibility, pinned operator examples, and
connection budgeting. See the [database plan](design/database.md) for future state.

The existing non-root/shell-free image runs explicit migrations using its normal
entrypoint plus `migrate` arguments. It embeds the same migrations as development;
it requires migration credentials rather than runtime credentials. Never run this
subcommand from application startup or init containers on every pod.

[`deploy/mariadb/operator`](../deploy/mariadb/operator/) supplies a standalone
database, Database/User/Grant resources, Secret placeholders, enforced TLS/CA
mounts, migration Job, and three-application-replica example. All replicas share
one database endpoint and never mount database volumes. Budget steady, surge,
terminating, migration, and admin connections; the example budgets 92 of 100.

These examples were **not deployed to a cluster**. A single MariaDB pod is not HA;
three application pods do not change that. Before production availability claims,
select/test any required replication/Galera/MaxScale topology, storage/node placement,
failover, and pooled-connection/uncertain-write recovery. Independently configure
off-volume backups, retention, and successful restoration into a fresh instance;
PVCs/replicas are not backups. Scheduled ingestion and cross-pod external budgets
are later work, not provided by this foundation.

Verify writes through one app pod are visible through another and that app restarts,
rolling updates, and recovery do not lose data before checking the broader
[delivery milestones](goals/delivery.md#2-keep-reliable-event-and-application-history).

## Verification

Run `go test -race ./...`, `go vet ./...`, and a Docker build. Container smoke checks
should exercise `/healthz`, `/readyz`, the embedded UI/assets, the checker, non-root
operation, and SIGTERM shutdown. Probe tests protect dependency-free responses,
shutdown readiness, HTTP status/redirect failures, deadlines, and connection errors.

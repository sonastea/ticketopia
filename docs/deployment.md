# Container deployment

Ticketopia ships a multi-stage [Dockerfile](../Dockerfile). The builder uses Go
1.27 and Node.js 24 to install pinned npm dependencies, bundle component scripts,
generate templ code, compile Tailwind CSS, and build both Go executables. Generated
assets are embedded, so the runtime does not need the source tree or Node.js.

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

Open <http://localhost:8080>. The [.dockerignore](../.dockerignore) allowlists build
inputs and excludes local configuration, databases, dependencies, and binaries.
Secrets are not build arguments and `.env` is not baked into the image. Supply
environment variables at runtime; for local testing, `--env-file .env` is also
supported. Use the deployment platform's secret storage in production.

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
| `GET /readyz` | `200` with `ok` after initialization; `503` with `not ready` if shutdown has begun and the handler is still reachable. |

Both responses are plain text with `Cache-Control: no-store`. Startup completes
before the server begins listening. On SIGTERM or SIGINT, readiness becomes false
and the server stops accepting connections, with up to **five seconds** to finish
in-flight requests. Allow a longer container termination grace period (Docker's
default stop timeout and Kubernetes's default 30 seconds are sufficient).

Probes never call Ticketmaster, geolocation, or the cache. An external API outage
should not cause restart loops, and a shared-cache failure can use the local
fallback. These probes indicate process health, not guaranteed event availability.

Liveness answers whether the process responds; readiness answers whether the
initialized application can accept traffic. Required dependencies could be added
to readiness checks if future features cannot work without them; optional caches
and external providers should not gate readiness for the current application.
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

## Verification

Run `go test -race ./...`, `go vet ./...`, and a Docker build. Container smoke checks
should exercise `/healthz`, `/readyz`, the embedded UI/assets, the checker, non-root
operation, and SIGTERM shutdown. Probe tests protect dependency-free responses,
shutdown readiness, HTTP status/redirect failures, deadlines, and connection errors.

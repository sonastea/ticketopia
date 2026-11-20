# Metrics, traces and logs in SigNoz (OpenTelemetry)

Ticketopia exports operational metrics and traces over OTLP/HTTP when an
OpenTelemetry endpoint is configured. Logs stay as structured JSON on stdout; a
collector next to the app ships those lines. Any OTLP/HTTP backend or collector works.

- **Metrics** leave the app directly: off unless an endpoint is set, exported
  every 60 seconds and once more on shutdown. An unreachable endpoint never
  stops startup or requests; each failed export logs a `Telemetry export failed`
  warning and the next interval tries again.
- **Traces** leave the app directly too, but routine successful traces are
  discarded before export. Diagnostic traces are sent in batches, with a
  bounded queue and a final flush on shutdown. A collector outage can drop
  spans but does not block request handling.
- **Logs** are read from Docker's container log files by the collector in
  [`deploy/otel-collector`](../deploy/otel-collector/), which parses the JSON
  fields into log attributes, maps `level` to severity and `message` to the body,
  and forwards only lines with Ticketopia's `component` field.
- The periodic metric summaries stay on stdout (`Live activity metrics` every
  15 seconds, `MCP metrics` at most once a minute, `Event catalog operational
  status` every 15 minutes), so nothing is lost without a backend and the load
  tests can still read them. The collector does not forward them: the same
  figures arrive as metrics, and the live activity summary alone would be about
  5,800 log records a day per replica. To save space it also leaves out the
  container log file name and the `level` attribute, which severity replaces.

## App configuration

Ticketopia uses standard OpenTelemetry variables, including exporter options
such as signal-specific headers, timeout, compression and TLS certificates.

| Variable | Effect |
| --- | --- |
| `OTEL_EXPORTER_OTLP_ENDPOINT` | Base OTLP/HTTP URL, for example `http://10.0.0.5:4318`. Enables both signals at `/v1/metrics` and `/v1/traces` unless individually disabled or overridden. |
| `OTEL_EXPORTER_OTLP_METRICS_ENDPOINT` | Full metrics URL, used as is; overrides the base. |
| `OTEL_EXPORTER_OTLP_TRACES_ENDPOINT` | Full traces URL, used as is; overrides the base. |
| `OTEL_EXPORTER_OTLP_HEADERS` | Headers such as `Authorization=Bearer …` when a proxy in front of the collector requires them. Secret. |
| `OTEL_METRIC_EXPORT_INTERVAL` | Milliseconds between exports (default 60000). |
| `OTEL_SERVICE_NAME`, `OTEL_RESOURCE_ATTRIBUTES` | Override `service.name` (default `ticketopia`) or add attributes such as `deployment.environment.name=production`. |
| `OTEL_SDK_DISABLED=true` | Disable both signals. |
| `OTEL_METRICS_EXPORTER=none`, `OTEL_TRACES_EXPORTER=none` | Disable just that signal. |

Only `http/protobuf` (port 4318) is supported for the global or signal-specific
`OTEL_EXPORTER_OTLP_*PROTOCOL` settings. An unsupported protocol for an enabled
signal, or an exporter other than `otlp` or `none`, stops startup with an error.
Every replica reports `service.version` from `SOURCE_COMMIT` and
`service.instance.id` from its hostname (the container ID on Docker, the pod name
on Kubernetes).

## Traces

Ticketopia exports a trace only if at least one span is marked as an error or
takes **2 seconds or longer**. It decides when the root operation finishes, so
an otherwise healthy request still appears if a nested tool fails. Fast,
successful traces never go to SigNoz; metrics still count them. In SigNoz
**Traces**, filter to `service.name = ticketopia` and use duration or errors
to narrow the results.

- HTTP server spans use route templates, never raw URLs or query parameters.
  HTTP 5xx and panics are errors; ordinary HTTP 4xx retain their status code
  without being marked as server failures. Health checks, static assets and
  live activity stream connections are excluded.
- MCP tool spans are children of `POST /mcp`. Tool errors are marked even when
  the HTTP request returns 200. Only registered names (or `unknown`) and fixed
  outcomes are exported, not arguments or results.
- Live activity snapshot, projection and count operations have separate spans,
  as do event history collection, missing event refreshes and reminder
  processing. Failed operations are marked without raw error messages.

Logs with a slow or failed span can include `trace_id` and `span_id` for lookup;
routine fast logs don't link to discarded traces. No request body, credential, identity, provider
payload, or raw exception text is added to spans. HTTP requests start local
traces; Ticketopia does not accept trace or baggage headers from public clients.

Ticketopia filters locally before sending, so no collector sampling policy is
needed. The app buffers context for up to 1,024 in-flight traces with up to
16 child spans each. Larger bursts or unusually large traces can lose some
surrounding context rather than grow memory without bound.

## Metrics

Fixed dimensions only, as in the logs: never accounts, IPs, tokens, tool
arguments or event data. Counters are cumulative; durations are histograms in
seconds with bounds 1 ms, 10 ms, 50 ms, 250 ms, 1 s and 5 s. Depending on its
version, SigNoz may show names with dots replaced by underscores.

| Metric | Type | Attributes |
| --- | --- | --- |
| `ticketopia.http.server.requests` | counter | `method`, `route` (template, or `unmatched`), `status_class` (`2xx`…`5xx`) |
| `ticketopia.http.server.duration` | histogram | `method`, `route`; event streams are not timed |
| `ticketopia.mcp.tool.calls` | counter | `tool`, `outcome` (`ok`, `tool_error`, `error`), `signed_in` |
| `ticketopia.mcp.tool.duration` | histogram | `tool` |
| `ticketopia.mcp.refusals` | counter | `reason`, as in [agent tool metrics](app-mcp.md) |
| `ticketopia.oauth.revocations` | counter | `outcome` (`revoked`, `revoked_rotated`, `unknown`, `other_client`) |
| `ticketopia.activity.operation.duration` | histogram | `operation` |
| `ticketopia.activity.streams`, `.pending`, `.sql_jobs`, `.accounted_bytes` | gauges | — |
| `ticketopia.activity.cache` | gauge | `kind` (`candidates`, `cohorts`, `owners`) |
| `ticketopia.activity.sql_statements`, `.overwritten_updates`, `.write_errors` | counters | — |
| `ticketopia.activity.cache_lookups` | counter | `result` (`hit`, `miss`) |
| `ticketopia.activity.refusals` | counter | `status` (`429`, `503`) |
| `ticketopia.activity.failures` | counter | `reason` |
| `ticketopia.catalog.status` | gauge | `measure` (the `collection` fields of the status log); refreshed every 15 minutes, absent until the first query |
| `ticketopia.db.connections` | gauge | `state` (`in_use`, `idle`) |
| `ticketopia.db.connections.max` | gauge | — |
| `ticketopia.db.connection.waits`, `.wait_time` | counters | — |
| `ticketopia.process.goroutines`, `.heap_objects` | gauges | — |

Activity metrics appear when live activity is on, catalog and database metrics
with MariaDB, and MCP and OAuth metrics when those endpoints serve requests.
See [live activity rollout](live-activity-rollout.md) for what the activity
figures mean.

## Request logs

Successful requests are not logged, and `/healthz` and `/readyz` never are.
What is logged stays small however busy the site is:

| Record | Level | When |
| --- | --- | --- |
| `HTTP request failed` | error | The first 5xx per route each minute, at once, with `error` |
| `HTTP handler panicked` | error | Every panic, at once, with `panic` and a stack of at most 4 KB; the client gets a 500 |
| `HTTP server errors` | error | Once a minute, per route and status, when a 5xx repeated |
| `HTTP client errors` | warn | Once a minute: 429s, and 404s from known routes (possibly broken links) |
| `HTTP client errors` | info | Once a minute: other 4xx, including paths matching no route |
| `Slow HTTP requests` | warn | Once a minute, per route, for successful requests over 2 seconds (event streams excluded) |

Records carry `method`, `path`, `status`, `count`, `durationMs` (the slowest in
the minute) and `window`. `path` is the route template, such as
`/events/:event_id`, never an ID; requests matching no route show
`(no route)` with `examples` of the most frequent probed paths (for example
`/wp-login.php ×20, /.env ×3`), at most 100 characters each and without
query strings. A minute yields at most 100 summary lines; anything beyond is
one `HTTP responses over the per-minute group limit` line with its count.

For an at-a-glance view in SigNoz, save a logs view filtered to
`service.name = ticketopia` and severity `warn` or `error`.

## Coolify: Ticketopia and SigNoz on separate machines

1. **Make SigNoz's OTLP/HTTP port reachable from the Ticketopia machine only.**
   SigNoz's collector accepts OTLP on 4318 without authentication, so anyone who
   can reach it can write data. Prefer a private network between the machines
   (a provider VPC, WireGuard or Tailscale); otherwise allow only the Ticketopia
   machine's address to 4318 in the SigNoz machine's firewall. Across the public
   internet, put TLS and an authenticating proxy in front of it and set the
   header on both senders (`OTEL_EXPORTER_OTLP_HEADERS` for the app, `headers`
   on the collector's `otlp_http/signoz` exporter). Check which ports Coolify's
   SigNoz resource publishes.
2. **Ship logs.** Run `deploy/otel-collector` on the Ticketopia machine with
   `SIGNOZ_OTLP_ENDPOINT` set (for example `http://10.0.0.5:4318`). It runs the
   pinned contrib collector as root, read-only on `/var/lib/docker/containers`,
   starting at the end of existing files and remembering its position in a
   volume across restarts. It needs no ports. If the machine runs other apps
   whose JSON logs have a `component` field, tighten the `filter/ticketopia`
   condition.

   Production runs it as the `otel-collector` Docker Compose service in the
   Ticketopia project, not from the repository: the compose file embeds
   `config.yaml` with Coolify's volume `content:` key, because Coolify may
   mount a relative `./config.yaml` as an empty directory. Compose would also
   reject `${env:…}` inside that content, so the embedded copy has the SigNoz
   endpoint written in. Copy changes to `config.yaml` into the service by hand.
3. **Export metrics and traces.** Set `OTEL_EXPORTER_OTLP_ENDPOINT` to the same
   address on the Ticketopia application and redeploy; startup logs include
   `Exporting metrics over OTLP` and `Exporting traces over OTLP`. If the app
   uses only `OTEL_EXPORTER_OTLP_METRICS_ENDPOINT`, add the full
   `OTEL_EXPORTER_OTLP_TRACES_ENDPOINT` (`…/v1/traces`) or use the base setting instead.
   Optionally add
   `OTEL_RESOURCE_ATTRIBUTES=deployment.environment.name=production`.
4. In SigNoz, filter logs, metrics and traces by `service.name = ticketopia`. Log
   attributes include every JSON field, for example `outcome` and `client_host`
   on `OAuth token revocation` records.
5. Disk use in SigNoz is set by its retention periods (Settings → General),
   separately for logs, metrics and traces.

## Kubernetes

Point `OTEL_EXPORTER_OTLP_ENDPOINT` at the in-cluster collector's OTLP/HTTP
service. For logs, use SigNoz's `k8s-infra` Helm chart (or any collector reading
`/var/log/pods`) instead of `deploy/otel-collector`, with the same JSON parsing:
`level` to severity, `message` to body. This path is not yet exercised.

## Verification

Tests cover the environment rules, metrics and diagnostic traces to a fake
OTLP/HTTP receiver (including a shutdown flush and no export for fast successes),
HTTP error status and route privacy, and MCP child spans. Locally against Docker
with the pinned collector: a real
Linux build exported process and MCP metrics (calls, duration histogram,
refusals), including the final flush on stop, and the host collector forwarded
the app's and a test container's JSON lines with fields and severity while
dropping other containers' lines. After deploying tracing, check **Traces** for
`service.name = ticketopia` after a naturally slow or failed operation. A fast
successful request should not appear.

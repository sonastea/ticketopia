# Metrics and logs in SigNoz (OpenTelemetry)

Ticketopia exports its operational metrics over OTLP/HTTP when an OpenTelemetry
endpoint is configured, and keeps logging one structured JSON object per line to
stdout. A collector next to the app ships those log lines; nothing in the app
depends on SigNoz specifically, so any OTLP/HTTP backend or collector works.

- **Metrics** leave the app directly: off unless an endpoint is set, exported
  every 60 seconds and once more on shutdown. An unreachable endpoint never
  stops startup or requests; each failed export logs a `Telemetry export failed`
  warning and the next interval tries again.
- **Logs** are read from Docker's container log files by the collector in
  [`deploy/otel-collector`](../deploy/otel-collector/), which parses the JSON
  fields into log attributes, maps `level` to severity and `message` to the body,
  and forwards only lines with Ticketopia's `component` field.
- The once-a-minute metric log records (`MCP metrics`, `Live activity metrics`,
  `Event catalog operational status`) remain, so nothing is lost without a backend.

## App configuration

Standard OpenTelemetry variables; only these and the exporter's own standard
variables (headers, timeout, compression, TLS certificates) are read.

| Variable | Effect |
| --- | --- |
| `OTEL_EXPORTER_OTLP_ENDPOINT` | Base OTLP/HTTP URL, for example `http://10.0.0.5:4318`; metrics go to `/v1/metrics`. Setting this (or the next) turns export on. |
| `OTEL_EXPORTER_OTLP_METRICS_ENDPOINT` | Full metrics URL, used as is; overrides the base. |
| `OTEL_EXPORTER_OTLP_HEADERS` | Headers such as `Authorization=Bearer …` when a proxy in front of the collector requires them. Secret. |
| `OTEL_METRIC_EXPORT_INTERVAL` | Milliseconds between exports (default 60000). |
| `OTEL_SERVICE_NAME`, `OTEL_RESOURCE_ATTRIBUTES` | Override `service.name` (default `ticketopia`) or add attributes such as `deployment.environment.name=production`. |
| `OTEL_SDK_DISABLED=true`, `OTEL_METRICS_EXPORTER=none` | Turn export off while keeping an endpoint configured. |

Only HTTP/protobuf (port 4318) is supported: `OTEL_EXPORTER_OTLP_PROTOCOL` or
`OTEL_EXPORTER_OTLP_METRICS_PROTOCOL` set to anything else, or another
`OTEL_METRICS_EXPORTER`, stops startup with an error rather than exporting
nothing. Every replica reports `service.version` from `SOURCE_COMMIT` and
`service.instance.id` from its hostname (the container ID on Docker, the pod
name on Kubernetes).

## Metrics

Fixed dimensions only, as in the logs: never accounts, IPs, tokens, tool
arguments or event data. Counters are cumulative; durations are histograms in
seconds with bounds 1 ms, 10 ms, 50 ms, 250 ms, 1 s and 5 s. Depending on its
version, SigNoz may show names with dots replaced by underscores.

| Metric | Type | Attributes |
| --- | --- | --- |
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
3. **Export metrics.** Set `OTEL_EXPORTER_OTLP_ENDPOINT` to the same address on
   the Ticketopia application and redeploy; the startup log then contains
   `Exporting metrics over OTLP` with the endpoint. Optionally add
   `OTEL_RESOURCE_ATTRIBUTES=deployment.environment.name=production`.
4. In SigNoz, filter logs and metrics by `service.name = ticketopia`. Log
   attributes include every JSON field, for example `outcome` and `client_host`
   on `OAuth token revocation` records.

## Kubernetes

Point `OTEL_EXPORTER_OTLP_ENDPOINT` at the in-cluster collector's OTLP/HTTP
service. For logs, use SigNoz's `k8s-infra` Helm chart (or any collector reading
`/var/log/pods`) instead of `deploy/otel-collector`, with the same JSON parsing:
`level` to severity, `message` to body. This path is not yet exercised.

## Verification

Unit tests cover the environment rules, an export to a fake OTLP/HTTP receiver
(resource attributes, instruments created before setup) and the live activity and
catalog callbacks. Locally against Docker with the pinned collector: a real
Linux build exported process and MCP metrics (calls, duration histogram,
refusals), including the final flush on stop, and the host collector forwarded
the app's and a test container's JSON lines with fields and severity while
dropping other containers' lines. In production, the app and the collector
service run with these settings; data arriving in SigNoz is still to be
confirmed.

# Observability changes

## Unreleased

### 2026-11-13 — OpenTelemetry metrics export and log shipping

- Export operational metrics over OTLP/HTTP when the standard
  `OTEL_EXPORTER_OTLP_ENDPOINT` (or `OTEL_EXPORTER_OTLP_METRICS_ENDPOINT`) is
  set, for SigNoz or any collector: MCP tool calls, duration histograms and
  refusals; OAuth revocation outcomes; live activity gauges, counters and
  operation durations; event catalog status; SQL pool connections and waits;
  goroutines and heap. Resources carry `service.name=ticketopia`,
  `service.version` from `SOURCE_COMMIT` and a per-replica instance ID. Export
  is off by default, unsupported protocols or exporters stop startup, failed
  exports only warn, and a final export runs on shutdown. Existing metric logs
  are unchanged.
- Add `deploy/otel-collector`: a pinned contrib collector for a Docker or
  Coolify host that reads container logs, parses Ticketopia's JSON lines into
  attributes and severity, keeps only Ticketopia's lines and forwards them to
  `SIGNOZ_OTLP_ENDPOINT`. To save disk space it drops the periodic metric
  summary records, already exported as metrics, and the file name and `level`
  attributes; they stay on stdout.
- Verified with unit tests and locally with Docker against a stand-in OTLP
  receiver (metrics from a real build, including the shutdown flush, and shipped
  logs); not yet against a running SigNoz. See the
  [guide](../observability.md).

package appmcp

import (
	"context"
	"maps"
	"slices"
	"sync"
	"time"

	"github.com/rs/zerolog"
	"github.com/sonastea/ticketopia/internal/telemetry"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

// Exported instruments mirror the logged counts with the same fixed
// dimensions; they are no-ops unless telemetry export is configured.
var (
	toolCalls, _ = telemetry.Meter().Int64Counter("ticketopia.mcp.tool.calls", metric.WithUnit("{call}"),
		metric.WithDescription("MCP tool calls by tool, outcome (ok, tool_error, error) and signed_in."))
	toolDuration, _ = telemetry.Meter().Float64Histogram("ticketopia.mcp.tool.duration", metric.WithUnit("s"),
		metric.WithDescription("MCP tool call duration by tool."), metric.WithExplicitBucketBoundaries(telemetry.DurationBuckets...))
	refusals, _ = telemetry.Meter().Int64Counter("ticketopia.mcp.refusals", metric.WithUnit("{request}"),
		metric.WithDescription("Requests refused at the /mcp boundary by reason."))
)

// Refusal reasons at the /mcp boundary, before any tool runs.
const (
	RefusedOrigin              = "origin"
	RefusedIPRate              = "ip_rate"
	RefusedAccountRate         = "account_rate"
	RefusedBusy                = "busy"
	RefusedUnauthorized        = "unauthorized"
	RefusedAccountsUnavailable = "accounts_unavailable"
	RefusedBatch               = "batch"
	RefusedTooLarge            = "too_large"
	// With OAuth: a signed-in tool called without a token, or a change
	// requested through a read-only connection.
	RefusedSignInRequired    = "sign_in_required"
	RefusedInsufficientScope = "insufficient_scope"
)

// Metrics counts tool calls and boundary refusals per replica, with fixed
// dimensions only, like live-activity telemetry: registered tool names (any
// other name counts as "unknown", so clients cannot add dimensions), outcome,
// and whether the caller was signed in. Never arguments, accounts or IPs.
// Durations use cumulative, disjoint buckets (<=1/10/50/250/1000/5000 ms, then
// +Inf), not sampled percentiles.
type Metrics struct {
	mu       sync.Mutex
	tools    map[string]*ToolStats
	refusals map[string]uint64
	changed  bool
}

// ToolStats is one tool's cumulative counts. Calls = OK + ToolErrors + Errors.
type ToolStats struct {
	Calls      uint64    `json:"calls"`
	OK         uint64    `json:"ok"`
	ToolErrors uint64    `json:"tool_errors"`
	Errors     uint64    `json:"errors"`
	SignedIn   uint64    `json:"signed_in"`
	Seconds    float64   `json:"seconds"`
	Buckets    [7]uint64 `json:"buckets"`
}

// Snapshot is the metrics at one moment.
type Snapshot struct {
	Tools    map[string]ToolStats `json:"tools"`
	Refusals map[string]uint64    `json:"refusals"`
}

// toolNames are every tool this package can register; a test keeps the list
// in step with NewServer.
var toolNames = []string{"find_events", "list_categories", "event_details",
	"my_saved_events", "save_event", "unsave_event", "my_reminders", "set_reminder", "stop_reminder"}

func NewMetrics() *Metrics {
	return &Metrics{tools: map[string]*ToolStats{}, refusals: map[string]uint64{}}
}

// toolName is the registered name, or "unknown" for any other client-sent name.
func toolName(name string) string {
	if slices.Contains(toolNames, name) {
		return name
	}
	return "unknown"
}

func (m *Metrics) observe(tool, outcome string, signedIn bool, d time.Duration) {
	if m == nil {
		return
	}
	normalized := outcome
	if outcome != "ok" && outcome != "tool_error" {
		normalized = "error"
	}
	ctx := context.Background()
	toolCalls.Add(ctx, 1, metric.WithAttributes(attribute.String("tool", tool), attribute.String("outcome", normalized), attribute.Bool("signed_in", signedIn)))
	toolDuration.Record(ctx, d.Seconds(), metric.WithAttributes(attribute.String("tool", tool)))
	m.mu.Lock()
	defer m.mu.Unlock()
	s := m.tools[tool]
	if s == nil {
		s = &ToolStats{}
		m.tools[tool] = s
	}
	s.Calls++
	switch outcome {
	case "ok":
		s.OK++
	case "tool_error":
		s.ToolErrors++
	default:
		s.Errors++
	}
	if signedIn {
		s.SignedIn++
	}
	s.Seconds += d.Seconds()
	i := 0
	for _, bound := range [...]time.Duration{time.Millisecond, 10 * time.Millisecond, 50 * time.Millisecond, 250 * time.Millisecond, time.Second, 5 * time.Second} {
		if d <= bound {
			break
		}
		i++
	}
	s.Buckets[i]++
	m.changed = true
}

// Refuse counts a request refused at the HTTP boundary for one of the
// Refused* reasons.
func (m *Metrics) Refuse(reason string) {
	if m == nil {
		return
	}
	refusals.Add(context.Background(), 1, metric.WithAttributes(attribute.String("reason", reason)))
	m.mu.Lock()
	defer m.mu.Unlock()
	m.refusals[reason]++
	m.changed = true
}

func (m *Metrics) Snapshot() Snapshot {
	m.mu.Lock()
	defer m.mu.Unlock()
	s := Snapshot{Tools: make(map[string]ToolStats, len(m.tools)), Refusals: maps.Clone(m.refusals)}
	for name, stats := range m.tools {
		s.Tools[name] = *stats
	}
	return s
}

// Run logs an "MCP metrics" record every interval in which anything changed,
// until ctx ends. Use interval deltas, not cumulative averages, for alerting.
func (m *Metrics) Run(ctx context.Context, logger zerolog.Logger, every time.Duration) {
	tick := time.NewTicker(every)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
		}
		m.mu.Lock()
		changed := m.changed
		m.changed = false
		m.mu.Unlock()
		if changed {
			logger.Info().Interface("mcp", m.Snapshot()).Msg("MCP metrics")
		}
	}
}

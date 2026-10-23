package persistence

import (
	"encoding/csv"
	"os"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"
)

// soakRequest joins one measured probe request across the probe (client),
// nginx (probe log) and, in traced periods, r0's execution trace. Missing
// components are -1.
type soakRequest struct {
	Period  int
	State   string
	Traced  bool
	Op      string
	Method  string
	Started time.Time
	// Client is the probe's latency; Nginx its $request_time; Connect, Header
	// and Upstream nginx's $upstream_*_time (Upstream includes Connect).
	Client, Nginx, Connect, Header, Upstream time.Duration
	// Pre is r0's connection goroutine creation to handler start (accept,
	// request read, routing); Handler the task; Post the response flush.
	Pre, Handler, Post                                  time.Duration
	Running, Runnable, Network, Sync, Syscall, Other    time.Duration
	Assist, GCMark, STW                                 time.Duration
	Notify, NotifySync, Tx, TxNetwork, Commit, CommitIO time.Duration
}

// soakRequestComponents lists the reported fields in path order; ok is false
// when a component was not observed for the request. Differences of nginx's
// millisecond values can be negative for one request, so they are only
// summarized.
var soakRequestComponents = []struct {
	name  string
	value func(soakRequest) (time.Duration, bool)
}{
	{"client", known(func(r soakRequest) time.Duration { return r.Client })},
	{"probe↔nginx (client−nginx)", differs(func(r soakRequest) []time.Duration { return []time.Duration{r.Client, r.Nginx} })},
	{"nginx", known(func(r soakRequest) time.Duration { return r.Nginx })},
	{"nginx own (nginx−upstream)", differs(func(r soakRequest) []time.Duration { return []time.Duration{r.Nginx, r.Upstream} })},
	{"upstream connect", known(func(r soakRequest) time.Duration { return r.Connect })},
	{"upstream response", known(func(r soakRequest) time.Duration { return r.Upstream })},
	{"upstream outside r0 Go", differs(func(r soakRequest) []time.Duration {
		return []time.Duration{r.Upstream, r.Connect, r.Pre, r.Handler, r.Post}
	})},
	{"r0 pre-handler", known(func(r soakRequest) time.Duration { return r.Pre })},
	{"r0 handler", known(func(r soakRequest) time.Duration { return r.Handler })},
	{"r0 post-handler flush", known(func(r soakRequest) time.Duration { return r.Post })},
	{"handler running", known(func(r soakRequest) time.Duration { return r.Running })},
	{"handler runnable (sched)", known(func(r soakRequest) time.Duration { return r.Runnable })},
	{"handler network wait", known(func(r soakRequest) time.Duration { return r.Network })},
	{"handler sync wait", known(func(r soakRequest) time.Duration { return r.Sync })},
	{"handler syscall", known(func(r soakRequest) time.Duration { return r.Syscall })},
	{"handler other wait", known(func(r soakRequest) time.Duration { return r.Other })},
	{"handler GC assist", known(func(r soakRequest) time.Duration { return r.Assist })},
	{"handler ∩ GC mark", known(func(r soakRequest) time.Duration { return r.GCMark })},
	{"handler ∩ stop-the-world", known(func(r soakRequest) time.Duration { return r.STW })},
	{"membership notify", known(func(r soakRequest) time.Duration { return r.Notify })},
	{"notify sync wait", known(func(r soakRequest) time.Duration { return r.NotifySync })},
	{"sql tx", known(func(r soakRequest) time.Duration { return r.Tx })},
	{"sql tx network wait", known(func(r soakRequest) time.Duration { return r.TxNetwork })},
	{"sql commit", known(func(r soakRequest) time.Duration { return r.Commit })},
	{"sql commit network wait", known(func(r soakRequest) time.Duration { return r.CommitIO })},
}

func known(field func(soakRequest) time.Duration) func(soakRequest) (time.Duration, bool) {
	return func(r soakRequest) (time.Duration, bool) { v := field(r); return v, v >= 0 }
}

// differs is the first value minus the rest, when all were observed.
func differs(fields func(soakRequest) []time.Duration) func(soakRequest) (time.Duration, bool) {
	return func(r soakRequest) (time.Duration, bool) {
		values := fields(r)
		total := values[0]
		for _, v := range values {
			if v < 0 {
				return 0, false
			}
		}
		for _, v := range values[1:] {
			total -= v
		}
		return total, true
	}
}

func soakRoute(op, method string) string {
	if op == "browse" {
		return "GET /community"
	}
	return method + " /api/v1/me/event-interests/:event_id"
}

// joinSoakRequests matches a probe run's samples to its nginx log lines (by
// order: the probe is sequential, and its first five iterations are warmup)
// and to r0 trace tasks (by route and containment in the client interval).
func joinSoakRequests(t *testing.T, period int, state string, iterations int, result loadProbeResult, logged []probeLogEntry, traced *traceSummary) []soakRequest {
	const warmup = 5
	useLog := len(logged) == 2*(iterations+warmup)
	for i := 0; useLog && i < len(logged); i++ {
		want := "GET"
		if i%2 == 1 {
			want = map[bool]string{true: "DELETE", false: "PUT"}[(i/2)%2 == 0]
		}
		useLog = logged[i].Method == want
	}
	if !useLog {
		t.Logf("period %d: %d probe log lines not in probe order (want %d); nginx timings omitted", period, len(logged), 2*(iterations+warmup))
	}
	var tasks []*tracedRequest
	if traced != nil {
		tasks = slices.Clone(traced.Requests)
		slices.SortFunc(tasks, func(a, b *tracedRequest) int { return a.Begin.Compare(b.Begin) })
	}
	rows, ambiguous, unmatched := []soakRequest{}, 0, 0
	for i := range iterations {
		for k, op := range []string{"browse", "write"} {
			timed := result.BrowseTimed
			if k == 1 {
				timed = result.WriteTimed
			}
			if i >= len(timed) || timed[i].Error != "" {
				continue
			}
			s := timed[i]
			method := "GET"
			if k == 1 {
				method = "PUT"
				if (i+warmup)%2 == 0 {
					method = "DELETE"
				}
			}
			r := soakRequest{Period: period, State: state, Traced: traced != nil, Op: op, Method: method, Started: s.Started, Client: s.Latency,
				Nginx: -1, Connect: -1, Header: -1, Upstream: -1, Pre: -1, Handler: -1, Post: -1, Running: -1, Runnable: -1, Network: -1, Sync: -1,
				Syscall: -1, Other: -1, Assist: -1, GCMark: -1, STW: -1, Notify: -1, NotifySync: -1, Tx: -1, TxNetwork: -1, Commit: -1, CommitIO: -1}
			if useLog {
				e := logged[2*(i+warmup)+k]
				r.Nginx, r.Connect, r.Header, r.Upstream = e.Request, e.Connect, e.Header, e.ResponseTime
			}
			if traced != nil {
				// The handler runs inside the probe's request interval; allow
				// clock rounding at both ends.
				lo, hi := s.Started.Add(-200*time.Microsecond), s.Started.Add(s.Latency+200*time.Microsecond)
				start, _ := slices.BinarySearchFunc(tasks, lo, func(a *tracedRequest, at time.Time) int { return a.Begin.Compare(at) })
				var match *tracedRequest
				count := 0
				for _, task := range tasks[start:] {
					if task.Begin.After(hi) {
						break
					}
					if task.Route == soakRoute(op, method) && !task.End.After(hi) {
						match = task
						count++
					}
				}
				switch {
				case count > 1:
					ambiguous++
				case count == 0:
					unmatched++
				default:
					fillTraced(&r, match)
				}
			}
			rows = append(rows, r)
		}
	}
	if traced != nil {
		t.Logf("period %d: matched trace tasks for %d/%d requests (%d ambiguous, %d unmatched)", period, len(rows)-ambiguous-unmatched, len(rows), ambiguous, unmatched)
	}
	return rows
}

func fillTraced(r *soakRequest, task *tracedRequest) {
	r.Handler = task.End.Sub(task.Begin)
	if !task.Created.IsZero() {
		r.Pre = task.Begin.Sub(task.Created)
	}
	if !task.Flushed.IsZero() {
		r.Post = task.Flushed.Sub(task.End)
	}
	r.Running, r.Runnable, r.Network, r.Sync, r.Syscall = task.States["running"], task.States["runnable"], task.States["wait:network"], task.States["wait:sync"], task.States["syscall"]
	r.Other = 0
	for state, d := range task.States {
		if strings.HasPrefix(state, "wait:") && state != "wait:network" && state != "wait:sync" {
			r.Other += d
		}
	}
	r.Assist, r.GCMark, r.STW = task.Assist, task.GCMark, task.STW
	r.Notify, r.NotifySync = task.Regions["activity.membership.notify"], task.RegionStates["activity.membership.notify"]["wait:sync"]
	r.Tx, r.TxNetwork = task.Regions["sql.tx"], task.RegionStates["sql.tx"]["wait:network"]
	r.Commit, r.CommitIO = task.Regions["sql.commit"], task.RegionStates["sql.commit"]["wait:network"]
}

func writeSoakRequests(path string, rows []soakRequest) error {
	file, err := os.Create(path)
	if err != nil {
		return err
	}
	w := csv.NewWriter(file)
	header := []string{"period", "state", "traced", "op", "method", "started_utc"}
	for _, c := range soakRequestComponents {
		header = append(header, c.name+" ms")
	}
	_ = w.Write(header)
	for _, r := range rows {
		record := []string{strconv.Itoa(r.Period), r.State, strconv.FormatBool(r.Traced), r.Op, r.Method, r.Started.UTC().Format(time.RFC3339Nano)}
		for _, c := range soakRequestComponents {
			v, ok := c.value(r)
			if !ok {
				record = append(record, "")
				continue
			}
			record = append(record, strconv.FormatFloat(float64(v)/1e6, 'f', 4, 64))
		}
		_ = w.Write(record)
	}
	w.Flush()
	if err := w.Error(); err != nil {
		file.Close()
		return err
	}
	return file.Close()
}

// logSoakRequestDiagnostics reports each component's mean, p50 and p95 per
// state and the difference between loaded and each other state present
// (no-live, or an ablation arm). Means add along the request path; percentiles
// do not.
func logSoakRequestDiagnostics(t *testing.T, rows []soakRequest) {
	for _, against := range soakArms {
		logSoakRequestDiagnosticsAgainst(t, rows, against)
	}
}

func logSoakRequestDiagnosticsAgainst(t *testing.T, rows []soakRequest, against string) {
	label := ""
	if against != soakNoLive {
		label = " vs " + against
	}
	for _, op := range []string{"write", "browse"} {
		for _, subset := range []struct {
			label string
			keep  func(soakRequest) bool
		}{{"all periods", func(soakRequest) bool { return true }}, {"untraced periods", func(r soakRequest) bool { return !r.Traced }}, {"traced periods", func(r soakRequest) bool { return r.Traced }}} {
			byState := map[string][]soakRequest{}
			for _, r := range rows {
				if r.Op == op && subset.keep(r) {
					byState[r.State] = append(byState[r.State], r)
				}
			}
			if len(byState[soakLoaded]) == 0 || len(byState[against]) == 0 {
				continue
			}
			p95 := func(state string) time.Duration {
				values := []time.Duration{}
				for _, r := range byState[state] {
					values = append(values, r.Client)
				}
				return durationPercentile(values, 95)
			}
			t.Logf("diagnostic %s %s%s: client p95 loaded=%s %s=%s ratio=%.3f (n=%d/%d)", op, subset.label, label, p95(soakLoaded), against, p95(against),
				float64(p95(soakLoaded))/float64(p95(against)), len(byState[soakLoaded]), len(byState[against]))
			if subset.label == "untraced periods" {
				continue
			}
			for _, c := range soakRequestComponents {
				stats := map[string][3]float64{}
				n := map[string]int{}
				for _, state := range []string{soakLoaded, against} {
					values := []float64{}
					for _, r := range byState[state] {
						if v, ok := c.value(r); ok {
							values = append(values, float64(v)/1e6)
						}
					}
					if len(values) == 0 {
						continue
					}
					stats[state] = [3]float64{meanFloat(values), percentile(values, 50), percentile(values, 95)}
					n[state] = len(values)
				}
				if n[soakLoaded] == 0 || n[against] == 0 {
					continue
				}
				l, b := stats[soakLoaded], stats[against]
				t.Logf("diagnostic %s %s%s %-28s loaded mean=%.3f p50=%.3f p95=%.3f | %s mean=%.3f p50=%.3f p95=%.3f | Δmean=%+.3f Δp50=%+.3f ms (n=%d/%d)",
					op, subset.label, label, c.name, l[0], l[1], l[2], against, b[0], b[1], b[2], l[0]-b[0], l[1]-b[1], n[soakLoaded], n[against])
			}
		}
	}
}

// logTraceRuntime reports r0's Go runtime over one traced period.
func logTraceRuntime(t *testing.T, period int, state string, s traceSummary, size int64) {
	wall := s.Wall.Seconds()
	if wall <= 0 {
		return
	}
	t.Logf("trace period %d %-7s r0 runtime: wall=%.1fs go_cpu=%.3f cores gc_cycles=%d (%.2f/s) gc_mark=%.1f%% stw=%.2fms sched_latency p50=%s p99=%s max=%s (n=%d) requests=%d trace=%.1f MiB",
		period, state, wall, s.Running.Seconds()/wall, s.GCCycles, float64(s.GCCycles)/wall, 100*s.GCMark.Seconds()/wall, float64(s.STW)/1e6,
		durationPercentile(s.SchedLatency, 50), durationPercentile(s.SchedLatency, 99), durationPercentile(s.SchedLatency, 100), len(s.SchedLatency),
		len(s.Requests), float64(size)/(1<<20))
}

func traceFileSize(path string) int64 {
	info, err := os.Stat(path)
	if err != nil {
		return 0
	}
	return info.Size()
}

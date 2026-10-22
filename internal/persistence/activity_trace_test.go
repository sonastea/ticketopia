package persistence

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime/trace"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

// tracedRequest is one completed http.request task from a replica's execution
// trace (see api.traceRequests), with its goroutine's time split by state.
type tracedRequest struct {
	Route      string
	Begin, End time.Time
	// Created is the connection goroutine's creation (nginx opens one upstream
	// connection per request); Flushed is its first block after the handler
	// returned, when the response has been written. Zero when not observed.
	Created, Flushed time.Time
	// States: "running", "runnable" (scheduler latency), "syscall" and
	// "wait:<reason>" (for example wait:network for SQL round trips).
	States      map[string]time.Duration
	Assist      time.Duration // GC mark assist on this goroutine
	GCMark, STW time.Duration // overlap with the global GC mark phase / stop-the-world
	// Regions sums each region type on this goroutine during the task, with
	// the same state split inside it.
	Regions      map[string]time.Duration
	RegionStates map[string]map[string]time.Duration
}

type traceSummary struct {
	Requests []*tracedRequest
	Wall     time.Duration
	// Running sums every goroutine's running time; with Wall it is the
	// process's Go CPU use. SchedLatency holds every runnable→running wait.
	Running      time.Duration
	SchedLatency []time.Duration
	GCCycles     int
	GCMark, STW  time.Duration
}

type traceInterval struct{ begin, end int64 }

type traceRegion struct {
	kind   string
	begin  int64
	states map[string]time.Duration
}

type traceGoroutine struct {
	state   string
	since   int64
	task    *tracedRequest
	ended   *tracedRequest // awaiting its post-handler block
	regions []traceRegion
	assist  int64 // mark-assist start, or -1
}

// traceFields splits a parsed-trace line into its event kind, key=value pairs
// (quotes removed) and bare state transition ("A->B").
func traceFields(line string) (string, map[string]string, string) {
	values := map[string]string{}
	kind, transition := "", ""
	field, quoted := strings.Builder{}, false
	flush := func() {
		token := field.String()
		field.Reset()
		if token == "" {
			return
		}
		key, value, ok := strings.Cut(token, "=")
		switch {
		case ok:
			if unquoted, err := strconv.Unquote(value); err == nil {
				value = unquoted
			}
			values[key] = value
		case strings.Contains(token, "->"):
			transition = token
		case kind == "":
			kind = token
		}
	}
	for i := 0; i < len(line); i++ {
		c := line[i]
		switch {
		case c == '\\' && quoted && i+1 < len(line):
			field.WriteByte(c)
			i++
			field.WriteByte(line[i])
		case c == '"':
			quoted = !quoted
			field.WriteByte(c)
		case c == ' ' && !quoted:
			flush()
		default:
			field.WriteByte(c)
		}
	}
	flush()
	return kind, values, transition
}

// analyzeTrace streams `go tool trace -d=parsed` (the standard toolchain's
// event dump) and reconstructs completed request tasks and runtime totals.
func analyzeTrace(ctx context.Context, path string) (traceSummary, error) {
	cmd := exec.CommandContext(ctx, "go", "tool", "trace", "-d=parsed", path)
	out, err := cmd.StdoutPipe()
	if err != nil {
		return traceSummary{}, err
	}
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Start(); err != nil {
		return traceSummary{}, err
	}
	summary, parseErr := parseTrace(out)
	if err := cmd.Wait(); err != nil {
		return summary, fmt.Errorf("go tool trace: %v %s", err, stderr.String())
	}
	return summary, parseErr
}

func parseTrace(r interface{ Read([]byte) (int, error) }) (traceSummary, error) {
	var summary traceSummary
	type syncPoint struct {
		at   int64
		wall time.Time
	}
	syncs := []syncPoint{}
	goroutines := map[string]*traceGoroutine{}
	created := map[string]int64{}
	tasks := map[string]*tracedRequest{}
	taskTimes := map[*tracedRequest][4]int64{} // begin, end, created, flushed
	var marks, stws []traceInterval
	markSince, first, last := int64(-1), int64(-1), int64(0)
	stwSince := map[string]int64{}
	g := func(id string) *traceGoroutine {
		if goroutines[id] == nil {
			goroutines[id] = &traceGoroutine{assist: -1}
		}
		return goroutines[id]
	}
	// account attributes the goroutine's time since its last event.
	account := func(gr *traceGoroutine, at int64) {
		if gr.state != "" && at > gr.since {
			d := time.Duration(at - gr.since)
			if gr.state == "running" {
				summary.Running += d
			}
			if gr.task != nil {
				gr.task.States[gr.state] += d
				for _, region := range gr.regions {
					region.states[gr.state] += d
				}
			}
		}
		gr.since = at
	}
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 64<<10), 4<<20)
	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "M=") {
			continue
		}
		kind, v, transition := traceFields(line)
		at, err := strconv.ParseInt(v["Time"], 10, 64)
		if err != nil {
			continue
		}
		if first < 0 {
			first = at
		}
		last = max(last, at)
		switch kind {
		case "Sync":
			if wall, err := time.Parse(time.RFC3339Nano, v["Wall"]); err == nil {
				syncs = append(syncs, syncPoint{at, wall})
			}
		case "StateTransition":
			id, ok := v["GoID"]
			if !ok {
				continue
			}
			from, to, _ := strings.Cut(transition, "->")
			gr := g(id)
			if from == "Runnable" && to == "Running" && gr.state == "runnable" {
				summary.SchedLatency = append(summary.SchedLatency, time.Duration(at-gr.since))
			}
			account(gr, at)
			if from == "NotExist" {
				created[id] = at
			}
			if gr.ended != nil && from == "Running" && to != "Runnable" {
				times := taskTimes[gr.ended]
				times[3] = at
				taskTimes[gr.ended] = times
				gr.ended = nil
			}
			switch to {
			case "Running":
				gr.state = "running"
			case "Runnable":
				gr.state = "runnable"
			case "Syscall":
				gr.state = "syscall"
			case "Waiting":
				gr.state = "wait:" + v["Reason"]
			case "NotExist":
				delete(goroutines, id)
				delete(created, id)
			default:
				gr.state = ""
			}
		case "TaskBegin":
			if v["Type"] != "http.request" {
				continue
			}
			gr := g(v["G"])
			account(gr, at)
			req := &tracedRequest{States: map[string]time.Duration{}, Regions: map[string]time.Duration{}, RegionStates: map[string]map[string]time.Duration{}}
			gr.task, gr.ended, gr.regions = req, nil, nil
			tasks[v["ID"]] = req
			taskTimes[req] = [4]int64{at, -1, created[v["G"]], 0}
		case "Log":
			if req := tasks[v["Task"]]; req != nil && v["Category"] == "route" {
				req.Route = v["Message"]
			}
		case "TaskEnd":
			req := tasks[v["ID"]]
			if req == nil {
				continue
			}
			delete(tasks, v["ID"])
			times := taskTimes[req]
			times[1] = at
			taskTimes[req] = times
			for _, gr := range goroutines {
				if gr.task == req {
					account(gr, at)
					gr.task, gr.ended, gr.regions = nil, req, nil
				}
			}
			summary.Requests = append(summary.Requests, req)
		case "RegionBegin":
			gr := g(v["G"])
			account(gr, at)
			gr.regions = append(gr.regions, traceRegion{v["Type"], at, map[string]time.Duration{}})
		case "RegionEnd":
			gr := g(v["G"])
			account(gr, at)
			if n := len(gr.regions); n > 0 && gr.regions[n-1].kind == v["Type"] {
				region := gr.regions[n-1]
				gr.regions = gr.regions[:n-1]
				if gr.task != nil {
					gr.task.Regions[region.kind] += time.Duration(at - region.begin)
					if gr.task.RegionStates[region.kind] == nil {
						gr.task.RegionStates[region.kind] = map[string]time.Duration{}
					}
					for state, d := range region.states {
						gr.task.RegionStates[region.kind][state] += d
					}
				}
			}
		case "RangeBegin", "RangeActive", "RangeEnd":
			name, scope := v["Name"], v["Scope"]
			begin := kind != "RangeEnd"
			switch {
			case name == "GC concurrent mark phase":
				if begin {
					markSince = at
				} else if markSince >= 0 {
					marks = append(marks, traceInterval{markSince, at})
					summary.GCMark += time.Duration(at - markSince)
					summary.GCCycles++
					markSince = -1
				}
			case strings.HasPrefix(name, "stop-the-world"):
				if begin {
					stwSince[scope] = at
				} else if since, ok := stwSince[scope]; ok {
					stws = append(stws, traceInterval{since, at})
					summary.STW += time.Duration(at - since)
					delete(stwSince, scope)
				}
			case name == "GC mark assist":
				id := strings.TrimSuffix(strings.TrimPrefix(scope, "Goroutine("), ")")
				gr := g(id)
				if begin {
					gr.assist = at
				} else if gr.assist >= 0 {
					if gr.task != nil {
						gr.task.Assist += time.Duration(at - gr.assist)
					}
					gr.assist = -1
				}
			}
		}
	}
	if err := scanner.Err(); err != nil {
		return summary, err
	}
	if len(syncs) == 0 {
		return summary, fmt.Errorf("trace has no wall-clock sync events")
	}
	wall := func(at int64) time.Time {
		k, _ := slices.BinarySearchFunc(syncs, at, func(s syncPoint, at int64) int { return int(s.at - at) })
		k = min(max(k-1, 0), len(syncs)-1)
		if syncs[k].at > at && k > 0 {
			k--
		}
		return syncs[k].wall.Add(time.Duration(at - syncs[k].at))
	}
	overlap := func(intervals []traceInterval, begin, end int64) time.Duration {
		total := int64(0)
		for _, iv := range intervals {
			total += max(0, min(end, iv.end)-max(begin, iv.begin))
		}
		return time.Duration(total)
	}
	for _, req := range summary.Requests {
		times := taskTimes[req]
		req.Begin, req.End = wall(times[0]), wall(times[1])
		if times[2] > 0 {
			req.Created = wall(times[2])
		}
		if times[3] > 0 {
			req.Flushed = wall(times[3])
		}
		req.GCMark, req.STW = overlap(marks, times[0], times[1]), overlap(stws, times[0], times[1])
	}
	summary.Wall = time.Duration(last - first)
	return summary, nil
}

// The parser is checked against a real trace from the current toolchain: a
// request task with a network round trip inside a commit region and a mutex
// wait inside the notification region.
func TestActivityTraceAnalysis(t *testing.T) {
	if _, err := exec.LookPath("go"); err != nil {
		t.Skip("go tool trace unavailable")
	}
	if trace.IsEnabled() {
		t.Skip("a trace is already recording")
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "request.trace")
	file, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := trace.Start(file); err != nil {
		t.Fatal(err)
	}
	// A loopback TCP pair, so the round trip blocks in the network poller.
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	client, err := net.Dial("tcp", listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	server, err := listener.Accept()
	if err != nil {
		t.Fatal(err)
	}
	defer server.Close()
	var mu sync.Mutex
	mu.Lock()
	locked := make(chan struct{})
	go func() {
		<-locked
		time.Sleep(20 * time.Millisecond)
		mu.Unlock()
	}()
	go func() {
		b := make([]byte, 1)
		if _, err := server.Read(b); err == nil {
			time.Sleep(15 * time.Millisecond)
			server.Write(b)
		}
	}()
	done := make(chan struct{})
	go func() {
		defer close(done)
		ctx, task := trace.NewTask(context.Background(), "http.request")
		trace.Log(ctx, "route", "PUT /api/v1/me/event-interests/:event_id")
		commit := trace.StartRegion(ctx, "sql.commit")
		client.Write([]byte{1})
		client.Read(make([]byte, 1))
		commit.End()
		notify := trace.StartRegion(context.Background(), "activity.membership.notify")
		close(locked)
		mu.Lock()
		notify.End()
		task.End()
	}()
	<-done
	trace.Stop()
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	summary, err := analyzeTrace(t.Context(), path)
	if err != nil {
		t.Fatal(err)
	}
	if len(summary.Requests) != 1 {
		t.Fatalf("requests=%d", len(summary.Requests))
	}
	req := summary.Requests[0]
	t.Logf("route=%q handler=%s states=%v regions=%v region_states=%v", req.Route, req.End.Sub(req.Begin), req.States, req.Regions, req.RegionStates)
	if req.Route != "PUT /api/v1/me/event-interests/:event_id" {
		t.Errorf("route %q", req.Route)
	}
	handler := req.End.Sub(req.Begin)
	if handler < 35*time.Millisecond || handler > time.Second {
		t.Errorf("handler %s", handler)
	}
	total := time.Duration(0)
	for _, d := range req.States {
		total += d
	}
	if diff := handler - total; diff < -time.Millisecond || diff > time.Millisecond {
		t.Errorf("states sum to %s, handler %s", total, handler)
	}
	if got := req.RegionStates["sql.commit"]["wait:network"]; got < 10*time.Millisecond {
		t.Errorf("commit network wait %s", got)
	}
	if got := req.RegionStates["activity.membership.notify"]["wait:sync"]; got < 15*time.Millisecond {
		t.Errorf("notification mutex wait %s", got)
	}
	if req.Regions["sql.commit"] < req.RegionStates["sql.commit"]["wait:network"] {
		t.Error("region shorter than its waits")
	}
}

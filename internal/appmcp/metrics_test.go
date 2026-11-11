package appmcp

import (
	"context"
	"net/http"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/rs/zerolog"
)

// Calls are counted per registered tool, outcome and sign-in; names a client
// makes up count as "unknown", so they cannot add metric dimensions.
func TestMetricsCountToolCallsWithFixedDimensions(t *testing.T) {
	m := NewMetrics()
	f := &fakeAccount{}
	base := privateServerWith(t, f, m).URL
	tool(t, base, "", "find_events", map[string]any{"limit": 0})      // tool error: invalid limit
	tool(t, base, "", "list_categories", map[string]any{})            // ok
	tool(t, base, "owner-token", "my_saved_events", map[string]any{}) // ok, signed in
	// A made-up tool is a protocol error, still counted, under "unknown".
	request, _ := http.NewRequestWithContext(t.Context(), http.MethodPost, base, strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"made-up-tool-1234","arguments":{}}}`))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Accept", "application/json, text/event-stream")
	if response, err := http.DefaultClient.Do(request); err != nil {
		t.Fatal(err)
	} else {
		response.Body.Close()
	}
	s := m.Snapshot()
	if got := s.Tools["find_events"]; got.Calls != 1 || got.ToolErrors != 1 || got.SignedIn != 0 {
		t.Errorf("find_events: %+v", got)
	}
	if got := s.Tools["list_categories"]; got.Calls != 1 || got.OK != 1 {
		t.Errorf("list_categories: %+v", got)
	}
	if got := s.Tools["my_saved_events"]; got.Calls != 1 || got.OK != 1 || got.SignedIn != 1 {
		t.Errorf("my_saved_events: %+v", got)
	}
	if got := s.Tools["unknown"]; got.Calls != 1 {
		t.Errorf("unknown: %+v", got)
	}
	for name, stats := range s.Tools {
		if name != "unknown" && !slices.Contains(toolNames, name) {
			t.Errorf("unexpected dimension %q", name)
		}
		var buckets uint64
		for _, b := range stats.Buckets {
			buckets += b
		}
		if buckets != stats.Calls || stats.OK+stats.ToolErrors+stats.Errors != stats.Calls {
			t.Errorf("%s counts disagree: %+v", name, stats)
		}
	}
}

// The list of names matches the tools NewServer registers with every service.
func TestToolNamesMatchRegisteredTools(t *testing.T) {
	reply := rpc(t, privateServer(t, &fakeAccount{}).URL, "", "tools/list", map[string]any{})
	var registered []string
	for _, tool := range reply.Result.Tools {
		registered = append(registered, tool.Name)
	}
	slices.Sort(registered)
	want := slices.Sorted(slices.Values(toolNames))
	if !slices.Equal(registered, want) {
		t.Fatalf("registered %v, toolNames %v", registered, want)
	}
}

type syncBuffer struct {
	mu sync.Mutex
	b  strings.Builder
}

func (s *syncBuffer) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.Write(p)
}

func (s *syncBuffer) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.String()
}

// The periodic record is logged only for intervals with changes.
func TestMetricsLogOnlyWhenChanged(t *testing.T) {
	m := NewMetrics()
	var logs syncBuffer
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan struct{})
	go func() { m.Run(ctx, zerolog.New(&logs), 10*time.Millisecond); close(done) }()
	time.Sleep(50 * time.Millisecond)
	if strings.Contains(logs.String(), "MCP metrics") {
		t.Fatal("logged without changes")
	}
	m.Refuse(RefusedBusy)
	deadline := time.Now().Add(2 * time.Second)
	for !strings.Contains(logs.String(), `"busy":1`) && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	time.Sleep(60 * time.Millisecond) // several more ticks without changes
	cancel()
	<-done
	if out := logs.String(); strings.Count(out, "MCP metrics") != 1 || !strings.Contains(out, `"refusals":{"busy":1}`) {
		t.Fatalf("logs: %s", out)
	}
}

package appmcp

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/rs/zerolog"
	"github.com/sonastea/ticketopia/internal/discovery"
	"github.com/sonastea/ticketopia/internal/models"
)

type fakeEvents struct {
	query  discovery.Query
	err    error
	detail map[string]models.EventDetail
}

func (f *fakeEvents) Events(_ context.Context, q discovery.Query) (models.EventList, error) {
	f.query = q
	if f.err != nil {
		return models.EventList{}, f.err
	}
	at := time.Date(2026, 10, 30, 4, 12, 0, 0, time.UTC)
	return models.EventList{Items: []models.Event{
		{ID: "ticketmaster:fresh", Name: "Fresh show"},
		{ID: "ticketmaster:gone", Name: "Removed show", Meta: &models.Freshness{DataAsOf: at, Stale: true}},
	}, Total: 2, Meta: models.Freshness{DataAsOf: time.Now().UTC()}}, nil
}

func (f *fakeEvents) Categories(context.Context) (models.CategoryList, error) {
	return models.CategoryList{Items: []models.Category{{NamedID: models.NamedID{ID: "KZFzniwnSyZfZ7v7nJ", Name: "Music"}}}}, nil
}

func (f *fakeEvents) Detail(_ context.Context, id string) (models.EventDetail, error) {
	if d, ok := f.detail[id]; ok {
		return d, nil
	}
	return models.EventDetail{}, discovery.ErrNotFound
}

func session(t *testing.T, f *fakeEvents) *mcp.ClientSession {
	t.Helper()
	return sessionWith(t, f, zerolog.Nop())
}

func sessionWith(t *testing.T, f *fakeEvents, logger zerolog.Logger) *mcp.ClientSession {
	t.Helper()
	server := NewServer(Deps{Events: f, Details: f, Now: func() time.Time { return time.Date(2026, 11, 9, 12, 0, 0, 0, time.UTC) },
		Describe: func(err error) string { return "described: " + err.Error() }, Logger: logger})
	serverTransport, clientTransport := mcp.NewInMemoryTransports()
	if _, err := server.Connect(t.Context(), serverTransport, nil); err != nil {
		t.Fatal(err)
	}
	client := mcp.NewClient(&mcp.Implementation{Name: "test", Version: "0"}, nil)
	s, err := client.Connect(t.Context(), clientTransport, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func call(t *testing.T, s *mcp.ClientSession, name string, args map[string]any) (*mcp.CallToolResult, map[string]any) {
	t.Helper()
	result, err := s.CallTool(t.Context(), &mcp.CallToolParams{Name: name, Arguments: args})
	if err != nil {
		t.Fatal(err)
	}
	out := map[string]any{}
	if !result.IsError {
		data, _ := json.Marshal(result.StructuredContent)
		_ = json.Unmarshal(data, &out)
	}
	return result, out
}

func text(r *mcp.CallToolResult) string {
	if len(r.Content) == 0 {
		return ""
	}
	if c, ok := r.Content[0].(*mcp.TextContent); ok {
		return c.Text
	}
	return ""
}

func TestToolsAreReadOnlyPublicSet(t *testing.T) {
	tools, err := session(t, &fakeEvents{}).ListTools(t.Context(), nil)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, tool := range tools.Tools {
		names = append(names, tool.Name)
	}
	slices.Sort(names)
	if !slices.Equal(names, []string{"event_details", "find_events", "list_categories"}) {
		t.Fatalf("tools: %v", names)
	}
	// Agents can discover result shapes; freshness allows an unknown time.
	for _, tool := range tools.Tools {
		schema, _ := json.Marshal(tool.OutputSchema)
		if tool.OutputSchema == nil || !strings.Contains(string(schema), `"data_as_of":{"format":"date-time","type":["string","null"]}`) {
			t.Errorf("%s output schema: %s", tool.Name, schema)
		}
	}
}

// find_events applies Discover's own parsing, and keeps list and per-event
// freshness so agents can tell what is not live.
func TestFindEventsUsesDiscoverQueryRules(t *testing.T) {
	f := &fakeEvents{}
	result, out := call(t, session(t, f), "find_events", map[string]any{"city": "  Chicago ", "country": "us", "limit": 5})
	if result.IsError {
		t.Fatal(text(result))
	}
	if f.query.City != "Chicago" || f.query.Country != "US" || f.query.Limit != 5 || f.query.StartDate != "2026-11-09" || f.query.Sort != "date_asc" {
		t.Fatalf("parsed query %+v", f.query)
	}
	items := out["items"].([]any)
	gone := items[1].(map[string]any)["meta"].(map[string]any)
	if len(items) != 2 || out["meta"] == nil || gone["stale"] != true || gone["data_as_of"] != "2026-10-30T04:12:00Z" {
		t.Fatalf("result %+v", out)
	}
}

func TestToolErrorsAreDescribedForAgents(t *testing.T) {
	f := &fakeEvents{detail: map[string]models.EventDetail{}}
	s := session(t, f)
	result, _ := call(t, s, "find_events", map[string]any{"limit": 500})
	if !result.IsError || !strings.Contains(text(result), "described: ") || !strings.Contains(text(result), "limit") {
		t.Fatalf("invalid limit: %+v %q", result, text(result))
	}
	if f.query.Limit != 0 {
		t.Fatal("an invalid query reached the event service")
	}
	// An explicit 0 is invalid, as in /api/v1, not the default page size.
	if result, _ := call(t, s, "find_events", map[string]any{"limit": 0}); !result.IsError || !strings.Contains(text(result), "limit") {
		t.Fatalf("limit 0: %q", text(result))
	}
	f.err = &discovery.UnavailableError{RetryAfter: 30 * time.Second}
	if result, _ := call(t, s, "find_events", map[string]any{"city": "Chicago"}); !result.IsError || !strings.HasPrefix(text(result), "described: ") {
		t.Fatalf("unavailable: %q", text(result))
	}
	if result, _ := call(t, s, "event_details", map[string]any{"event_id": "ticketmaster:missing"}); !result.IsError || !strings.Contains(text(result), "described: ") {
		t.Fatalf("missing event: %q", text(result))
	}
}

// Real details pass output validation: an unknown data_as_of is null, and
// history changes carry arbitrary JSON before/after values.
func TestEventDetailsReturnsDetail(t *testing.T) {
	seen := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	history := &models.EventHistory{FirstSeen: seen, LastSeen: seen, Changes: []models.EventChange{{ObservedAt: seen, Kind: "price",
		Summary: "Price changed", Before: json.RawMessage(`{"min":"25.00"}`), After: json.RawMessage(`null`)}}}
	f := &fakeEvents{detail: map[string]models.EventDetail{"ticketmaster:one": {Item: models.Event{ID: "ticketmaster:one", Name: "One"}, History: history}}}
	result, out := call(t, session(t, f), "event_details", map[string]any{"event_id": "ticketmaster:one"})
	if result.IsError || out["item"].(map[string]any)["name"] != "One" {
		t.Fatalf("%+v %q", out, text(result))
	}
}

// Stateless HTTP: a single POST of tools/call, with no initialize request or
// session header first, is answered as plain JSON. Load tools such as k6 rely
// on this.
func TestStatelessHTTPToolCallWithoutInitialize(t *testing.T) {
	server := httptest.NewServer(Handler(Deps{Events: &fakeEvents{}, Details: &fakeEvents{}}))
	defer server.Close()
	body := `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"list_categories","arguments":{}}}`
	request, _ := http.NewRequestWithContext(t.Context(), http.MethodPost, server.URL, strings.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Accept", "application/json, text/event-stream")
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	data, _ := io.ReadAll(response.Body)
	if response.StatusCode != http.StatusOK || !strings.HasPrefix(response.Header.Get("Content-Type"), "application/json") ||
		response.Header.Get("Mcp-Session-Id") != "" || !strings.Contains(string(data), "KZFzniwnSyZfZ7v7nJ") {
		t.Fatalf("status %d type %q session %q body %s", response.StatusCode, response.Header.Get("Content-Type"), response.Header.Get("Mcp-Session-Id"), data)
	}
}

// Calls the SDK rejects before a tool runs (wrong argument types) are logged
// too, with tool and outcome but never arguments.
func TestEveryToolCallIsLoggedWithoutArguments(t *testing.T) {
	var logs strings.Builder
	s := sessionWith(t, &fakeEvents{}, zerolog.New(&logs))
	if result, _ := call(t, s, "find_events", map[string]any{"limit": "lots", "keyword": "secret-search-term"}); !result.IsError {
		t.Fatal("wrong argument type accepted")
	}
	call(t, s, "list_categories", map[string]any{})
	out := logs.String()
	if !strings.Contains(out, `"tool":"find_events","outcome":"tool_error"`) || !strings.Contains(out, `"tool":"list_categories","outcome":"ok"`) ||
		strings.Contains(out, "secret-search-term") || strings.Contains(out, "lots") {
		t.Fatalf("logs: %s", out)
	}
}

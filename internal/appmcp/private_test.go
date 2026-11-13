package appmcp

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/auth"
	"github.com/sonastea/ticketopia/internal/models"
	"github.com/sonastea/ticketopia/internal/reminders"
	"github.com/sonastea/ticketopia/internal/saved"
)

type fakeAccount struct {
	calls []string
}

func (f *fakeAccount) record(call string) { f.calls = append(f.calls, call) }

func (f *fakeAccount) List(_ context.Context, owner string, v url.Values) (saved.List, error) {
	f.record("saved.List " + owner + " " + v.Encode())
	return saved.List{Items: []saved.Item{{Event: models.Event{ID: "ticketmaster:mine", Name: "Mine"}, SavedAt: time.Now()}}}, nil
}
func (f *fakeAccount) Save(_ context.Context, owner, id string) (saved.Item, bool, error) {
	f.record("saved.Save " + owner + " " + id)
	return saved.Item{Event: models.Event{ID: id}, SavedAt: time.Now()}, true, nil
}
func (f *fakeAccount) Remove(_ context.Context, owner, id string) error {
	f.record("saved.Remove " + owner + " " + id)
	return nil
}

type fakeReminders struct{ *fakeAccount }

func (f fakeReminders) List(_ context.Context, owner string, v url.Values) (reminders.Page, error) {
	f.record("reminders.List " + owner + " " + v.Encode())
	return reminders.Page{Items: []reminders.Item{}}, nil
}
func (f fakeReminders) Set(_ context.Context, owner, event string, lead int) (reminders.Item, error) {
	f.record("reminders.Set " + owner + " " + event)
	if lead > 10080 {
		return reminders.Item{}, reminders.Lead(lead)
	}
	return reminders.Item{EventID: event, LeadMinutes: lead, State: "scheduled"}, nil
}
func (f fakeReminders) Remove(_ context.Context, owner, event string) error {
	f.record("reminders.Remove " + owner + " " + event)
	return nil
}

// privateServer verifies "owner-token" (read and write) and "reader-token"
// (read only) as account owner-id, as the API's boundary does, only for
// requests carrying a token.
func privateServer(t *testing.T, f *fakeAccount) *httptest.Server {
	t.Helper()
	return privateServerWith(t, f, nil)
}

func privateServerWith(t *testing.T, f *fakeAccount, metrics *Metrics) *httptest.Server {
	t.Helper()
	handler := Handler(Deps{Events: &fakeEvents{}, Details: &fakeEvents{}, Saved: f, Reminders: fakeReminders{f}, Metrics: metrics,
		DescribePrivate: func(err error) string { return "private: " + err.Error() }})
	bearer := auth.RequireBearerToken(func(_ context.Context, token string, _ *http.Request) (*auth.TokenInfo, error) {
		switch token {
		case "owner-token":
			return &auth.TokenInfo{UserID: "owner-id", Scopes: []string{"read", "write"}, Expiration: time.Now().Add(time.Hour)}, nil
		case "reader-token":
			return &auth.TokenInfo{UserID: "owner-id", Scopes: []string{"read"}, Expiration: time.Now().Add(time.Hour)}, nil
		}
		return nil, auth.ErrInvalidToken
	}, nil)(handler)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "" {
			bearer.ServeHTTP(w, r)
			return
		}
		handler.ServeHTTP(w, r)
	}))
	t.Cleanup(server.Close)
	return server
}

type toolReply struct {
	Result struct {
		IsError           bool            `json:"isError"`
		StructuredContent json.RawMessage `json:"structuredContent"`
		Content           []struct{ Text string }
		Tools             []struct {
			Name        string
			Annotations struct {
				ReadOnlyHint    bool  `json:"readOnlyHint"`
				DestructiveHint *bool `json:"destructiveHint"`
			}
		}
	}
	Error json.RawMessage
}

func rpc(t *testing.T, base, token, method string, params any) toolReply {
	t.Helper()
	body, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": 1, "method": method, "params": params})
	request, _ := http.NewRequestWithContext(t.Context(), http.MethodPost, base, strings.NewReader(string(body)))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Accept", "application/json, text/event-stream")
	if token != "" {
		request.Header.Set("Authorization", "Bearer "+token)
	}
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	var reply toolReply
	if err := json.NewDecoder(response.Body).Decode(&reply); err != nil || reply.Error != nil {
		t.Fatalf("%s: %v %s", method, err, reply.Error)
	}
	return reply
}

func tool(t *testing.T, base, token, name string, args map[string]any) toolReply {
	t.Helper()
	return rpc(t, base, token, "tools/call", map[string]any{"name": name, "arguments": args})
}

// Signed-in tools act only for the token's account; without a token they
// explain how to sign in and touch nothing.
func TestPrivateToolsUseOnlyTheTokenAccount(t *testing.T) {
	f := &fakeAccount{}
	base := privateServer(t, f).URL
	event := map[string]any{"event_id": "ticketmaster:x"}
	for name, args := range map[string]map[string]any{"my_saved_events": {}, "save_event": event, "unsave_event": event, "my_reminders": {},
		"set_reminder": {"event_id": "ticketmaster:x", "lead_minutes": 30}, "stop_reminder": event} {
		reply := tool(t, base, "", name, args)
		if !reply.Result.IsError || !strings.Contains(reply.Result.Content[0].Text, "Authorization: Bearer") {
			t.Fatalf("%s without token: %+v", name, reply.Result)
		}
	}
	if len(f.calls) != 0 {
		t.Fatalf("anonymous calls reached services: %v", f.calls)
	}
	tool(t, base, "owner-token", "my_saved_events", map[string]any{"limit": 5})
	tool(t, base, "owner-token", "my_saved_events", map[string]any{"cursor": ""})
	save := tool(t, base, "owner-token", "save_event", map[string]any{"event_id": "ticketmaster:x"})
	var state struct {
		Saved, Created bool
	}
	_ = json.Unmarshal(save.Result.StructuredContent, &state)
	tool(t, base, "owner-token", "set_reminder", map[string]any{"event_id": "ticketmaster:x", "lead_minutes": 30})
	tool(t, base, "owner-token", "my_reminders", map[string]any{})
	tool(t, base, "owner-token", "stop_reminder", map[string]any{"event_id": "ticketmaster:x"})
	tool(t, base, "owner-token", "unsave_event", map[string]any{"event_id": "ticketmaster:x"})
	want := []string{"saved.List owner-id limit=5", "saved.List owner-id cursor=", "saved.Save owner-id ticketmaster:x", "reminders.Set owner-id ticketmaster:x",
		"reminders.List owner-id ", "reminders.Remove owner-id ticketmaster:x", "saved.Remove owner-id ticketmaster:x"}
	if strings.Join(f.calls, "|") != strings.Join(want, "|") || !state.Saved || !state.Created {
		t.Fatalf("calls %v state %+v", f.calls, state)
	}
	// Service errors use the account API wording.
	if reply := tool(t, base, "owner-token", "set_reminder", map[string]any{"event_id": "ticketmaster:x", "lead_minutes": 99999}); !reply.Result.IsError ||
		!strings.HasPrefix(reply.Result.Content[0].Text, "private: ") {
		t.Fatalf("invalid lead: %+v", reply.Result)
	}
}

// Agents can tell reads from writes, and which writes remove data.
func TestToolAnnotationsMarkWrites(t *testing.T) {
	reply := rpc(t, privateServer(t, &fakeAccount{}).URL, "", "tools/list", map[string]any{})
	destructive := map[string]bool{}
	for _, tool := range reply.Result.Tools {
		if tool.Annotations.ReadOnlyHint {
			destructive[tool.Name] = false
			continue
		}
		destructive[tool.Name] = tool.Annotations.DestructiveHint == nil || *tool.Annotations.DestructiveHint
	}
	for name, want := range map[string]bool{"find_events": false, "my_saved_events": false, "save_event": false, "set_reminder": true, "unsave_event": true, "stop_reminder": true} {
		if got, ok := destructive[name]; !ok || got != want {
			t.Errorf("%s destructive=%v listed=%v, want %v", name, got, ok, want)
		}
	}
	for _, tool := range reply.Result.Tools {
		if (tool.Name == "find_events" || tool.Name == "my_reminders") && !tool.Annotations.ReadOnlyHint {
			t.Errorf("%s is not marked read-only", tool.Name)
		}
	}
}

func TestPrivateToolsNeedAccountServices(t *testing.T) {
	tools, err := session(t, &fakeEvents{}).ListTools(t.Context(), nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, tool := range tools.Tools {
		if strings.HasPrefix(tool.Name, "my_") || strings.Contains(tool.Name, "save") || strings.Contains(tool.Name, "reminder") {
			t.Fatalf("%s listed without account services", tool.Name)
		}
	}
}

// A read-only connection reads the account but cannot change it, even if a
// request gets past the HTTP boundary's scope check.
func TestReadOnlyTokensCannotWrite(t *testing.T) {
	f := &fakeAccount{}
	base := privateServer(t, f).URL
	event := map[string]any{"event_id": "ticketmaster:x"}
	for name, args := range map[string]map[string]any{"save_event": event, "unsave_event": event,
		"set_reminder": {"event_id": "ticketmaster:x", "lead_minutes": 30}, "stop_reminder": event} {
		if reply := tool(t, base, "reader-token", name, args); !reply.Result.IsError || !strings.Contains(reply.Result.Content[0].Text, "read-only") {
			t.Fatalf("%s with read-only token: %+v", name, reply.Result)
		}
	}
	if len(f.calls) != 0 {
		t.Fatalf("read-only writes reached services: %v", f.calls)
	}
	if reply := tool(t, base, "reader-token", "my_saved_events", map[string]any{}); reply.Result.IsError {
		t.Fatalf("read-only read: %+v", reply.Result)
	}
}

// SignedInTools drives the HTTP boundary's sign-in and scope challenges, so
// it must name exactly the registered signed-in tools and their writes.
func TestSignedInToolsMatchRegistration(t *testing.T) {
	reply := rpc(t, privateServer(t, &fakeAccount{}).URL, "", "tools/list", map[string]any{})
	public := map[string]bool{"find_events": true, "list_categories": true, "event_details": true}
	listed := 0
	for _, tool := range reply.Result.Tools {
		if public[tool.Name] {
			continue
		}
		listed++
		write, ok := SignedInTools[tool.Name]
		if !ok || write == tool.Annotations.ReadOnlyHint {
			t.Errorf("%s: in SignedInTools=%v write=%v readOnlyHint=%v", tool.Name, ok, write, tool.Annotations.ReadOnlyHint)
		}
	}
	if listed != len(SignedInTools) {
		t.Fatalf("listed %d signed-in tools, SignedInTools has %d", listed, len(SignedInTools))
	}
}

package appmcp

import (
	"bytes"
	"context"
	"encoding/json"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"github.com/sonastea/ticketopia/internal/accounts"
	"github.com/sonastea/ticketopia/internal/drafts"
	"github.com/sonastea/ticketopia/internal/models"
	"github.com/sonastea/ticketopia/internal/recommendations"
)

type fakeRecommendations struct{ *fakeAccount }

func (f fakeRecommendations) Publish(_ context.Context, owner, id string, w recommendations.Write) (recommendations.Item, bool, error) {
	f.record("recommendations.Publish " + owner + " " + id + " via=" + w.Via + " host=" + w.ViaHost + " reason=" + w.Reason)
	if f.recommendation != nil && w.KeepExisting {
		return *f.recommendation, false, nil
	}
	item := recommendations.Item{Event: models.Event{ID: id}, Reason: w.Reason, Via: w.Via, ViaHost: w.ViaHost, RecommendedAt: time.Now()}
	f.recommendation = &item
	return item, true, nil
}
func (f fakeRecommendations) WithdrawForAgent(_ context.Context, owner, id string) error {
	f.record("recommendations.WithdrawForAgent " + owner + " " + id)
	if f.recommendation != nil && (f.recommendation.Via == "" || f.recommendation.Reason != "") {
		return recommendations.ErrPersonal
	}
	return nil
}
func (f fakeRecommendations) Own(_ context.Context, owner string, v url.Values) (recommendations.List, error) {
	f.record("recommendations.Own " + owner + " " + v.Encode())
	items := []recommendations.Item{}
	if f.recommendation != nil {
		items = append(items, *f.recommendation)
	}
	return recommendations.List{Items: items}, nil
}

type fakeDrafts struct{ *fakeAccount }

func (f fakeDrafts) CreateRecommendation(_ context.Context, account, eventID, text string, client drafts.Client) (drafts.Draft, error) {
	f.record("drafts.Create " + account + " " + eventID + " client=" + client.Name + " host=" + client.Host + " grant=" + client.GrantID)
	if _, err := drafts.Text(text); err != nil {
		return drafts.Draft{}, err
	}
	if text == "full" {
		return drafts.Draft{}, drafts.ErrLimit
	}
	return drafts.Draft{ID: strings.Repeat("a", 32), EventID: eventID, Kind: drafts.KindRecommendation, Text: text, ClientName: client.Name, ClientHost: client.Host}, nil
}
func (f fakeDrafts) List(_ context.Context, account string) ([]drafts.Draft, error) {
	f.record("drafts.List " + account)
	return []drafts.Draft{}, nil
}
func (f fakeDrafts) Discard(_ context.Context, account, id string) error {
	f.record("drafts.Discard " + account + " " + id)
	return nil
}

var publishingTools = map[string]map[string]any{
	"make_interest_public":    {"event_id": "ticketmaster:x"},
	"recommend_event":         {"event_id": "ticketmaster:x"},
	"withdraw_recommendation": {"event_id": "ticketmaster:x"},
}

var draftAndVisibilityTools = map[string]map[string]any{
	"make_interest_private": {"event_id": "ticketmaster:x"},
	"my_recommendations":    {},
	"draft_recommendation":  {"event_id": "ticketmaster:x", "reason": "Worth it"},
	"my_drafts":             {},
	"discard_draft":         {"draft_id": strings.Repeat("a", 32)},
}

// Every new tool refuses anonymous calls, and publishing tools refuse
// connections without publish (read-only and write-only alike) before any
// service runs.
func TestPublishingToolsNeedSignInAndPublishScope(t *testing.T) {
	f := &fakeAccount{}
	base := privateServer(t, f).URL
	for _, set := range []map[string]map[string]any{publishingTools, draftAndVisibilityTools} {
		for name, args := range set {
			if reply := tool(t, base, "", name, args); !reply.Result.IsError || !strings.Contains(reply.Result.Content[0].Text, "sign in required") {
				t.Fatalf("%s anonymous: %+v", name, reply.Result)
			}
		}
	}
	for name, args := range publishingTools {
		for _, token := range []string{"owner-token", "reader-token"} {
			reply := tool(t, base, token, name, args)
			if !reply.Result.IsError || !strings.Contains(reply.Result.Content[0].Text, "can't post publicly") {
				t.Fatalf("%s with %s: %+v", name, token, reply.Result)
			}
		}
		if SignedInTools[name] != ScopePublish {
			t.Fatalf("%s scope %q", name, SignedInTools[name])
		}
	}
	// publish does not imply write: private changes still need write.
	if reply := tool(t, base, "poster-token", "make_interest_private", draftAndVisibilityTools["make_interest_private"]); !reply.Result.IsError {
		t.Fatalf("publish-only token changed privately: %+v", reply.Result)
	}
	if len(f.calls) != 0 {
		t.Fatalf("refused calls reached services: %v", f.calls)
	}
}

// Publishing tools act only on the token's account, keep an existing
// recommendation's reason, attribute new ones to the app, and log only the
// tool and the app's host.
func TestPublishingToolsActForTheTokenAccount(t *testing.T) {
	var logs bytes.Buffer
	f := &fakeAccount{logger: zerolog.New(&logs)}
	base := privateServer(t, f).URL
	event := map[string]any{"event_id": "ticketmaster:x"}

	reply := tool(t, base, "publisher-token", "make_interest_public", event)
	var interest interestState
	_ = json.Unmarshal(reply.Result.StructuredContent, &interest)
	if reply.Result.IsError || interest.Item == nil || interest.Item.Visibility != "public" {
		t.Fatalf("make_interest_public: %+v", reply.Result)
	}
	if reply := tool(t, base, "owner-token", "make_interest_private", event); reply.Result.IsError {
		t.Fatalf("make_interest_private: %+v", reply.Result)
	}

	// An existing recommendation, reason included, comes back unchanged.
	f.recommendation = &recommendations.Item{Event: models.Event{ID: "ticketmaster:x"}, Reason: "My own words"}
	reply = tool(t, base, "publisher-token", "recommend_event", event)
	var state recommendationState
	_ = json.Unmarshal(reply.Result.StructuredContent, &state)
	if reply.Result.IsError || state.Created || state.Item.Reason != "My own words" || state.Item.Via != "" {
		t.Fatalf("existing recommendation: %+v", reply.Result)
	}
	f.recommendation = nil
	reply = tool(t, base, "publisher-token", "recommend_event", event)
	if !strings.Contains(string(reply.Result.StructuredContent), `"via":"Test Agent"`) {
		t.Fatalf("new recommendation lacks via: %s", reply.Result.StructuredContent)
	}
	if reply := tool(t, base, "publisher-token", "my_recommendations", map[string]any{}); !strings.Contains(string(reply.Result.StructuredContent), `"via":"Test Agent"`) {
		t.Fatalf("my_recommendations lacks via: %s", reply.Result.StructuredContent)
	}
	tool(t, base, "publisher-token", "withdraw_recommendation", event)
	// A recommendation the person wrote is not the agent's to withdraw.
	f.recommendation = &recommendations.Item{Event: models.Event{ID: "ticketmaster:x"}, Reason: "My own words"}
	if reply := tool(t, base, "publisher-token", "withdraw_recommendation", event); !reply.Result.IsError || !strings.Contains(reply.Result.Content[0].Text, "only they can withdraw it") {
		t.Fatalf("withdrew the person's recommendation: %+v", reply.Result)
	}

	want := []string{
		"interests.Set owner-id ticketmaster:x public",
		"interests.Set owner-id ticketmaster:x private",
		"recommendations.Publish owner-id ticketmaster:x via=Test Agent host=agent.example reason=",
		"recommendations.Publish owner-id ticketmaster:x via=Test Agent host=agent.example reason=",
		"recommendations.Own owner-id ",
		"recommendations.WithdrawForAgent owner-id ticketmaster:x",
		"recommendations.WithdrawForAgent owner-id ticketmaster:x",
	}
	if strings.Join(f.calls, "|") != strings.Join(want, "|") {
		t.Fatalf("calls:\n%s", strings.Join(f.calls, "\n"))
	}
	if got := strings.Count(logs.String(), `"message":"Agent published"`); got != 4 { // the refused withdrawal logs nothing
		t.Fatalf("Agent published records: %d\n%s", got, logs.String())
	}
	for _, line := range strings.Split(strings.TrimSpace(logs.String()), "\n") {
		if !strings.Contains(line, "Agent published") {
			continue
		}
		var record map[string]any
		_ = json.Unmarshal([]byte(line), &record)
		delete(record, "level")
		delete(record, "message")
		if len(record) != 2 || record["client_host"] != "agent.example" || record["tool"] == nil {
			t.Fatalf("log record carries more than tool and client_host: %s", line)
		}
	}
	if strings.Contains(logs.String(), "owner-id") || strings.Contains(logs.String(), "My own words") {
		t.Fatalf("logs carry account or text: %s", logs.String())
	}
}

// draft_recommendation stores a private draft through the drafts service
// only, never the recommendations service, and points the agent at the
// review page.
func TestDraftRecommendationNeverPublishes(t *testing.T) {
	f := &fakeAccount{}
	base := privateServer(t, f).URL
	reply := tool(t, base, "owner-token", "draft_recommendation", map[string]any{"event_id": "ticketmaster:x", "reason": "Arrive early"})
	var result draftResult
	_ = json.Unmarshal(reply.Result.StructuredContent, &result)
	if reply.Result.IsError || result.Published || result.ReviewURL != "https://events.example/me/drafts" || result.Draft.Text != "Arrive early" {
		t.Fatalf("draft: %+v %s", reply.Result, reply.Result.StructuredContent)
	}
	// Over the cap, the agent is told what to do.
	reply = tool(t, base, "owner-token", "draft_recommendation", map[string]any{"event_id": "ticketmaster:x", "reason": "full"})
	if !reply.Result.IsError || !strings.Contains(reply.Result.Content[0].Text, "discard_draft") {
		t.Fatalf("draft limit: %+v", reply.Result)
	}
	tool(t, base, "owner-token", "my_drafts", map[string]any{})
	tool(t, base, "owner-token", "discard_draft", map[string]any{"draft_id": strings.Repeat("a", 32)})
	for _, call := range f.calls {
		if strings.HasPrefix(call, "recommendations.") {
			t.Fatalf("drafting reached recommendations: %v", f.calls)
		}
	}
	// An API token identifies no app: no name, host or connection.
	if f.calls[0] != "drafts.Create owner-id ticketmaster:x client= host= grant=" {
		t.Fatalf("calls %v", f.calls)
	}
	// An OAuth connection's draft carries its verified host and connection.
	if reply := tool(t, base, "publisher-token", "draft_recommendation", map[string]any{"event_id": "ticketmaster:x", "reason": "Arrive early"}); reply.Result.IsError ||
		f.calls[len(f.calls)-1] != "drafts.Create owner-id ticketmaster:x client=Test Agent host=agent.example grant=grant-1" {
		t.Fatalf("OAuth draft: %+v %v", reply.Result, f.calls)
	}
	// A read-only connection cannot draft.
	if reply := tool(t, base, "reader-token", "draft_recommendation", map[string]any{"event_id": "ticketmaster:x", "reason": "x"}); !reply.Result.IsError {
		t.Fatalf("read-only draft: %+v", reply.Result)
	}
}

// The publish limit is per account and answers with a retry delay.
func TestPublishLimitPerAccount(t *testing.T) {
	now := time.Date(2026, 11, 14, 12, 0, 0, 0, time.UTC)
	l := NewPublishLimiter(2, func() time.Time { return now })
	for range 2 {
		if ok, _ := l.Allow("a"); !ok {
			t.Fatal("refused within the allowance")
		}
	}
	if ok, wait := l.Allow("a"); ok || wait < 29*time.Minute {
		t.Fatalf("third publish: ok=%v wait=%v", ok, wait)
	}
	if ok, _ := l.Allow("b"); !ok {
		t.Fatal("another account was limited")
	}
	now = now.Add(30 * time.Minute)
	if ok, _ := l.Allow("a"); !ok {
		t.Fatal("allowance did not refill")
	}

	f := &fakeAccount{limiter: NewPublishLimiter(1, nil)}
	base := privateServer(t, f).URL
	tool(t, base, "publisher-token", "withdraw_recommendation", map[string]any{"event_id": "ticketmaster:x"})
	reply := tool(t, base, "publisher-token", "withdraw_recommendation", map[string]any{"event_id": "ticketmaster:x"})
	if !reply.Result.IsError || !strings.Contains(reply.Result.Content[0].Text, "Retry after") || len(f.calls) != 1 {
		t.Fatalf("limited call: %+v calls=%v", reply.Result, f.calls)
	}
}

// Descriptions say what becomes public and that text goes through drafts.
func TestPublishingDescriptions(t *testing.T) {
	reply := rpc(t, privateServer(t, &fakeAccount{account: accounts.Account{}}).URL, "", "tools/list", map[string]any{})
	descriptions := map[string]string{}
	for _, tool := range reply.Result.Tools {
		descriptions[tool.Name] = tool.Description
	}
	for name, words := range map[string][]string{
		"make_interest_public": {"public", "permission to post publicly"},
		"recommend_event":      {"Publicly", "draft_recommendation", "unchanged"},
		"draft_recommendation": {"Nothing is published", "review_url"},
		"mark_interested":      {"never makes a choice public"},
	} {
		for _, word := range words {
			if !strings.Contains(descriptions[name], word) {
				t.Errorf("%s description lacks %q: %s", name, word, descriptions[name])
			}
		}
	}
}

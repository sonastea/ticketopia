package api

import (
	"context"
	"encoding/json"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/sonastea/ticketopia/internal/accounts"
	"github.com/sonastea/ticketopia/internal/appmcp"
	"github.com/sonastea/ticketopia/internal/interests"
	"github.com/sonastea/ticketopia/internal/models"
	"github.com/sonastea/ticketopia/internal/recommendations"
	"github.com/sonastea/ticketopia/internal/useractivity"
)

func TestEventCommunitySeparatesPublicCountsFromViewerState(t *testing.T) {
	a, account, recs, browser, bearer, _, id := recommendationAPI(t)
	activity := &eventActivityStub{}
	WithEventActivity(activity)(a)
	owner := account.account.ID
	other := accounts.ID()
	interestRepo := &interestStub{items: map[string]map[string]interests.Item{
		owner: {id: {Event: models.Event{ID: id}, Visibility: "private"}},
		other: {id: {Event: models.Event{ID: id}, Visibility: "private"}},
	}}
	a.interests = interests.New(interestRepo, a.saved)
	recs.items[owner] = map[string]recommendations.Item{id: {Event: models.Event{ID: id}}}
	if _, _, err := a.saved.Save(t.Context(), owner, id); err != nil {
		t.Fatal(err)
	}
	path := "/api/v1/events/" + id + "/community"

	read := func(cookie, token string) (int, map[string]json.RawMessage, string) {
		w := accountRequest(a, "GET", path, "", cookie, token, "", "", "")
		if w.Header().Get("Cache-Control") != "private, no-store" {
			t.Fatal("community state is cacheable", w.Header().Get("Cache-Control"))
		}
		var body map[string]json.RawMessage
		_ = json.Unmarshal(w.Body.Bytes(), &body)
		return w.Code, body, w.Body.String()
	}

	code, body, raw := read("", "")
	if code != 200 || string(body["viewer"]) != "null" || !strings.Contains(raw, `"counts":{"interested":2,"recommendations":1,"discussions":2}`) {
		t.Fatal("anonymous read lost counts or invented a viewer", code, raw)
	}
	for _, credential := range [][2]string{{browser, ""}, {"", bearer}} {
		code, body, raw = read(credential[0], credential[1])
		if code != 200 || string(body["viewer"]) != `{"saved":true,"interested":true,"interest_visibility":"private","recommended":true}` {
			t.Fatal("signed-in viewer state wrong", credential, code, raw)
		}
		if !strings.Contains(raw, `"interested":2`) {
			t.Fatal("viewer read changed public counts", raw)
		}
	}
	if strings.Contains(raw, other) || strings.Contains(raw, account.account.Email) {
		t.Fatal("aggregate leaked identities", raw)
	}

	// Another signed-in person sees the same totals and only their own state.
	account.account.ID = accounts.ID()
	if _, body, raw = read("", bearer); string(body["viewer"]) != `{"saved":false,"interested":false,"recommended":false}` {
		t.Fatal("viewer state crossed accounts", raw)
	}
	account.account.ID = owner

	for _, tc := range []struct {
		name, path, token string
		status            int
	}{
		{"rejected bearer is not anonymous", path, "not-a-token", 401},
		{"query rejected", path + "?limit=1", "", 400},
		{"invalid event", "/api/v1/events/invalid/community", "", 400},
	} {
		if w := accountRequest(a, "GET", tc.path, "", "", tc.token, "", "", ""); w.Code != tc.status {
			t.Fatal(tc.name, w.Code, w.Body.String())
		}
	}
	activity.fail = true
	if code, _, raw = read("", ""); code != 503 || strings.Contains(raw, `"counts"`) {
		t.Fatal("failed counts were fabricated", code, raw)
	}
	activity.fail = false
	interestRepo.unavailable = true
	if code, _, _ = read("", ""); code != 503 {
		t.Fatal("failed interest totals were fabricated", code)
	}
	a.eventActivity = nil
	if code, _, _ = read("", ""); code != 503 {
		t.Fatal("unwired activity reported availability", code)
	}
}

type userActivityStub struct {
	owner string
	query useractivity.Query
	items []useractivity.Item
	calls int
}

func (r *userActivityStub) List(_ context.Context, owner string, q useractivity.Query) ([]useractivity.Item, error) {
	r.calls++
	r.owner, r.query = owner, q
	return r.items, nil
}

func TestUserActivityPaginatesPublicTimeline(t *testing.T) {
	a, account, _, _, _ := accountAPI(t)
	owner := account.account.ID
	at := time.Date(2026, 11, 1, 12, 0, 0, 0, time.UTC)
	event := models.Event{ID: "ticketmaster:Timeline", Name: "Timeline show"}
	post := accounts.ID()
	repo := &userActivityStub{items: []useractivity.Item{
		{Type: useractivity.TypeRecommendation, At: at, Event: event, Recommendation: &useractivity.Recommendation{Reason: "Great venue", RecommendedAt: at, UpdatedAt: at}},
		{Type: useractivity.TypeDiscussionPost, At: at, Event: event, Post: &useractivity.Post{ID: post, ThreadID: post, Body: "Doors at 7?", CreatedAt: at, UpdatedAt: at}},
		{Type: useractivity.TypeInterest, At: at.Add(-time.Hour), Event: event},
	}}
	WithUserActivity(repo)(a)
	path := "/api/v1/users/" + owner + "/activity"

	w := accountRequest(a, "GET", path+"?limit=2", "", "", "", "", "", "")
	var page useractivity.List
	if w.Code != 200 || w.Header().Get("Cache-Control") != "private, no-store" || json.Unmarshal(w.Body.Bytes(), &page) != nil {
		t.Fatal("activity page failed", w.Code, w.Body.String())
	}
	if repo.owner != owner || repo.query.Limit != 2 || len(page.Items) != 2 || page.NextCursor == nil || page.Items[1].Post == nil || page.Items[1].Post.Body != "Doors at 7?" {
		t.Fatal("activity page shape wrong", repo.owner, repo.query, w.Body.String())
	}
	if strings.Contains(w.Body.String(), account.account.Email) || strings.Contains(w.Body.String(), "Secret city") {
		t.Fatal("private account fields leaked", w.Body.String())
	}

	// The cursor resumes after the last item in (time, type, key) order.
	w = accountRequest(a, "GET", path+"?limit=2&cursor="+*page.NextCursor, "", "", "", "", "", "")
	c := repo.query.Cursor
	if w.Code != 200 || c == nil || !c.At.Equal(at) || c.Rank != useractivity.Rank(useractivity.TypeDiscussionPost) || c.ID != post {
		t.Fatal("cursor did not round-trip", w.Code, c)
	}

	calls := repo.calls
	for _, tc := range []struct {
		name, path string
		status     int
	}{
		{"cursor bound to another profile", "/api/v1/users/" + accounts.ID() + "/activity?cursor=" + *page.NextCursor, 400},
		{"limit bound", path + "?limit=101", 400},
		{"unknown filter", path + "?type=interest", 400},
		{"repeated limit", path + "?limit=1&limit=2", 400},
		{"malformed cursor", path + "?cursor=not!base64", 400},
		{"unknown profile", "/api/v1/users/" + accounts.ID() + "/activity", 404},
	} {
		if w := accountRequest(a, "GET", tc.path, "", "", "", "", "", ""); w.Code != tc.status {
			t.Fatal(tc.name, w.Code, w.Body.String())
		}
	}
	if repo.calls != calls {
		t.Fatal("rejected requests read activity")
	}

	a.userActivity = nil
	if w := accountRequest(a, "GET", path, "", "", "", "", "", ""); w.Code != 503 {
		t.Fatal("unwired activity reported availability", w.Code)
	}
}

// Agents get exactly what the HTTP routes return, including the token
// account's own state, through the same shared reads.
func TestCommunityToolsMatchHTTPRoutes(t *testing.T) {
	a, account, recs, _, bearer, _, id := recommendationAPI(t)
	WithEventActivity(&eventActivityStub{})(a)
	WithUserActivity(&userActivityStub{items: []useractivity.Item{{Type: useractivity.TypeInterest, At: time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC), Event: models.Event{ID: id}}}})(a)
	a.runCtx = t.Context()
	WithMCP(appmcp.Config{Enabled: true, RatePerMinute: 600, MaxInFlight: 8})(a)
	routes := a.Routes()
	owner := account.account.ID
	recs.items[owner] = map[string]recommendations.Item{id: {Event: models.Event{ID: id}, Profile: account.account.Public(), Reason: "Worth it", RecommendedAt: time.Date(2026, 11, 2, 0, 0, 0, 0, time.UTC)}}
	if w := accountRequest(a, "PUT", "/api/v1/me/event-interests/"+id, `{"visibility":"public"}`, "", bearer, "", "", ""); w.Code != 201 {
		t.Fatal("public interest fixture", w.Code, w.Body.String())
	}
	for _, tc := range []struct {
		tool, args, path string
	}{
		{"event_community", `{"event_id":"` + id + `"}`, "/api/v1/events/" + id + "/community"},
		{"profile_activity", `{"user_id":"` + owner + `"}`, "/api/v1/users/" + owner + "/activity"},
		{"profile_activity", `{}`, "/api/v1/users/" + owner + "/activity"},
		{"event_recommendations", `{"event_id":"` + id + `","limit":1}`, "/api/v1/events/" + id + "/recommendations?limit=1"},
		{"event_interested_people", `{"event_id":"` + id + `"}`, "/api/v1/events/" + id + "/interested-users"},
	} {
		http := accountRequest(a, "GET", tc.path, "", "", bearer, "", "", "")
		response := mcpPost(t, routes, `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"`+tc.tool+`","arguments":`+tc.args+`}}`,
			map[string]string{"Authorization": "Bearer " + bearer})
		var reply struct {
			Result struct {
				IsError           bool            `json:"isError"`
				StructuredContent json.RawMessage `json:"structuredContent"`
			} `json:"result"`
		}
		if http.Code != 200 || response.Code != 200 || json.Unmarshal(response.Body.Bytes(), &reply) != nil || reply.Result.IsError {
			t.Fatalf("%s %s: http %d, mcp %d %s", tc.tool, tc.args, http.Code, response.Code, response.Body.String())
		}
		var fromHTTP, fromTool any
		_ = json.Unmarshal(http.Body.Bytes(), &fromHTTP)
		_ = json.Unmarshal(reply.Result.StructuredContent, &fromTool)
		if !reflect.DeepEqual(fromHTTP, fromTool) {
			t.Fatalf("%s differs from %s:\n tool %s\n http %s", tc.tool, tc.path, reply.Result.StructuredContent, http.Body.String())
		}
	}
}

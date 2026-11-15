package appmcp

import (
	"context"
	"encoding/json"
	"net/url"
	"strings"
	"testing"

	"github.com/sonastea/ticketopia/internal/accounts"
	"github.com/sonastea/ticketopia/internal/discussions"
	"github.com/sonastea/ticketopia/internal/eventactivity"
	"github.com/sonastea/ticketopia/internal/interests"
	"github.com/sonastea/ticketopia/internal/models"
	"github.com/sonastea/ticketopia/internal/recommendations"
	"github.com/sonastea/ticketopia/internal/useractivity"
)

type fakeCommunity struct{ *fakeAccount }

func (f fakeCommunity) EventCommunity(_ context.Context, viewer, id string) (eventactivity.Community, error) {
	f.record("community.Event " + viewer + " " + id)
	if id == "ticketmaster:broken" {
		return eventactivity.Community{}, eventactivity.ErrUnavailable
	}
	out := eventactivity.Community{EventID: id, Counts: eventactivity.CommunityCounts{Interested: 4, Recommendations: 2, Discussions: 1}}
	if viewer != "" {
		out.Viewer = &eventactivity.CommunityViewer{Saved: true, Interested: true, InterestVisibility: "private"}
	}
	return out, nil
}

func (f fakeCommunity) ProfileActivity(_ context.Context, user string, v url.Values) (useractivity.List, error) {
	f.record("community.Profile " + user + " " + v.Encode())
	if user == "missing" {
		return useractivity.List{}, accounts.ErrNotFound
	}
	return useractivity.List{Items: []useractivity.Item{{Type: useractivity.TypeInterest}}}, nil
}

func (f fakeCommunity) Event(_ context.Context, id string, v url.Values) (recommendations.List, error) {
	f.record("recommendations.Event " + id + " " + v.Encode())
	if id == "ticketmaster:broken" {
		return recommendations.List{}, recommendations.ErrUnavailable
	}
	count := 1
	return recommendations.List{Items: []recommendations.Item{{Event: models.Event{ID: id}, Profile: accounts.PublicProfile{ID: "author-id", DisplayName: "Author"}, Reason: "Great"}}, Count: &count}, nil
}

func (f fakeCommunity) Participants(_ context.Context, id string, v url.Values) (interests.Participants, error) {
	f.record("interests.Participants " + id + " " + v.Encode())
	if id == "ticketmaster:broken" {
		return interests.Participants{}, interests.ErrUnavailable
	}
	return interests.Participants{Items: []interests.Participant{{Profile: accounts.PublicProfile{ID: "fan-id", DisplayName: "Fan"}}}}, nil
}

func (f fakeCommunity) List(_ context.Context, viewer, event, thread string, v url.Values) (discussions.List, error) {
	f.record("discussions.List " + viewer + " " + event + " " + thread + " " + v.Encode())
	if event == "ticketmaster:broken" {
		return discussions.List{}, discussions.ErrUnavailable
	}
	return discussions.List{Items: []discussions.Post{{ID: "post-id", EventID: event, ThreadID: "post-id", Body: "Doors?"}}, Count: 1}, nil
}

func (f fakeCommunity) Thread(_ context.Context, viewer, event, id string) (discussions.Post, error) {
	f.record("discussions.Thread " + viewer + " " + event + " " + id)
	if event == "ticketmaster:broken" {
		return discussions.Post{}, discussions.ErrUnavailable
	}
	return discussions.Post{ID: id, EventID: event, ThreadID: id, Body: "Doors?"}, nil
}

// Each public read passes its arguments through unchanged, works without a
// token, and reports service errors in the account API's wording.
func TestPublicEventReadsPassArgumentsAndWordErrors(t *testing.T) {
	f := &fakeAccount{}
	base := privateServer(t, f).URL
	for _, tc := range []struct {
		tool string
		args map[string]any
		want string
	}{
		{"event_recommendations", map[string]any{"event_id": "ticketmaster:x", "limit": 2, "cursor": "c"}, "recommendations.Event ticketmaster:x cursor=c&limit=2"},
		{"event_interested_people", map[string]any{"event_id": "ticketmaster:x", "limit": 3}, "interests.Participants ticketmaster:x limit=3"},
		{"event_discussions", map[string]any{"event_id": "ticketmaster:x"}, "discussions.List  ticketmaster:x  "},
		{"discussion_thread", map[string]any{"event_id": "ticketmaster:x", "thread_id": "t1"}, "discussions.Thread  ticketmaster:x t1|discussions.List  ticketmaster:x t1 "},
	} {
		f.calls = nil
		reply := tool(t, base, "", tc.tool, tc.args)
		if reply.Result.IsError || strings.Join(f.calls, "|") != tc.want {
			t.Fatalf("%s: %+v calls %q", tc.tool, reply.Result, f.calls)
		}
		broken := map[string]any{}
		for k, v := range tc.args {
			broken[k] = v
		}
		broken["event_id"] = "ticketmaster:broken"
		if reply := tool(t, base, "owner-token", tc.tool, broken); !reply.Result.IsError || !strings.HasPrefix(reply.Result.Content[0].Text, "private: ") {
			t.Fatalf("%s error not worded as the API: %+v", tc.tool, reply.Result)
		}
	}
}

// Community reads are public: anonymous callers get the same totals without a
// viewer; signed in, the viewer is the token's account and only theirs.
func TestCommunityToolsArePublicAndScopeViewerToCaller(t *testing.T) {
	f := &fakeAccount{}
	base := privateServer(t, f).URL
	read := func(token string) eventactivity.Community {
		t.Helper()
		reply := tool(t, base, token, "event_community", map[string]any{"event_id": "ticketmaster:x"})
		var out eventactivity.Community
		if reply.Result.IsError || json.Unmarshal(reply.Result.StructuredContent, &out) != nil {
			t.Fatalf("event_community %q: %+v", token, reply.Result)
		}
		return out
	}
	if out := read(""); out.Viewer != nil || out.Counts.Interested != 4 || out.Counts.Discussions != 1 {
		t.Fatalf("anonymous read: %+v", out)
	}
	if out := read("reader-token"); out.Viewer == nil || !out.Viewer.Saved || out.Counts.Interested != 4 {
		t.Fatalf("signed-in read: %+v", out)
	}
	if reply := tool(t, base, "", "event_community", map[string]any{"event_id": "ticketmaster:broken"}); !reply.Result.IsError ||
		!strings.HasPrefix(reply.Result.Content[0].Text, "private: ") {
		t.Fatalf("service error not worded as the API: %+v", reply.Result)
	}

	if reply := tool(t, base, "", "profile_activity", map[string]any{}); !reply.Result.IsError || !strings.Contains(reply.Result.Content[0].Text, "/users/<id>") {
		t.Fatalf("anonymous own-profile read: %+v", reply.Result)
	}
	tool(t, base, "", "profile_activity", map[string]any{"user_id": "someone", "limit": 5})
	tool(t, base, "reader-token", "profile_activity", map[string]any{"cursor": "next"})
	if reply := tool(t, base, "", "profile_activity", map[string]any{"user_id": "missing"}); !reply.Result.IsError {
		t.Fatalf("missing profile: %+v", reply.Result)
	}
	want := []string{"community.Event  ticketmaster:x", "community.Event owner-id ticketmaster:x", "community.Event  ticketmaster:broken",
		"community.Profile someone limit=5", "community.Profile owner-id cursor=next", "community.Profile missing "}
	if strings.Join(f.calls, "|") != strings.Join(want, "|") {
		t.Fatalf("calls %q", f.calls)
	}

	f.calls = nil
	tool(t, base, "reader-token", "event_discussions", map[string]any{"event_id": "ticketmaster:x"})
	if len(f.calls) != 1 || f.calls[0] != "discussions.List owner-id ticketmaster:x  " {
		t.Fatalf("signed-in discussions read lost the viewer: %q", f.calls)
	}
	for _, listed := range rpc(t, base, "", "tools/list", map[string]any{}).Result.Tools {
		if (listed.Name == "event_community" || listed.Name == "profile_activity") && !listed.Annotations.ReadOnlyHint {
			t.Fatalf("%s is not read-only", listed.Name)
		}
	}
}

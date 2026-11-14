package appmcp

import (
	"context"
	"encoding/json"
	"net/url"
	"strings"
	"testing"

	"github.com/sonastea/ticketopia/internal/accounts"
	"github.com/sonastea/ticketopia/internal/eventactivity"
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

	for _, listed := range rpc(t, base, "", "tools/list", map[string]any{}).Result.Tools {
		if (listed.Name == "event_community" || listed.Name == "profile_activity") && !listed.Annotations.ReadOnlyHint {
			t.Fatalf("%s is not read-only", listed.Name)
		}
	}
}

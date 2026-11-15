package appmcp

import (
	"context"
	"net/url"
	"strings"
	"testing"

	"github.com/sonastea/ticketopia/internal/discussionfollows"
)

type fakeFollowed struct{ *fakeAccount }

func (f fakeFollowed) List(_ context.Context, who string, v url.Values) (discussionfollows.Page[discussionfollows.Follow], error) {
	f.record("follows.List " + who + " " + v.Encode())
	return discussionfollows.Page[discussionfollows.Follow]{Items: []discussionfollows.Follow{{EventID: "ticketmaster:x", ThreadID: "t", Frequency: "daily"}}}, nil
}

func (f fakeFollowed) Notifications(_ context.Context, who string, v url.Values) (discussionfollows.Page[discussionfollows.Notification], error) {
	f.record("follows.Notifications " + who + " " + v.Encode())
	return discussionfollows.Page[discussionfollows.Notification]{Items: []discussionfollows.Notification{{ID: "n", EventID: "ticketmaster:x"}}}, nil
}

// Followed-discussion tools act only for the token's account and need sign-in.
func TestFollowedDiscussionToolsUseTheTokenAccount(t *testing.T) {
	f := &fakeAccount{}
	base := privateServer(t, f).URL
	for _, tc := range []struct {
		tool string
		args map[string]any
		want string
	}{
		{"my_followed_discussions", map[string]any{"limit": 3}, "follows.List owner-id limit=3"},
		{"my_notifications", map[string]any{"cursor": "c"}, "follows.Notifications owner-id cursor=c"},
	} {
		if reply := tool(t, base, "", tc.tool, tc.args); !reply.Result.IsError || !strings.Contains(reply.Result.Content[0].Text, "Authorization: Bearer") {
			t.Fatalf("%s without token: %+v", tc.tool, reply.Result)
		}
		f.calls = nil
		if reply := tool(t, base, "reader-token", tc.tool, tc.args); reply.Result.IsError || strings.Join(f.calls, "|") != tc.want {
			t.Fatalf("%s: %+v calls %q", tc.tool, reply.Result, f.calls)
		}
	}
}

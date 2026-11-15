package appmcp

import (
	"context"
	"net/url"
	"strings"
	"testing"

	"github.com/sonastea/ticketopia/internal/accounts"
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

func (f fakeFollowed) Read(_ context.Context, who, id string) error {
	f.record("follows.Read " + who + " " + id)
	return nil
}

// Signed-in discussion and preference tools act only for the token's account
// and need sign-in.
func TestFollowedDiscussionToolsUseTheTokenAccount(t *testing.T) {
	f := &fakeAccount{account: accounts.Account{Email: "private@example.com", Preferences: accounts.Preferences{City: "Chicago", Country: "US"}}}
	base := privateServer(t, f).URL
	for _, tc := range []struct {
		tool string
		args map[string]any
		want string
	}{
		{"my_followed_discussions", map[string]any{"limit": 3}, "follows.List owner-id limit=3"},
		{"my_notifications", map[string]any{"cursor": "c"}, "follows.Notifications owner-id cursor=c"},
		{"mark_notification_read", map[string]any{"notification_id": "n1"}, "follows.Read owner-id n1"},
		{"my_preferences", map[string]any{}, "accounts.Account owner-id"},
	} {
		if reply := tool(t, base, "", tc.tool, tc.args); !reply.Result.IsError || !strings.Contains(reply.Result.Content[0].Text, "Authorization: Bearer") {
			t.Fatalf("%s without token: %+v", tc.tool, reply.Result)
		}
		f.calls = nil
		token := "reader-token"
		if SignedInTools[tc.tool] == ScopeWrite {
			// A read-only connection is refused before the service is reached.
			if reply := tool(t, base, token, tc.tool, tc.args); !reply.Result.IsError || len(f.calls) != 0 {
				t.Fatalf("%s with read only: %+v calls %q", tc.tool, reply.Result, f.calls)
			}
			token = "owner-token"
		}
		if reply := tool(t, base, token, tc.tool, tc.args); reply.Result.IsError || strings.Join(f.calls, "|") != tc.want {
			t.Fatalf("%s: %+v calls %q", tc.tool, reply.Result, f.calls)
		}
	}
	reply := tool(t, base, "reader-token", "my_preferences", map[string]any{})
	if body := string(reply.Result.StructuredContent); !strings.Contains(body, `"city":"Chicago"`) || strings.Contains(body, "private@example.com") {
		t.Fatalf("my_preferences: %s", body)
	}
}

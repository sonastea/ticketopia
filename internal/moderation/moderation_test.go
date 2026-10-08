package moderation

import (
	"context"
	"errors"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/sonastea/ticketopia/internal/accounts"
)

type accessStub struct {
	Repository
	allowed bool
	err     error
}

func (r accessStub) IsModerator(context.Context, string) (bool, error) { return r.allowed, r.err }
func TestAuthorizationFailsClosedBeforePrivateReads(t *testing.T) {
	who, post := accounts.ID(), accounts.ID()
	for _, tc := range []struct {
		repo accessStub
		want error
	}{{accessStub{}, ErrForbidden}, {accessStub{err: ErrUnavailable}, ErrUnavailable}} {
		s := New(tc.repo)
		if _, err := s.Queue(t.Context(), who, nil); !errors.Is(err, tc.want) {
			t.Fatal(err)
		}
		if _, err := s.Case(t.Context(), who, post, nil); !errors.Is(err, tc.want) {
			t.Fatal(err)
		}
		if _, err := s.Decide(t.Context(), who, post, Review{}); !errors.Is(err, tc.want) {
			t.Fatal(err)
		}
	}
	if err := New(accessStub{allowed: true}).RequireModerator(t.Context(), ""); !errors.Is(err, accounts.ErrUnauthenticated) {
		t.Fatal(err)
	}
}
func TestModerationTextAndInputValidation(t *testing.T) {
	for _, text := range []string{strings.Repeat("界", MaxText+1), "\x00", "\x7f", string([]byte{0xff})} {
		if _, err := Text("context", text, false); err == nil {
			t.Fatal("invalid text accepted")
		}
	}
	if text, err := Text("context", "  First\r\nsecond  ", true); err != nil || text != "First\nsecond" {
		t.Fatal(text, err)
	}
	if _, err := Text("reason", " \n ", true); err == nil {
		t.Fatal("empty required text")
	}
	s := New(accessStub{allowed: true})
	who, post := accounts.ID(), accounts.ID()
	for _, r := range []Review{{Action: "remove", Reason: "x"}, {Action: "hide", Reason: "x"}, {Action: "hide", Reason: " ", ExpectedVersion: new(int64), ExpectedUpdatedAt: time.Now()}} {
		if _, err := s.Decide(t.Context(), who, post, r); err == nil {
			t.Fatal("invalid review accepted")
		}
	}
	for _, reason := range []string{"", "downvote", "spam\n"} {
		if _, _, err := s.Report(t.Context(), who, post, reason, ""); err == nil {
			t.Fatal("invalid reason")
		}
	}
}
func TestPrivateCollectionCursorsBindViewerAndCollection(t *testing.T) {
	scope := "reports:" + accounts.ID()
	q, err := ParseQuery(url.Values{"limit": {"1"}}, scope, false)
	if err != nil {
		t.Fatal(err)
	}
	items := []Report{{ID: accounts.ID(), CreatedAt: time.Now().UTC()}, {ID: accounts.ID(), CreatedAt: time.Now().UTC().Add(-time.Hour)}}
	result := page(items, q, scope, func(r Report) string { return r.ID }, func(r Report) time.Time { return r.CreatedAt })
	if len(result.Items) != 1 || result.NextCursor == nil {
		t.Fatal(result)
	}
	values := url.Values{"cursor": {*result.NextCursor}}
	parsed, err := ParseQuery(values, scope, false)
	if err != nil || parsed.After != items[0].ID || !parsed.At.Equal(items[0].CreatedAt) {
		t.Fatal(parsed, err)
	}
	for _, other := range []string{"reports:" + accounts.ID(), "outcomes:" + strings.TrimPrefix(scope, "reports:")} {
		if _, err := ParseQuery(values, other, false); err == nil {
			t.Fatal("cross-scope cursor")
		}
	}
	for _, bad := range []url.Values{{"limit": {"0"}}, {"limit": {"101"}}, {"limit": {"1", "2"}}, {"owner": {"x"}}, {"state": {"all"}}, {"cursor": {""}}} {
		if _, err := ParseQuery(bad, scope, false); err == nil {
			t.Fatal("invalid query", bad)
		}
	}
	if _, err := ParseQuery(url.Values{"state": {"all"}}, "queue", true); err != nil {
		t.Fatal(err)
	}
}

package discussionfollows

import (
	"context"
	"errors"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/sonastea/ticketopia/internal/accounts"
)

func TestDigestBoundaries(t *testing.T) {
	for _, tc := range []struct{ frequency, at, want string }{
		{"immediate", "2026-10-08T17:12:13Z", "2026-10-08T17:12:13Z"},
		{"daily", "2026-10-08T23:59:59Z", "2026-10-09T00:00:00Z"},
		{"weekly", "2026-10-11T23:59:59Z", "2026-10-12T00:00:00Z"},
		{"weekly", "2026-10-12T00:00:00Z", "2026-10-19T00:00:00Z"},
		{"daily", "2026-12-31T20:00:00-05:00", "2027-01-02T00:00:00Z"},
	} {
		t.Run(tc.frequency+tc.at, func(t *testing.T) {
			at, _ := time.Parse(time.RFC3339, tc.at)
			if got := DeliveryAt(tc.frequency, at).Format(time.RFC3339); got != tc.want {
				t.Fatal(got, tc.want)
			}
		})
	}
}

type fixture struct {
	Repository
	items []Notification
	who   string
}

func (f *fixture) Notifications(_ context.Context, who string, q Query) ([]Notification, error) {
	f.who = who
	return f.items, nil
}
func TestPrivatePaginationAndReplyLinks(t *testing.T) {
	who := accounts.ID()
	at := time.Now().UTC()
	repo := &fixture{items: []Notification{{ID: accounts.ID(), EventID: "ticketmaster:Event", ThreadID: accounts.ID(), PostID: accounts.ID(), AvailableAt: at}, {ID: accounts.ID(), AvailableAt: at.Add(-time.Second)}}}
	s := New(repo)
	list, err := s.Notifications(t.Context(), who, url.Values{"limit": {"1"}})
	if err != nil || len(list.Items) != 1 || list.NextCursor == nil || repo.who != who {
		t.Fatal(list, err)
	}
	n := list.Items[0]
	u, _ := url.Parse(n.URL)
	if u.Path != "/events/ticketmaster:Event/discussions/"+n.ThreadID || u.Query().Get("post_id") != n.PostID || u.Fragment != "post-"+n.PostID {
		t.Fatal("wrong reply link", n.URL)
	}
	if _, err = s.Notifications(t.Context(), accounts.ID(), url.Values{"cursor": {*list.NextCursor}}); err == nil {
		t.Fatal("cross-account cursor accepted")
	}
	if _, err = s.List(t.Context(), who, url.Values{"cursor": {*list.NextCursor}}); err == nil {
		t.Fatal("cross-collection cursor accepted")
	}
	if _, err = s.Notifications(t.Context(), "", nil); !errors.Is(err, accounts.ErrUnauthenticated) {
		t.Fatal(err)
	}
	for _, values := range []url.Values{{"limit": {"0"}}, {"limit": {"101"}}, {"limit": {"1", "2"}}, {"owner": {"other"}}, {"cursor": {strings.Repeat("a", 1025)}}} {
		if _, err := ParseQuery(values, "scope"); err == nil {
			t.Fatal("invalid query", values)
		}
	}
	for _, frequency := range []string{"", "hourly", "DAILY"} {
		if Frequency(frequency) == nil {
			t.Fatal("invalid frequency", frequency)
		}
	}
}

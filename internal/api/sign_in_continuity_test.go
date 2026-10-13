package api

import (
	"html"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/sonastea/ticketopia/internal/models"
	"github.com/sonastea/ticketopia/internal/saved"
)

func TestSignInCancellationAndStartFailureKeepEventJourney(t *testing.T) {
	a, repo, _, _, _ := accountAPI(t)
	id := "ticketmaster:resume"
	snapshots := &savedStub{items: map[string]map[string]saved.Item{}, detail: models.EventDetail{Item: models.Event{ID: id, Name: "A <special> night"}, Meta: models.Freshness{DataAsOf: time.Now()}}}
	a.saved = saved.New(snapshots, nil)
	origin := "/?city=Berlin&cursor=next&limit=2"
	target := "/events/" + id + "?section=community&recommend=true&return_to=" + url.QueryEscape(origin)
	for _, tc := range []struct {
		path    string
		limited bool
		status  int
	}{
		{"/auth/sign-in?cancelled=true&return_to=", false, 200},
		{"/auth/google/start?return_to=", true, 429},
	} {
		repo.limited = tc.limited
		w := accountRequest(a, "GET", tc.path+url.QueryEscape(target), "", "", "", "", "", "")
		markup := w.Body.String()
		for _, want := range []string{`href="` + html.EscapeString(target) + `"`, `return_to=` + url.QueryEscape(target), "A &lt;special&gt; night", "Recommendations", "Choose your action there."} {
			if !strings.Contains(markup, want) {
				t.Errorf("missing context %q: %s", want, markup)
			}
		}
		if w.Code != tc.status || w.Header().Get("Cache-Control") != "private, no-store" || len(snapshots.items) != 0 || !repo.account.Preferences.NotificationsPaused || repo.account.InterestVisibility != "private" {
			t.Fatal("sign-in changed state or lost HTTP contract", w.Code)
		}
		if tc.limited && w.Header().Get("Retry-After") != "600" {
			t.Fatal("start failure lost retry guidance")
		}
	}
}

func TestSignInPreservesPaginatedSavedReturn(t *testing.T) {
	target := "/saved?limit=1&cursor=next"
	a, _, _, _, _ := accountAPI(t)
	w := accountRequest(a, "GET", target, "", "", "", "", "", "")
	if w.Code != 303 || w.Header().Get("Location") != "/auth/sign-in?return_to="+url.QueryEscape(target) {
		t.Fatal("guest Saved redirect dropped collection page", w.Code, w.Header())
	}
	if got := safeAuthReturn(target); got != target {
		t.Fatalf("sign-in dropped saved page: %s", got)
	}
	for _, unsafe := range []string{"/saved?return_to=https://evil.example", "/saved?cursor=x&cursor=y", "//evil.example/saved?cursor=next"} {
		if got := safeAuthReturn(unsafe); got == unsafe || strings.Contains(got, "evil.example") {
			t.Fatal("unsafe saved return", got)
		}
	}
}

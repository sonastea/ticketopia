package api

import (
	"context"
	"html"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/sonastea/ticketopia/internal/accounts"
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

// flowStub completes OAuth locally so the callback's save intent can be observed.
type flowStub struct {
	*accountStub
	flows map[[32]byte]accounts.Flow
}

func (r *flowStub) PutFlow(_ context.Context, f accounts.Flow) error {
	r.flows[f.StateHash] = f
	return nil
}
func (r *flowStub) ConsumeFlow(_ context.Context, state, browser [32]byte, _ time.Time) (accounts.Flow, error) {
	f, ok := r.flows[state]
	delete(r.flows, state)
	if !ok || f.BrowserHash != browser {
		return accounts.Flow{}, accounts.ErrFlow
	}
	return f, nil
}
func (r *flowStub) Login(context.Context, accounts.Identity, accounts.Credential) (accounts.Account, error) {
	return r.account, nil
}

type providerStub struct{}

func (providerStub) AuthorizationURL(l accounts.Login) string {
	return "https://accounts.google.com/auth?state=" + url.QueryEscape(l.State)
}
func (providerStub) Verify(context.Context, string, accounts.Flow) (accounts.Identity, error) {
	return accounts.Identity{Issuer: accounts.GoogleIssuer, Subject: "subject", Email: "private@example.com"}, nil
}

func TestSignInCompletesRequestedSave(t *testing.T) {
	id := "ticketmaster:resume"
	target := "/?city=Berlin&selected_event=" + url.QueryEscape(id)
	for _, tc := range []struct {
		name, query string
		saved       bool
	}{
		{"save intent", "&intent=save", true},
		{"no intent", "", false},
		{"unknown intent", "&intent=interest", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a, repo, _, _, _ := accountAPI(t)
			flows := &flowStub{repo, map[[32]byte]accounts.Flow{}}
			a.accounts = accounts.New(flows, providerStub{})
			snapshots := &savedStub{items: map[string]map[string]saved.Item{}, detail: models.EventDetail{Item: models.Event{ID: id, Name: "Resume show"}, Meta: models.Freshness{DataAsOf: time.Now()}}}
			a.saved = saved.New(snapshots, nil)

			page := accountRequest(a, "GET", "/auth/sign-in?return_to="+url.QueryEscape(target)+tc.query, "", "", "", "", "", "")
			if strings.Contains(page.Body.String(), "save this event to your private Saved list") != tc.saved {
				t.Fatal("sign-in page misdescribes what sign-in completes", page.Body.String())
			}
			start := accountRequest(a, "GET", "/auth/google/start?return_to="+url.QueryEscape(target)+tc.query, "", "", "", "", "", "")
			location, _ := url.Parse(start.Header().Get("Location"))
			callback := httptest.NewRequest("GET", "/auth/google/callback?code=provider-code&state="+url.QueryEscape(location.Query().Get("state")), nil)
			for _, cookie := range start.Result().Cookies() {
				if cookie.Value != "" {
					callback.AddCookie(cookie)
				}
			}
			w := httptest.NewRecorder()
			a.Routes().ServeHTTP(w, callback)
			if w.Code != 303 || w.Header().Get("Location") != target {
				t.Fatal("sign-in lost its destination", w.Code, w.Header())
			}
			if _, ok := snapshots.items[repo.account.ID][id]; ok != tc.saved {
				t.Fatalf("saved = %v, want %v", ok, tc.saved)
			}
			if repo.account.Preferences.NotificationsPaused != true || repo.account.InterestVisibility != "private" {
				t.Fatal("save intent changed unrelated account state")
			}
		})
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

package api

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/rs/zerolog"
	"github.com/sonastea/ticketopia/internal/accounts"
	"github.com/sonastea/ticketopia/internal/discovery"
	"github.com/sonastea/ticketopia/internal/discussionfollows"
	"github.com/sonastea/ticketopia/internal/kv"
	"github.com/sonastea/ticketopia/internal/moderation"
	"github.com/sonastea/ticketopia/internal/recommendations"
)

func TestSelectedEventFailureDoesNotReplaceResults(t *testing.T) {
	fixture := categoryFixture(t)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "/events/missing") {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		if strings.Contains(r.URL.Path, "classifications") {
			_, _ = w.Write(fixture)
			return
		}
		_, _ = fmt.Fprint(w, `{"page":{"number":0,"size":20,"totalElements":1,"totalPages":1},"_embedded":{"events":[{"id":"available","name":"Available show","dates":{"status":{"code":"canceled"}},"priceRanges":[{"currency":"USD","min":20,"max":40}]}]}}`)
	}))
	defer upstream.Close()
	cache := kv.NewMemory()
	defer cache.Close()
	a := &api{events: discovery.New(t.Context(), cache, zerolog.Nop(), discovery.Config{APIKey: "test", BaseURL: upstream.URL})}
	for _, tc := range []struct {
		path   string
		status int
		text   string
	}{
		{"/?city=Boston&selected_event=ticketmaster:missing", 200, "Available show"},
		{"/events/ticketmaster:missing", 404, "Event unavailable"},
		{"/events/ticketmaster:available", 200, "Canceled"},
		{"/events/ticketmaster:available?section=unsupported", 400, "choose overview, discussion, or community"},
		{"/api/v1/events?city=Boston&selected_event=ticketmaster:available", 400, "invalid_query"},
		{"/?city=Boston&selected_event=a&selected_event=b", 400, "use one value"},
	} {
		rec := httptest.NewRecorder()
		a.Routes().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, tc.path, nil))
		if rec.Code != tc.status || !strings.Contains(rec.Body.String(), tc.text) {
			t.Fatalf("%s: got %d, missing %q", tc.path, rec.Code, tc.text)
		}
		if tc.path == "/events/ticketmaster:available" {
			body := rec.Body.String()
			actions := strings.Index(body, `data-event-actions`)
			if price := strings.Index(body, "USD 20"); price < 0 || price >= actions || strings.Index(body, "Canceled") >= actions {
				t.Fatal("decision-critical price/cancellation must precede participation controls")
			}
		}
	}
}

func TestAccountFamilyNavigationRoutesAndRoles(t *testing.T) {
	a, _, browser, _, _, _ := discussionAPI(t)
	a.discussionFollows = discussionfollows.New(&discussionFollowStub{})
	repo := &moderationStub{}
	a.moderation = moderation.New(repo)
	for _, moderator := range []bool{false, true} {
		repo.allowed = moderator
		for _, path := range []string{"/me", "/me/preferences", "/me/interests", "/follows", "/radar", "/me/reminders", "/me/discussions", "/me/notifications"} {
			w := accountRequest(a, "GET", path, "", browser, "", "", "", "")
			body := w.Body.String()
			if !strings.Contains(body, `class="account-navigation"`) || !strings.Contains(body, `href="`+path+`" aria-current="page"`) || !strings.Contains(body, "More in your space") {
				t.Fatalf("%s lost current family navigation: %d %s", path, w.Code, body)
			}
			if !strings.Contains(body, `href="/me/reports"`) || strings.Contains(body, `href="/moderation"`) != moderator {
				t.Fatalf("%s changed Safety role gate", path)
			}
			if w.Header().Get("Cache-Control") != "private, no-store" {
				t.Fatalf("%s lost private response", path)
			}
		}
	}
}

func TestEventSectionFragments(t *testing.T) {
	a, posts, browser, bearer, _, id := discussionAPI(t)
	if w := discussionRequest(a, "POST", "/api/v1/events/"+id+"/discussions", `{"body":"Section question"}`, bearer, accounts.ID()); w.Code != 201 {
		t.Fatal(w.Code, w.Body.String())
	}
	recs := &recommendationStub{items: map[string]map[string]recommendations.Item{}, profiles: map[string]accounts.PublicProfile{}}
	a.recommendations = recommendations.New(recs, a.saved)
	for _, cookie := range []string{"", browser} {
		for _, panel := range []bool{false, true} {
			for _, section := range []string{"overview", "discussion", "community"} {
				posts.listCalls, recs.listCalls = 0, 0
				r := httptest.NewRequest("GET", "/events/"+id+"?section="+section+"&return_to=%2Fsaved", nil)
				r.Header.Set("X-Ticketopia-Section", "true")
				if panel {
					r.Header.Set("X-Ticketopia-Panel", "true")
				}
				if cookie != "" {
					r.AddCookie(&http.Cookie{Name: a.authConfig.CookieName("session"), Value: cookie})
				}
				w := httptest.NewRecorder()
				a.Routes().ServeHTTP(w, r)
				body := w.Body.String()
				mode := "standalone"
				if panel {
					mode = "context"
				}
				for _, want := range []string{`data-event-section-region`, `data-event-id="` + id + `"`, `data-section="` + section + `"`, `data-presentation="` + mode + `"`, `data-event-shell-state`, `data-action-return=`} {
					if w.Code != 200 || !strings.Contains(body, want) {
						t.Fatalf("%s/%s: %d missing %s", section, mode, w.Code, want)
					}
				}
				if strings.Contains(body, "<html") || strings.Count(body, "data-event-section-region") != 1 || w.Header().Get("Cache-Control") != "private, no-store" || w.Header().Get("Vary") != "X-Ticketopia-Panel, X-Ticketopia-Section" {
					t.Fatal("invalid fragment boundary", w.Header())
				}
				if (posts.listCalls > 0) != (section == "discussion") || (recs.listCalls > 0) != (section == "community") {
					t.Fatalf("%s fetched unrelated lists: discussions=%d recommendations=%d", section, posts.listCalls, recs.listCalls)
				}
				if section == "overview" && !strings.Contains(body, "data-recommend-intent") {
					t.Fatal("Overview lost recommendation capability")
				}
				if strings.Contains(body, "data-save-form") != (cookie != "") {
					t.Fatal("fragment retained another viewer's private controls")
				}
			}
		}
	}
	for _, query := range []string{"?section=wrong", "?section=overview&section=discussion", "?recommend=true", "?unknown=true"} {
		r := httptest.NewRequest("GET", "/events/"+id+query, nil)
		r.Header.Set("X-Ticketopia-Section", "true")
		w := httptest.NewRecorder()
		a.Routes().ServeHTTP(w, r)
		if w.Code != 400 || strings.Contains(w.Body.String(), "<html") || strings.Contains(w.Body.String(), "data-event-section-region") {
			t.Fatal("fragment validation status lost", query, w.Code)
		}
	}
}

func TestReturnContextCannotNavigateOutsideDiscovery(t *testing.T) {
	for _, raw := range []string{"https://evil.example/", "//evil.example/", "/events/ticketmaster:other", "/?selected_event=other", "/?city=A&city=B", "/?city=%zz", "/#external", "/?cursor=invalid"} {
		if got := safeReturnURL(raw); got != "/" {
			t.Errorf("accepted unsafe/invalid return context %q: %q", raw, got)
		}
	}
	valid := "/?city=Boston&country=US&keyword=jazz"
	if got := safeReturnURL(valid); got != valid {
		t.Errorf("lost valid discovery filters: %q", got)
	}
}

func TestDisabledDestinationsRemainTruthful(t *testing.T) {
	a := &api{}
	for _, path := range []string{"/saved", "/community", "/me", "/me/interests"} {
		availability := "In development"
		if path == "/me" || path == "/me/interests" || path == "/saved" {
			availability = "Accounts not enabled"
		} else if path == "/community" {
			availability = "require accounts to be enabled"
		}
		rec := httptest.NewRecorder()
		a.Routes().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
		if rec.Code != 200 || !strings.Contains(rec.Body.String(), availability) || !strings.Contains(rec.Body.String(), "Discover events") {
			t.Errorf("planned destination %s is missing its availability/recovery state", path)
		}
	}
}

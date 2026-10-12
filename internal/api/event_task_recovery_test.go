package api

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/labstack/echo/v4"
	"github.com/sonastea/ticketopia/internal/accounts"
)

func TestNativeParticipationFailureKeepsSafeEventContext(t *testing.T) {
	a, _, _, _, _ := accountAPI(t)
	for _, route := range []string{"/saved/ticketmaster:recovery", "/interests/ticketmaster:recovery"} {
		for _, origin := range []string{"/events/ticketmaster:recovery?section=discussion&return_to=%2Fsaved", "https://evil.example/", "//evil.example/"} {
			body := url.Values{"action": {"remove"}, "return_to": {origin}}
			w := accountRequest(a, "POST", route, body.Encode(), "", "", "", "", "application/x-www-form-urlencoded")
			if w.Code != 401 || !strings.Contains(w.Body.String(), "Event request couldn't finish") {
				t.Fatalf("%s: expected contextual authentication failure: %d %s", route, w.Code, w.Body.String())
			}
			markup := w.Body.String()
			markup = markup[strings.Index(markup, "<main"):strings.Index(markup, "</main>")]
			if strings.Contains(markup, "evil.example") || strings.Contains(markup, `href="/me"`) || strings.Contains(markup, "Return to your profile") {
				t.Fatal("unsafe or unrelated recovery destination", markup)
			}
			if strings.HasPrefix(origin, "/events/") && (!strings.Contains(markup, "return_to="+url.QueryEscape(origin)) || !strings.Contains(markup, "section=discussion")) {
				t.Fatal("lost event/section sign-in continuity", markup)
			}
		}
	}
}

func TestNativeParticipationRecoveryPreservesErrorContracts(t *testing.T) {
	for _, route := range []string{"/saved/ticketmaster:recovery", "/interests/ticketmaster:recovery"} {
		for _, tc := range []struct {
			name   string
			status int
		}{
			{"csrf", http.StatusForbidden},
			{"unavailable", http.StatusServiceUnavailable},
		} {
			t.Run(route+"/"+tc.name, func(t *testing.T) {
				a, _, browser, _, csrf := accountAPI(t)
				if tc.name == "csrf" {
					csrf = "expired"
				}
				origin := "/saved?limit=1&cursor=next"
				body := url.Values{"action": {"remove"}, "return_to": {origin}, "csrf_token": {csrf}}
				w := accountRequest(a, "POST", route, body.Encode(), browser, "", "", "", "application/x-www-form-urlencoded")
				if w.Code != tc.status || w.Header().Get("Cache-Control") != "private, no-store" || !strings.Contains(w.Body.String(), `href="/saved?limit=1&amp;cursor=next"`) {
					t.Fatalf("lost error contract or collection recovery: %d %v %s", w.Code, w.Header(), w.Body.String())
				}
				if strings.Contains(w.Body.String(), "Sign in to continue here") {
					t.Fatal("authenticated failure should not prompt sign-in")
				}
			})
		}
	}
}

func TestEventTaskRecoveryKeepsRetryHint(t *testing.T) {
	a, _, _, _, _ := accountAPI(t)
	w := httptest.NewRecorder()
	c := echo.New().NewContext(httptest.NewRequest("GET", "/", nil), w)
	if err := a.eventTaskError(c, accounts.ErrLimited, "/community?city=Berlin"); err != nil {
		t.Fatal(err)
	}
	if w.Code != http.StatusTooManyRequests || w.Header().Get("Retry-After") != "600" || !strings.Contains(w.Body.String(), `href="/community?city=Berlin"`) {
		t.Fatalf("lost retry hint or task context: %d %v", w.Code, w.Header())
	}
}

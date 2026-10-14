package api

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/labstack/echo/v4"
	"github.com/rs/zerolog"
	"github.com/sonastea/ticketopia/internal/accounts"
	"github.com/sonastea/ticketopia/internal/discovery"
	"github.com/sonastea/ticketopia/internal/location"
)

// HTTP tests use a narrow repository stub, never a runtime in-memory auth mode.
type accountStub struct {
	accounts.Repository
	account     accounts.Account
	credentials map[[32]byte]accounts.Credential
	lastOwner   string
	unavailable bool
	limited     bool
	authCalls   int
}

func (r *accountStub) Authenticate(_ context.Context, h [32]byte, kind string, now time.Time) (accounts.Account, accounts.Credential, error) {
	r.authCalls++
	if r.unavailable {
		return accounts.Account{}, accounts.Credential{}, errors.New("db-password-must-not-leak")
	}
	c, ok := r.credentials[h]
	if !ok || c.Kind != kind || !now.Before(c.ExpiresAt) {
		return accounts.Account{}, c, accounts.ErrUnauthenticated
	}
	return r.account, c, nil
}

func TestRequestAuthenticationReuseAndIsolation(t *testing.T) {
	a, repo, browser, bearer, _ := accountAPI(t)
	repo.authCalls = 0
	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.AddCookie(&http.Cookie{Name: a.authConfig.CookieName("session"), Value: browser})
	req.Header.Set("Authorization", "Bearer "+bearer)
	c := e.NewContext(req, httptest.NewRecorder())
	for range 2 {
		p, err := a.principal(c, false)
		if err != nil || p.Credential.Kind != "api" {
			t.Fatal(p, err)
		}
		if _, err := a.principal(c, true); !errors.Is(err, accounts.ErrUnauthenticated) {
			t.Fatal("browser accepted bearer", err)
		}
		p, err = a.browserPrincipal(c)
		if err != nil || p.Credential.Kind != "session" {
			t.Fatal(p, err)
		}
	}
	if repo.authCalls != 2 {
		t.Fatalf("authentication calls = %d, want 2", repo.authCalls)
	}
	repo.credentials = nil
	c = e.NewContext(req, httptest.NewRecorder())
	if _, err := a.principal(c, false); !errors.Is(err, accounts.ErrUnauthenticated) {
		t.Fatal("revoked credential reused across requests", err)
	}
}
func (r *accountStub) Get(_ context.Context, id string) (accounts.Account, error) {
	if id != r.account.ID {
		return accounts.Account{}, accounts.ErrNotFound
	}
	return r.account, nil
}
func (r *accountStub) UpdateProfile(_ context.Context, id string, p accounts.ProfilePatch) (accounts.Account, error) {
	r.lastOwner = id
	if p.DisplayName != nil {
		r.account.DisplayName = *p.DisplayName
	}
	if p.Bio != nil {
		r.account.Bio = *p.Bio
	}
	if p.InterestVisibility != nil {
		r.account.InterestVisibility = *p.InterestVisibility
	}
	return r.account, nil
}
func (r *accountStub) UpdatePreferences(_ context.Context, id string, p accounts.PreferencesPatch) (accounts.Account, error) {
	r.lastOwner = id
	v := &r.account.Preferences
	if p.City != nil {
		v.City = *p.City
	}
	if p.Country != nil {
		v.Country = *p.Country
	}
	if p.Timezone != nil {
		v.Timezone = *p.Timezone
	}
	if p.CategoryIDs != nil {
		v.CategoryIDs = *p.CategoryIDs
	}
	if p.EmailReminders != nil {
		v.EmailReminders = *p.EmailReminders
	}
	if p.WeeklyDigest != nil {
		v.WeeklyDigest = *p.WeeklyDigest
	}
	if p.NotificationsPaused != nil {
		v.NotificationsPaused = *p.NotificationsPaused
	}
	return r.account, nil
}

func TestHTMLPreferencesPreserveStoredDigestIncludingValidation(t *testing.T) {
	for _, initial := range []bool{false, true} {
		a, repo, browser, bearer, csrf := accountAPI(t)
		repo.account.Preferences.WeeklyDigest = initial
		for _, zone := range []string{"America/Chicago", "Not/AZone"} {
			body := url.Values{"city": {"Chicago"}, "country": {"US"}, "timezone": {zone}, "email_reminders": {"on"}, "notifications_paused": {"on"}, "csrf_token": {csrf}}
			w := accountRequest(a, "POST", "/me/preferences", body.Encode(), browser, "", "", "", "application/x-www-form-urlencoded")
			want := 303
			if zone == "Not/AZone" {
				want = 400
			}
			if w.Code != want || repo.account.Preferences.WeeklyDigest != initial {
				t.Fatal("HTML changed digest", w.Code, w.Body.String())
			}
			if want == 400 && (strings.Contains(w.Body.String(), `name="weekly_digest"`) || !strings.Contains(w.Body.String(), `value="Not/AZone"`)) {
				t.Fatal("validation lost form contract")
			}
		}
		w := accountRequest(a, "POST", "/me/preferences", "timezone=UTC&weekly_digest=on&csrf_token="+csrf, browser, "", "", "", "application/x-www-form-urlencoded")
		if w.Code != 400 || repo.account.Preferences.WeeklyDigest != initial {
			t.Fatal("hidden/client digest trusted")
		}
		w = accountRequest(a, "POST", "/me/preferences", "section=interests&csrf_token="+csrf, browser, "", "", "", "application/x-www-form-urlencoded")
		if w.Code != 303 || repo.account.Preferences.WeeklyDigest != initial {
			t.Fatal("category save changed digest")
		}
		// The separately supported JSON preference contract still changes digest.
		w = accountRequest(a, "PATCH", "/api/v1/me/preferences", `{"weekly_digest":true}`, "", bearer, "", "", "")
		if w.Code != 200 || !repo.account.Preferences.WeeklyDigest {
			t.Fatal("JSON digest contract changed", w.Code)
		}
	}
}
func (r *accountStub) ListTokens(_ context.Context, id string, now time.Time) ([]accounts.Credential, error) {
	result := []accounts.Credential{}
	for _, c := range r.credentials {
		if c.AccountID == id && c.Kind == "api" && now.Before(c.ExpiresAt) {
			result = append(result, c)
		}
	}
	return result, nil
}
func (r *accountStub) CreateToken(_ context.Context, c accounts.Credential, _ time.Time) error {
	r.credentials[c.Hash] = c
	return nil
}
func (r *accountStub) Revoke(_ context.Context, owner, id string) error {
	for hash, c := range r.credentials {
		if c.AccountID == owner && c.ID == id {
			delete(r.credentials, hash)
		}
	}
	return nil
}
func (r *accountStub) RevokeAll(_ context.Context, id string) error {
	for hash, c := range r.credentials {
		if c.AccountID == id {
			delete(r.credentials, hash)
		}
	}
	return nil
}
func (r *accountStub) Allow(context.Context, [32]byte, time.Time, time.Duration, int) (bool, error) {
	return !r.limited, nil
}

func accountAPI(t *testing.T) (*api, *accountStub, string, string, string) {
	t.Helper()
	owner := accounts.ID()
	browser := base64.RawURLEncoding.EncodeToString(bytes.Repeat([]byte{1}, 32))
	bearer := base64.RawURLEncoding.EncodeToString(bytes.Repeat([]byte{2}, 32))
	r := &accountStub{account: accounts.Account{ID: owner, DisplayName: "Event explorer", Email: "private@example.com", InterestVisibility: "private", Preferences: accounts.Preferences{City: "Secret city", Timezone: "UTC", CategoryIDs: []string{}, NotificationsPaused: true}}, credentials: make(map[[32]byte]accounts.Credential)}
	for raw, kind := range map[string]string{browser: "session", bearer: "api"} {
		hash := accounts.Hash(raw)
		r.credentials[hash] = accounts.Credential{ID: accounts.ID(), AccountID: owner, Hash: hash, Kind: kind, Name: kind, CreatedAt: time.Now().UTC(), ExpiresAt: time.Now().Add(time.Hour)}
	}
	s := accounts.New(r, nil)
	a := &api{accounts: s, authConfig: accounts.Config{Enabled: true, BaseURL: "https://events.example", ClientID: "test", ClientSecret: "test"}}
	p, err := s.Authenticate(t.Context(), browser, "session")
	if err != nil {
		t.Fatal(err)
	}
	return a, r, browser, bearer, p.CSRF
}
func accountRequest(a *api, method, path, body, cookie, bearer, csrf, origin, media string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, path, strings.NewReader(body))
	if media == "" {
		media = "application/json"
	}
	r.Header.Set("Content-Type", media)
	if cookie != "" {
		r.AddCookie(&http.Cookie{Name: a.authConfig.CookieName("session"), Value: cookie})
	}
	if bearer != "" {
		r.Header.Set("Authorization", "Bearer "+bearer)
	}
	if csrf != "" {
		r.Header.Set("X-CSRF-Token", csrf)
	}
	if origin != "" {
		r.Header.Set("Origin", origin)
	}
	w := httptest.NewRecorder()
	a.Routes().ServeHTTP(w, r)
	return w
}
func TestAccountAuthenticationPrivacyAndValidation(t *testing.T) {
	a, repo, browser, bearer, csrf := accountAPI(t)
	for _, tc := range []struct {
		name, method, path, body, cookie, token, csrf, origin string
		status                                                int
	}{
		{"guest", "GET", "/api/v1/me", "", "", "", "", "", 401},
		{"bad cookie", "GET", "/api/v1/me", "", "junk", "", "", "", 401},
		{"session is not API token", "GET", "/api/v1/me", "", "", browser, "", "", 401},
		{"API token is not cookie", "GET", "/api/v1/me", "", bearer, "", "", "", 401},
		{"private cookie read", "GET", "/api/v1/me", "", browser, "", "", "", 200},
		{"private bearer read", "GET", "/api/v1/me", "", "", bearer, "", "", 200},
		{"cookie write needs CSRF", "PATCH", "/api/v1/me/profile", `{"display_name":"Pal"}`, browser, "", "", "", 403},
		{"wrong origin", "PATCH", "/api/v1/me/profile", `{"display_name":"Pal"}`, browser, "", csrf, "https://attacker.example", 403},
		{"owner cannot be supplied", "PATCH", "/api/v1/me/profile", `{"user_id":"another","display_name":"Pal"}`, browser, "", csrf, "", 400},
		{"null rejected", "PATCH", "/api/v1/me/profile", `{"bio":null}`, browser, "", csrf, "", 400},
		{"duplicate field rejected", "PATCH", "/api/v1/me/profile", `{"bio":"one","bio":"two"}`, browser, "", csrf, "", 400},
		{"empty patch rejected", "PATCH", "/api/v1/me/profile", `{}`, browser, "", csrf, "", 400},
		{"nonobject rejected", "PATCH", "/api/v1/me/profile", `[]`, browser, "", csrf, "", 400},
		{"unknown visibility rejected", "PATCH", "/api/v1/me/profile", `{"interest_visibility":"friends"}`, browser, "", csrf, "", 400},
		{"large body rejected", "PATCH", "/api/v1/me/profile", `{"bio":"` + strings.Repeat("x", 17000) + `"}`, browser, "", csrf, "", 400},
		{"valid cookie write", "PATCH", "/api/v1/me/profile", `{"display_name":"Concert pal","bio":"<script>alert(1)</script>"}`, browser, "", csrf, "https://events.example", 200},
		{"valid bearer write", "PATCH", "/api/v1/me/preferences", `{"city":"Berlin","country":"de","timezone":"Europe/Berlin","category_ids":["sports","music","sports"]}`, "", bearer, "", "", 200},
		{"bearer cannot mint tokens", "POST", "/api/v1/me/tokens", `{"name":"unexpected"}`, "", bearer, "", "", 401},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w := accountRequest(a, tc.method, tc.path, tc.body, tc.cookie, tc.token, tc.csrf, tc.origin, "")
			if w.Code != tc.status {
				t.Fatalf("%d: %s", w.Code, w.Body.String())
			}
			if w.Header().Get("Cache-Control") != "private, no-store" {
				t.Fatal("private response cacheable")
			}
			if tc.status >= 400 && !strings.HasPrefix(w.Header().Get("Content-Type"), "application/problem+json") {
				t.Fatal("missing problem response")
			}
		})
	}
	if repo.lastOwner != repo.account.ID || repo.account.Preferences.Country != "DE" || len(repo.account.Preferences.CategoryIDs) != 2 {
		t.Fatal("owner/normalization lost")
	}
	for _, path := range []string{"/api/v1/users/" + repo.account.ID, "/users/" + repo.account.ID} {
		w := accountRequest(a, "GET", path, "", "", "", "", "", "")
		if w.Code != 200 || strings.Contains(w.Body.String(), repo.account.Email) || strings.Contains(w.Body.String(), "Europe/Berlin") || strings.Contains(w.Body.String(), "Secret city") {
			t.Fatal("public projection leaked private data", w.Body.String())
		}
		if path[1:4] != "api" && strings.Contains(w.Body.String(), "<script>alert") {
			t.Fatal("unescaped public bio")
		}
	}
	repo.unavailable = true
	w := accountRequest(a, "GET", "/api/v1/me", "", browser, "", "", "", "")
	if w.Code != 503 || strings.Contains(w.Body.String(), "db-password-must-not-leak") {
		t.Fatal("database failure not closed/redacted")
	}
}
func TestAccountTokenAndSignOutJourneys(t *testing.T) {
	a, repo, browser, _, csrf := accountAPI(t)
	w := accountRequest(a, "POST", "/api/v1/me/tokens", `{"name":"Personal app"}`, browser, "", csrf, "", "")
	if w.Code != 201 {
		t.Fatal(w.Code, w.Body.String())
	}
	var issued struct {
		Item  accounts.Credential `json:"item"`
		Token string              `json:"token"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &issued); err != nil || issued.Token == "" || issued.Item.Name != "Personal app" {
		t.Fatal("no credential issued", err)
	}
	w = accountRequest(a, "GET", "/api/v1/me/tokens", "", browser, "", "", "", "")
	if w.Code != 200 || strings.Contains(w.Body.String(), issued.Token) || strings.Contains(w.Body.String(), "token_hash") {
		t.Fatal("token secret exposed on listing")
	}
	w = accountRequest(a, "GET", "/api/v1/me", "", "", issued.Token, "", "", "")
	if w.Code != 200 || strings.Contains(w.Body.String(), "csrf_token") {
		t.Fatal("API token not usable/separated")
	}
	w = accountRequest(a, "DELETE", "/api/v1/me/tokens/"+accounts.ID(), "", browser, "", csrf, "", "")
	if w.Code != 204 {
		t.Fatal("foreign/missing token must be an idempotent no-op")
	}
	w = accountRequest(a, "DELETE", "/api/v1/me/tokens/"+issued.Item.ID, "", browser, "", csrf, "", "")
	if w.Code != 204 {
		t.Fatal(w.Code)
	}
	w = accountRequest(a, "GET", "/api/v1/me", "", "", issued.Token, "", "", "")
	if w.Code != 401 {
		t.Fatal("revoked token accepted")
	}
	form := url.Values{"csrf_token": {csrf}, "display_name": {"No-JS pal"}, "bio": {"A regular form"}, "interest_visibility": {"private"}}
	w = accountRequest(a, "POST", "/me/profile?csrf_token="+url.QueryEscape(csrf), form.Encode(), browser, "", "", "https://events.example", "application/x-www-form-urlencoded")
	if w.Code != 400 {
		t.Fatal("form accepted query parameters")
	}
	w = accountRequest(a, "POST", "/me/profile", form.Encode(), browser, "", "", "https://events.example", "application/x-www-form-urlencoded")
	if w.Code != 303 || repo.account.DisplayName != "No-JS pal" {
		t.Fatal("ordinary profile form failed", w.Code, w.Body.String())
	}
	w = accountRequest(a, "POST", "/auth/logout-all", url.Values{"csrf_token": {csrf}}.Encode(), browser, "", "", "https://events.example", "application/x-www-form-urlencoded")
	if w.Code != 303 || len(repo.credentials) != 0 {
		t.Fatal("sign out everywhere didn't revoke all credentials")
	}
	w = accountRequest(a, "GET", "/api/v1/me", "", browser, "", "", "", "")
	if w.Code != 401 {
		t.Fatal("logged-out session accepted")
	}
}

func TestRateLimitedRequestsReturnRetryAfterWithoutIssuingCredentials(t *testing.T) {
	for _, tc := range []struct {
		name, method, path, media string
	}{
		{"login start", "GET", "/auth/google/start", ""},
		{"login callback", "GET", "/auth/google/callback?state=invalid&code=unused", ""},
		{"API token creation", "POST", "/api/v1/me/tokens", "application/json"},
		{"HTML token creation", "POST", "/me/tokens", "application/x-www-form-urlencoded"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a, repo, browser, _, csrf := accountAPI(t)
			repo.limited = true
			body := `{"name":"Blocked token"}`
			if tc.media == "application/x-www-form-urlencoded" {
				body = url.Values{"name": {"Blocked token"}, "csrf_token": {csrf}}.Encode()
			}
			before := len(repo.credentials)
			w := accountRequest(a, tc.method, tc.path, body, browser, "", csrf, "", tc.media)
			if w.Code != http.StatusTooManyRequests || w.Header().Get("Retry-After") != "600" || w.Header().Get("Cache-Control") != "private, no-store" {
				t.Fatalf("rate limit response: %d %v %s", w.Code, w.Header(), w.Body.String())
			}
			if len(repo.credentials) != before || len(w.Result().Cookies()) != 0 || w.Header().Get("Location") != "" {
				t.Fatal("rate-limited request issued credentials or redirected")
			}
			if tc.media == "application/json" && (!strings.HasPrefix(w.Header().Get("Content-Type"), "application/problem+json") || !strings.Contains(w.Body.String(), `"code":"rate_limited"`)) {
				t.Fatal("API lost the rate-limit error contract")
			}
		})
	}
}

func TestDeleteSessionRevokesOnlyTheCurrentCredential(t *testing.T) {
	for _, kind := range []string{"session", "api"} {
		t.Run(kind, func(t *testing.T) {
			a, repo, browser, bearer, csrf := accountAPI(t)
			cookie, token, raw, other := browser, "", browser, bearer
			if kind == "api" {
				cookie, token, raw, other = "", bearer, bearer, browser
			} else {
				w := accountRequest(a, "DELETE", "/api/v1/me/session", "", cookie, token, "", "", "")
				if w.Code != 403 || len(repo.credentials) != 2 {
					t.Fatal("session deletion bypassed CSRF", w.Code)
				}
			}
			w := accountRequest(a, "DELETE", "/api/v1/me/session", "", cookie, token, csrf, "", "")
			if w.Code != 204 {
				t.Fatal(w.Code, w.Body.String())
			}
			if _, exists := repo.credentials[accounts.Hash(raw)]; exists {
				t.Fatal("current credential remained active")
			}
			if _, exists := repo.credentials[accounts.Hash(other)]; !exists {
				t.Fatal("deletion revoked an unrelated credential")
			}
			cookies := w.Result().Cookies()
			if kind == "session" {
				if len(cookies) != 1 || cookies[0].Name != a.authConfig.CookieName("session") || cookies[0].Value != "" || cookies[0].MaxAge != -1 {
					t.Fatal("browser session cookie was not cleared", cookies)
				}
			} else if len(cookies) != 0 {
				t.Fatal("API-token revocation changed browser cookies")
			}
		})
	}
}
func TestAccountSignInReturnAndCookiePolicy(t *testing.T) {
	for _, raw := range []string{"https://attacker.example/", "//attacker.example/", "/\\attacker.example/", "/auth/google/start", "/events/ticketmaster:x#outside", "/%2f%2fattacker.example/"} {
		if got := safeAuthReturn(raw); got != "/me" {
			t.Errorf("unsafe return %q -> %q", raw, got)
		}
	}
	for _, raw := range []string{"/me/preferences", "/me/interests", "/?city=Berlin&selected_event=ticketmaster:x&section=discussion", "/events/ticketmaster:x?section=overview&return_to=%2F%3Fcity%3DBerlin"} {
		if got := safeAuthReturn(raw); got != raw {
			t.Errorf("lost return %q -> %q", raw, got)
		}
	}
	a, _, _, _, _ := accountAPI(t)
	w := accountRequest(a, "GET", "/me/interests", "", "", "", "", "", "")
	if w.Code != 303 || !strings.Contains(w.Header().Get("Location"), "return_to=%2Fme%2Finterests") {
		t.Fatal("guest return task lost")
	}
	c := a.Routes().NewContext(httptest.NewRequest("GET", "/", nil), httptest.NewRecorder())
	a.cookie(c, "session", "token", time.Hour)
	for _, cookie := range c.Response().Header().Values("Set-Cookie") {
		if !strings.Contains(cookie, "__Host-ticketopia_session=") || !strings.Contains(cookie, "Secure") || !strings.Contains(cookie, "HttpOnly") || !strings.Contains(cookie, "SameSite=Lax") || strings.Contains(cookie, "Domain=") {
			t.Fatal("unsafe production cookie", cookie)
		}
	}
	a.accounts = nil
	w = accountRequest(a, "GET", "/api/v1/me", "", "", "", "", "", "")
	if w.Code != 503 {
		t.Fatal("disabled auth API must fail closed")
	}
}

func TestAccountDefaultLocationPrecedenceAndStorageFailure(t *testing.T) {
	a, repo, browser, _, _ := accountAPI(t)
	a.locations = location.New(t.Context(), nil, zerolog.Nop(), location.Config{Disabled: true})
	repo.account.Preferences.City, repo.account.Preferences.Country = "Berlin", "DE"
	for _, tc := range []struct {
		path                    string
		remembered, unavailable bool
		city, source            string
	}{
		{"/", false, false, "Berlin", "account"},
		{"/?city=Paris&country=FR", true, false, "Paris", "search"},
		{"/", true, false, "London", "saved"},
		{"/?city=", true, false, "", ""},
		{"/", false, true, "", ""},
	} {
		repo.unavailable = tc.unavailable
		req := httptest.NewRequest("GET", tc.path, nil)
		req.AddCookie(&http.Cookie{Name: a.authConfig.CookieName("session"), Value: browser})
		ctx := a.Routes().NewContext(req, httptest.NewRecorder())
		if tc.remembered {
			encoded, _ := json.Marshal(location.City{Name: "London", Country: "GB"})
			req.AddCookie(&http.Cookie{Name: cityCookie, Value: base64.RawURLEncoding.EncodeToString(encoded)})
		}
		query, err := discovery.ParseQuery(ctx.QueryParams(), time.Now())
		if err != nil {
			t.Fatal(err)
		}
		source, err := a.locateSearch(ctx, &query)
		if err != nil || query.City != tc.city || source != tc.source {
			t.Fatalf("%s: city=%s source=%s err=%v", tc.path, query.City, source, err)
		}
	}
}

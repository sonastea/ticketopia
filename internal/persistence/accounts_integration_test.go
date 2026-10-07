package persistence

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"testing/fstest"
	"time"

	"github.com/rs/zerolog"
	"github.com/sonastea/ticketopia/internal/accounts"
	"github.com/sonastea/ticketopia/internal/api"
	"github.com/sonastea/ticketopia/internal/kv"
	"github.com/sonastea/ticketopia/internal/models"
)

type accountProviderFixture struct{}

func (accountProviderFixture) AuthorizationURL(l accounts.Login) string {
	return "https://accounts.example/authorize?state=" + l.State
}
func (accountProviderFixture) Verify(_ context.Context, code string, _ accounts.Flow) (accounts.Identity, error) {
	return accounts.Identity{Issuer: accounts.GoogleIssuer, Subject: code, Email: "same-email@example.com"}, nil
}
func loginAccount(t *testing.T, s *accounts.Service, subject string) (accounts.Account, string) {
	t.Helper()
	l, _, err := s.Begin(t.Context(), "/me")
	if err != nil {
		t.Fatal(err)
	}
	a, raw, returnTo, err := s.Complete(t.Context(), l.State, l.Browser, subject)
	if err != nil || returnTo != "/me" {
		t.Fatal("account login", err)
	}
	return a, raw
}
func pointer[T any](v T) *T { return &v }

func TestMariaDBAccountIdentityConcurrencyAndRestart(t *testing.T) {
	f := newMaria(t)
	f.migrate(t)
	pools := []*Pool{f.open(t), f.open(t), f.open(t)}
	services := []*accounts.Service{}
	for _, p := range pools {
		services = append(services, accounts.New(p.Accounts(), accountProviderFixture{}))
	}
	var wg sync.WaitGroup
	errs := make(chan error, 12)
	ids := make(chan string, 12)
	for worker := range 12 {
		wg.Go(func() {
			s := services[worker%3]
			l, _, err := s.Begin(t.Context(), "/me")
			if err == nil {
				var a accounts.Account
				a, _, _, err = s.Complete(t.Context(), l.State, l.Browser, "StableCaseSubject")
				if err == nil {
					ids <- a.ID
				}
			}
			if err != nil {
				errs <- err
			}
		})
	}
	wg.Wait()
	close(errs)
	close(ids)
	for err := range errs {
		t.Fatal(err)
	}
	firstID := ""
	for id := range ids {
		if firstID == "" {
			firstID = id
		} else if id != firstID {
			t.Fatal("concurrent first sign-ins duplicated identity")
		}
	}
	a, raw := loginAccount(t, services[0], "StableCaseSubject")
	other, _ := loginAccount(t, services[1], "stablecasesubject")
	if a.ID == other.ID || a.Email != other.Email || a.InterestVisibility != "private" || a.DisplayName != "Event explorer" || !a.Preferences.NotificationsPaused {
		t.Fatal("identity/case/private defaults failed")
	}
	var identities, users, sessions int
	if err := pools[0].db.QueryRowContext(t.Context(), `SELECT (SELECT COUNT(*) FROM account_identities),(SELECT COUNT(*) FROM accounts),(SELECT COUNT(*) FROM account_credentials WHERE account_id=? AND kind='session')`, a.ID).Scan(&identities, &users, &sessions); err != nil || identities != 2 || users != 2 || sessions != 10 {
		t.Fatal("identity or bounded session count", identities, users, sessions, err)
	}
	if _, err := services[1].UpdateProfile(t.Context(), a.ID, accounts.ProfilePatch{DisplayName: pointer("Local pal"), Bio: pointer("No provider dependency"), InterestVisibility: pointer("private")}); err != nil {
		t.Fatal(err)
	}
	if _, err := services[1].UpdatePreferences(t.Context(), a.ID, accounts.PreferencesPatch{City: pointer("Berlin"), Country: pointer("de"), Timezone: pointer("Europe/Berlin"), CategoryIDs: pointer([]string{"sports", "music"})}); err != nil {
		t.Fatal(err)
	}
	_, apiToken, err := services[0].CreateToken(t.Context(), a.ID, "Personal client")
	if err != nil {
		t.Fatal(err)
	}
	if err := pools[0].Close(); err != nil {
		t.Fatal(err)
	}
	restarted := f.open(t)
	s := accounts.New(restarted.Accounts(), accountProviderFixture{})
	for token, kind := range map[string]string{raw: "session", apiToken: "api"} {
		principal, err := s.Authenticate(t.Context(), token, kind)
		if err != nil || principal.Account.ID != a.ID || principal.Account.DisplayName != "Local pal" || principal.Account.Preferences.City != "Berlin" || len(principal.Account.Preferences.CategoryIDs) != 2 {
			t.Fatal("account/credentials/preferences did not survive restart", err)
		}
	}
	public, err := s.Public(t.Context(), a.ID)
	data, _ := json.Marshal(public)
	if err != nil || strings.Contains(string(data), a.Email) || strings.Contains(string(data), "Berlin") {
		t.Fatal("public privacy lost", err)
	}
	// Session and API secret values must not be interchangeable or stored raw.
	if _, err := s.Authenticate(t.Context(), apiToken, "session"); !errors.Is(err, accounts.ErrUnauthenticated) {
		t.Fatal("API token used as browser session", err)
	}
	if _, _, err := restarted.Accounts().Authenticate(t.Context(), accounts.Hash(apiToken), "api", time.Now().Add(accounts.CredentialLifetime+time.Second)); !errors.Is(err, accounts.ErrUnauthenticated) {
		t.Fatal("expired token accepted", err)
	}
	var stored []byte
	hash := accounts.Hash(apiToken)
	if err := restarted.db.QueryRowContext(t.Context(), `SELECT token_hash FROM account_credentials WHERE token_hash=?`, hash[:]).Scan(&stored); err != nil || len(stored) != 32 || string(stored) == apiToken {
		t.Fatal("raw token storage", err)
	}
	if err := s.RevokeAll(t.Context(), a.ID); err != nil {
		t.Fatal(err)
	}
	for token, kind := range map[string]string{raw: "session", apiToken: "api"} {
		if _, err := services[2].Authenticate(t.Context(), token, kind); !errors.Is(err, accounts.ErrUnauthenticated) {
			t.Fatal("cross-pool revocation failed", err)
		}
	}
	for _, query := range []string{`DELETE FROM accounts`, `UPDATE account_identities SET subject='reassigned'`, `UPDATE account_credentials SET token_hash=REPEAT('x',32)`} {
		if _, err := restarted.db.ExecContext(t.Context(), query); err == nil {
			t.Fatal("runtime privilege escalation", query)
		}
	}
}

func TestMariaDBOneTimeBrowserBoundFlowsAndSharedLimits(t *testing.T) {
	f := newMaria(t)
	f.migrate(t)
	p1, p2 := f.open(t), f.open(t)
	s := accounts.New(p1.Accounts(), accountProviderFixture{})
	l, _, err := s.Begin(t.Context(), "/events/ticketmaster:pending")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := p2.Accounts().ConsumeFlow(t.Context(), accounts.Hash(l.State), accounts.Hash("another browser"), time.Now()); !errors.Is(err, accounts.ErrFlow) {
		t.Fatal("wrong browser consumed flow", err)
	}
	var successes atomic.Int32
	var wg sync.WaitGroup
	for _, r := range []*AccountRepository{p1.Accounts(), p2.Accounts()} {
		wg.Go(func() {
			_, err := r.ConsumeFlow(t.Context(), accounts.Hash(l.State), accounts.Hash(l.Browser), time.Now())
			if err == nil {
				successes.Add(1)
			} else if !errors.Is(err, accounts.ErrFlow) {
				t.Error(err)
			}
		})
	}
	wg.Wait()
	if successes.Load() != 1 {
		t.Fatal("one-time flow was replayable")
	}
	l, _, err = s.Begin(t.Context(), "/me")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := p1.Accounts().ConsumeFlow(t.Context(), accounts.Hash(l.State), accounts.Hash(l.Browser), time.Now().Add(accounts.FlowLifetime+time.Second)); !errors.Is(err, accounts.ErrFlow) {
		t.Fatal("expired flow accepted", err)
	}
	key := accounts.Hash("rate fixture")
	var allowed atomic.Int32
	for i := range 30 {
		wg.Go(func() {
			p := p1
			if i%2 == 0 {
				p = p2
			}
			ok, err := p.Accounts().Allow(t.Context(), key, time.Now(), 10*time.Minute, 20)
			if err != nil {
				t.Error(err)
			}
			if ok {
				allowed.Add(1)
			}
		})
	}
	wg.Wait()
	if allowed.Load() != 20 {
		t.Fatal("shared rate limit", allowed.Load())
	}
	a, _ := loginAccount(t, s, "limit-owner")
	for i := range 10 {
		if _, _, err := s.CreateToken(t.Context(), a.ID, "Client"); err != nil {
			t.Fatal(i, err)
		}
	}
	if _, _, err := accounts.New(p2.Accounts(), accountProviderFixture{}).CreateToken(t.Context(), a.ID, "Too many"); !errors.Is(err, accounts.ErrCredentialLimit) {
		t.Fatal("API credential cap not shared", err)
	}
	tokens, err := p2.Accounts().ListTokens(t.Context(), a.ID, time.Now().Add(accounts.CredentialLifetime+time.Second))
	if err != nil || len(tokens) != 0 {
		t.Fatal("expired credentials still listed", err)
	}
}

func TestMariaDBGoogleHTTPFlowAndAccountOwnership(t *testing.T) {
	f := newMaria(t)
	f.migrate(t)
	p := f.open(t)
	s := accounts.New(p.Accounts(), accountProviderFixture{})
	cache := kv.NewMemory()
	defer cache.Close()
	config := accounts.Config{Enabled: true, BaseURL: "https://events.example", ClientID: "fixture", ClientSecret: "fixture"}
	a, err := api.NewAPI(t.Context(), zerolog.Nop(), cache, api.WithAccounts(config, s))
	if err != nil {
		t.Fatal(err)
	}
	routes := a.Routes()
	start := httptest.NewRecorder()
	routes.ServeHTTP(start, httptest.NewRequest("GET", "/auth/google/start?return_to=%2Fme%2Finterests", nil))
	if start.Code != 303 {
		t.Fatal(start.Code, start.Body.String())
	}
	location := start.Result().Header.Get("Location")
	state := strings.Split(location, "state=")[1]
	flowCookie := start.Result().Cookies()[0]
	callback := httptest.NewRequest("GET", "/auth/google/callback?state="+state+"&code=HTTP-owner", nil)
	callback.AddCookie(flowCookie)
	finish := httptest.NewRecorder()
	routes.ServeHTTP(finish, callback)
	if finish.Code != 303 || finish.Header().Get("Location") != "/me/interests" {
		t.Fatal("sign-in return lost", finish.Code, finish.Body.String())
	}
	var cookie *http.Cookie
	for _, c := range finish.Result().Cookies() {
		if c.Name == config.CookieName("session") {
			cookie = c
		}
	}
	if cookie == nil || !cookie.HttpOnly || !cookie.Secure || cookie.SameSite != http.SameSiteLaxMode {
		t.Fatal("missing secure session")
	}
	me := httptest.NewRequest("GET", "/api/v1/me", nil)
	me.AddCookie(cookie)
	rec := httptest.NewRecorder()
	routes.ServeHTTP(rec, me)
	var own struct {
		Item accounts.Account `json:"item"`
		CSRF string           `json:"csrf_token"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &own); err != nil || rec.Code != 200 || own.CSRF == "" {
		t.Fatal("account read", rec.Code, err)
	}
	patch := httptest.NewRequest("PATCH", "/api/v1/me/profile", strings.NewReader(`{"display_name":"HTTP pal"}`))
	patch.AddCookie(cookie)
	patch.Header.Set("Content-Type", "application/json")
	patch.Header.Set("X-CSRF-Token", own.CSRF)
	patch.Header.Set("Origin", config.BaseURL)
	rec = httptest.NewRecorder()
	routes.ServeHTTP(rec, patch)
	if rec.Code != 200 {
		t.Fatal("verified owner patch", rec.Code, rec.Body.String())
	}
	other, raw := loginAccount(t, s, "Another-owner")
	otherPrincipal, err := s.Authenticate(t.Context(), raw, "session")
	if err != nil {
		t.Fatal(err)
	}
	revoke := httptest.NewRequest("DELETE", "/api/v1/me/tokens/"+otherPrincipal.Credential.ID, nil)
	revoke.AddCookie(cookie)
	revoke.Header.Set("X-CSRF-Token", own.CSRF)
	rec = httptest.NewRecorder()
	routes.ServeHTTP(rec, revoke)
	if rec.Code != 204 {
		t.Fatal(rec.Code)
	}
	if principal, err := s.Authenticate(t.Context(), raw, "session"); err != nil || principal.Account.ID != other.ID {
		t.Fatal("another owner revoked a credential", err)
	}
	// The exact callback cannot create a second credential on replay.
	replay := httptest.NewRequest("GET", callback.URL.String(), nil)
	replay.AddCookie(flowCookie)
	rec = httptest.NewRecorder()
	routes.ServeHTTP(rec, replay)
	if rec.Code != 400 {
		t.Fatal("callback replay accepted", rec.Code)
	}
}

func TestMariaDBAccountMigrationPreservesV1Events(t *testing.T) {
	f := newMaria(t)
	v1, err := migrations.ReadFile("migrations/00001_event_identity.sql")
	if err != nil {
		t.Fatal(err)
	}
	if err := migrateFS(t.Context(), f.migration, fstest.MapFS{"00001_event_identity.sql": &fstest.MapFile{Data: v1}}); err != nil {
		t.Fatal(err)
	}
	if _, err := Open(t.Context(), f.runtime); err == nil {
		t.Fatal("v2 app accepted v1 schema")
	}
	db, err := connect(t.Context(), f.migration)
	if err != nil {
		t.Fatal(err)
	}
	event := sampleEvent("v1-retained")
	if _, err := (&EventRepository{db: db, timeout: f.migration.QueryTimeout}).Upsert(t.Context(), event); err != nil {
		db.Close()
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	f.migrate(t)
	p := f.open(t)
	got, err := p.Events().Get(t.Context(), event.ID)
	if err != nil || got.Name != event.Name || got.Source.ID != event.Source.ID {
		t.Fatal("v1 event lost during accounts migration", err)
	}
	loginAccount(t, accounts.New(p.Accounts(), accountProviderFixture{}), "upgraded-account")
}

// Optional browser harness receives only a local fixture credential. It uses the
// real MariaDB repository and production handlers, never a runtime auth bypass.
func TestMariaDBAccountBrowserReview(t *testing.T) {
	script := os.Getenv("ACCOUNT_BROWSER_SCRIPT")
	if script == "" {
		t.Skip("set ACCOUNT_BROWSER_SCRIPT for browser verification")
	}
	t.Setenv("TICKETMASTER_KEY", "")
	t.Setenv("IP_GEOLOCATION_ENABLED", "false")
	f := newMaria(t)
	f.migrate(t)
	p := f.open(t)
	s := accounts.New(p.Accounts(), accountProviderFixture{})
	owner, raw := loginAccount(t, s, "browser-review")
	cache := kv.NewMemory()
	defer cache.Close()
	now := time.Now().UTC()
	catalog, _ := json.Marshal(map[string]any{"data": []models.Category{{NamedID: models.NamedID{ID: "music", Name: "Music"}, Genres: []models.Genre{}}, {NamedID: models.NamedID{ID: "sports", Name: "Sports"}, Genres: []models.Genre{}}, {NamedID: models.NamedID{ID: "arts", Name: "Arts & Theatre"}, Genres: []models.Genre{}}}, "fetched_at": now, "fresh_until": now.Add(time.Hour), "stale_until": now.Add(24 * time.Hour)})
	if err := cache.Set(t.Context(), "ticketmaster:v1:categories:en", catalog, 24*time.Hour); err != nil {
		t.Fatal(err)
	}
	server := httptest.NewUnstartedServer(nil)
	config := accounts.Config{Enabled: true, BaseURL: "http://" + server.Listener.Addr().String(), ClientID: "fixture", ClientSecret: "fixture"}
	a, err := api.NewAPI(t.Context(), zerolog.Nop(), cache, api.WithAccounts(config, s))
	if err != nil {
		t.Fatal(err)
	}
	server.Config.Handler = a.Routes()
	server.Start()
	defer server.Close()
	command := exec.CommandContext(t.Context(), "node", script)
	command.Env = append(os.Environ(), "ACCOUNT_BASE_URL="+server.URL, "ACCOUNT_FIXTURE_TOKEN="+raw, "ACCOUNT_FIXTURE_ID="+owner.ID)
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("browser review: %v\n%s", err, output)
	}
	t.Log(string(output))
}

// Package reviewfixture serves production handlers over a disposable MariaDB
// schema with seeded, signed-in fixture accounts for local manual or agent
// review. Credentials reach browsers only as Playwright storage-state files;
// there is no runtime auth bypass. Local development only: it is imported by
// tests and the review MCP command, never by cmd/ticketopia.
package reviewfixture

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/rs/zerolog"
	"github.com/sonastea/ticketopia/internal/accounts"
	"github.com/sonastea/ticketopia/internal/activity"
	"github.com/sonastea/ticketopia/internal/api"
	"github.com/sonastea/ticketopia/internal/appmcp"
	"github.com/sonastea/ticketopia/internal/discovery"
	"github.com/sonastea/ticketopia/internal/events"
	"github.com/sonastea/ticketopia/internal/kv"
	"github.com/sonastea/ticketopia/internal/location"
	"github.com/sonastea/ticketopia/internal/persistence"
	"github.com/sonastea/ticketopia/internal/persistence/persistencetest"
)

// Options configures one review server. Addr and Database.Addr must be
// loopback IP addresses with ports. SESEndpoint is the local SES emulator
// used only by scenarios that send email (DefaultSESEndpoint when empty).
type Options struct {
	Scenario    string
	Addr        string
	StateDir    string
	Database    persistencetest.Options
	SESEndpoint string
}

// Manifest describes a running server; its JSON is written to manifest.json.
type Manifest struct {
	BaseURL            string             `json:"base_url"`
	Scenario           string             `json:"scenario"`
	Run                string             `json:"run"`
	Accounts           map[string]Account `json:"accounts"`
	Paths              map[string]string  `json:"paths"`
	ScenarioPaths      map[string]string  `json:"scenario_paths"`
	EventIDs           []string           `json:"event_ids"`
	DiscussionThreadID string             `json:"discussion_thread_id"`
}

type Account struct {
	ID           string `json:"id"`
	DisplayName  string `json:"display_name"`
	Email        string `json:"email"`
	StorageState string `json:"storage_state"`
	// APIToken authenticates as this account with Authorization: Bearer.
	APIToken string `json:"api_token"`
}

// Server is a running review server. Close stops it and removes everything it
// created: HTTP listener, pools, schema, users and state files.
type Server struct {
	manifest  Manifest
	done      chan error
	closers   []func() error
	closeOnce sync.Once
	closeErr  error
}

func (s *Server) Manifest() Manifest { return s.manifest }

// Done receives an error if the HTTP server stops before Close.
func (s *Server) Done() <-chan error { return s.done }

func (s *Server) Close() error {
	s.closeOnce.Do(func() {
		var errs []error
		for _, c := range slices.Backward(s.closers) {
			errs = append(errs, c())
		}
		s.closeErr = errors.Join(errs...)
	})
	return s.closeErr
}

func (s *Server) onClose(c func() error) { s.closers = append(s.closers, c) }

// LoopbackAddr reports whether addr is a literal loopback IP and port.
func LoopbackAddr(addr string) bool {
	host, _, err := net.SplitHostPort(addr)
	ip := net.ParseIP(host)
	return err == nil && ip != nil && ip.IsLoopback()
}

// Start validates options before touching the database, then provisions,
// seeds and serves. ctx bounds setup only; the server runs until Close.
func Start(ctx context.Context, o Options) (_ *Server, err error) {
	sc, ok := scenarios[o.Scenario]
	if !ok {
		return nil, fmt.Errorf("unknown review scenario %q; choose one of: %s", o.Scenario, strings.Join(ScenarioNames(), ", "))
	}
	if !LoopbackAddr(o.Addr) {
		return nil, fmt.Errorf("review server address must be a loopback IP and port")
	}
	// The fixture creates schemas and users; keep it on a local database.
	if !LoopbackAddr(o.Database.Addr) {
		return nil, fmt.Errorf("review database address must be a loopback IP and port")
	}
	if o.StateDir == "" {
		return nil, fmt.Errorf("review state directory is required")
	}
	if o.SESEndpoint == "" {
		o.SESEndpoint = DefaultSESEndpoint
	}
	ses, err := sesEndpoint(o.SESEndpoint)
	if err != nil {
		return nil, err
	}
	run := accounts.ID()[:12]
	s := &Server{done: make(chan error, 1)}
	defer func() {
		if err != nil {
			err = errors.Join(err, s.Close())
		}
	}()

	db, err := persistencetest.Create(ctx, o.Database)
	if err != nil {
		return nil, err
	}
	s.onClose(db.Close)
	config := persistence.Config{Enabled: true, Host: db.Host, Port: db.Port, Database: db.Name,
		User: db.MigrationUser, Password: db.Password, TLSMode: "verify-full", CAFile: db.CAFile,
		MaxOpen: 10, MaxIdle: 2, Lifetime: 5 * time.Minute, IdleTime: time.Minute, DialTimeout: 3 * time.Second,
		ReadTimeout: 5 * time.Second, WriteTimeout: 5 * time.Second, StartupTimeout: 10 * time.Second,
		QueryTimeout: 3 * time.Second, ReadyTimeout: 500 * time.Millisecond, MigrationTimeout: 2 * time.Minute}
	if err := persistence.Migrate(ctx, config); err != nil {
		return nil, err
	}
	if err := db.GrantRuntime(ctx); err != nil {
		return nil, err
	}
	// Moderator assignment is an operator action, so it uses migration access.
	operator, err := persistence.Open(ctx, config)
	if err != nil {
		return nil, err
	}
	s.onClose(operator.Close)
	config.User = db.RuntimeUser
	p, err := persistence.Open(ctx, config)
	if err != nil {
		return nil, err
	}
	s.onClose(p.Close)

	// The running server outlives the setup context (for example a tool call).
	runCtx, cancel := context.WithCancel(context.WithoutCancel(ctx))
	s.onClose(func() error { cancel(); return nil })
	cache := kv.NewMemory()
	s.onClose(func() error { cache.Close(); return nil })

	listener, err := net.Listen("tcp", o.Addr)
	if err != nil {
		return nil, err
	}
	base := "http://" + listener.Addr().String()
	auth := accounts.New(p.Accounts(), providerFixture{run})
	f := &fixture{ctx: ctx, p: p, operator: operator, auth: auth, cache: cache, now: time.Now().UTC(), base: base, ses: ses, db: db}
	// Fixtures are deterministic regardless of the environment: Ticketmaster and
	// IP lookup are off (an empty key disables provider requests), so events come
	// only from cache fixtures and durable snapshots. Stored-Discover scenarios
	// instead collect from a loopback stand-in for the provider.
	f.discovery = discovery.New(runCtx, cache, zerolog.Nop(), discovery.Config{})
	options := []api.Option{}
	if sc.stored {
		f.upstream = newUpstream()
		s.onClose(func() error { f.upstream.server.Close(); return nil })
		f.discovery = discovery.New(runCtx, cache, zerolog.Nop(), discovery.Config{APIKey: "review-fixture", BaseURL: f.upstream.server.URL,
			Catalog: p.Discovery(), Coordinator: p.ProviderBudget("review-fixture", 1000), DailyBudget: 1000})
		options = append(options, api.WithPersistence(p.Ready, events.New(p.Events())))
	}
	s.onClose(func() error { f.discovery.WaitRefreshes(); return nil })
	if err := f.seed(sc); err != nil {
		listener.Close()
		return nil, err
	}

	authConfig := accounts.Config{Enabled: true, BaseURL: base, ClientID: "fixture", ClientSecret: "fixture"}
	live := activity.New(runCtx, p.Activity(), 256)
	s.onClose(func() error { live.Close(); return nil })
	recommendations := p.Recommendations()
	recommendations.EnableLiveActivity(true)
	app, err := api.NewAPI(runCtx, zerolog.Nop(), cache, append([]api.Option{
		api.WithDiscovery(f.discovery),
		api.WithLocation(location.New(runCtx, cache, zerolog.Nop(), location.Config{Disabled: true})),
		api.WithSavedEvents(p.Saved()), api.WithReminders(p.Reminders()), api.WithRadar(p.Radar(), time.Hour),
		api.WithFollows(p.Follows()), api.WithEventInterests(p.Interests()), api.WithEventRecommendations(recommendations),
		api.WithLiveActivity(live, 60), api.WithEventDiscussions(p.Discussions()), api.WithEventActivity(p.EventActivity()),
		api.WithDiscussionFollows(p.DiscussionFollows()), api.WithModeration(p.Moderation()), api.WithAccounts(authConfig, auth),
		// The app's agent tools, so reviews and load scripts can call /mcp too.
		api.WithMCP(appmcp.Config{Enabled: true, RatePerMinute: 6000, MaxInFlight: 64})}, options...)...)
	if err != nil {
		listener.Close()
		return nil, err
	}
	routes := app.Routes()
	cookie := authConfig.CookieName("session")
	// Verify the actual signed-in default-query path, rather than relying only
	// on a manually reproduced cache-key format while the provider is disabled.
	request := httptest.NewRequest(http.MethodGet, base+"/", nil)
	request.AddCookie(&http.Cookie{Name: cookie, Value: f.tokens["owner"]})
	response := httptest.NewRecorder()
	routes.ServeHTTP(response, request)
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `data-event-id="`+f.entries[0].ID+`"`) {
		listener.Close()
		return nil, fmt.Errorf("owner default Discover did not use seeded results: status %d", response.Code)
	}
	server := &http.Server{Handler: routes, ReadHeaderTimeout: 10 * time.Second}
	go func() {
		if err := server.Serve(listener); !errors.Is(err, http.ErrServerClosed) {
			s.done <- err
		}
	}()
	s.onClose(func() error {
		shutdown, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := server.Shutdown(shutdown); err != nil {
			return server.Close()
		}
		return nil
	})

	if err := os.MkdirAll(o.StateDir, 0o700); err != nil {
		return nil, err
	}
	host, _, _ := net.SplitHostPort(o.Addr)
	s.manifest = Manifest{BaseURL: base, Scenario: o.Scenario, Run: run, Accounts: map[string]Account{},
		Paths: f.paths, ScenarioPaths: f.scenarioPaths, DiscussionThreadID: f.threadID}
	for _, e := range f.entries {
		s.manifest.EventIDs = append(s.manifest.EventIDs, e.ID)
	}
	for _, key := range slices.Sorted(maps.Keys(f.people)) {
		a := f.people[key]
		state := filepath.Join(o.StateDir, key+".json")
		s.onClose(func() error { return removeIfExists(state) })
		if err := writeJSON(state, map[string]any{"origins": []any{}, "cookies": []map[string]any{{
			"name": cookie, "value": f.tokens[key], "domain": host, "path": "/",
			"expires": -1, "httpOnly": true, "secure": false, "sameSite": "Lax",
		}}}); err != nil {
			return nil, err
		}
		s.manifest.Accounts[key] = Account{ID: a.ID, DisplayName: a.DisplayName, Email: a.Email, StorageState: state, APIToken: f.apiTokens[key]}
	}
	// Publish the manifest last so a present manifest always describes a ready server.
	manifest := filepath.Join(o.StateDir, "manifest.json")
	s.onClose(func() error { return removeIfExists(manifest) })
	if err := writeJSON(manifest, s.manifest); err != nil {
		return nil, err
	}
	return s, nil
}

func writeJSON(path string, v any) error {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o600)
}

func removeIfExists(path string) error {
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}

// providerFixture signs in fixture accounts without Google. Addresses embed
// the run token so captured email can be matched to this run.
type providerFixture struct{ run string }

func (providerFixture) AuthorizationURL(l accounts.Login) string {
	return "https://accounts.example/authorize?state=" + l.State
}
func (p providerFixture) Verify(_ context.Context, code string, _ accounts.Flow) (accounts.Identity, error) {
	return accounts.Identity{Issuer: accounts.GoogleIssuer, Subject: code, Email: code + "+" + p.run + "@review.example"}, nil
}

package persistence

import (
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"sync"
	"sync/atomic"
	"testing"
	"testing/fstest"
	"time"

	"github.com/rs/zerolog"
	"github.com/sonastea/ticketopia/internal/accounts"
	"github.com/sonastea/ticketopia/internal/api"
	"github.com/sonastea/ticketopia/internal/discovery"
	"github.com/sonastea/ticketopia/internal/events"
	"github.com/sonastea/ticketopia/internal/kv"
	"github.com/sonastea/ticketopia/internal/models"
	"github.com/sonastea/ticketopia/internal/saved"
)

func savedDetail(id string) models.EventDetail {
	e := sampleEvent(id)
	e.PriceRanges = []models.PriceRange{{Currency: "USD", Min: pointer("25.00"), Max: pointer("40.00")}}
	e.Images = []models.Image{{URL: "https://example.com/event.jpg", Width: 640, Height: 360}}
	e.Info, e.PleaseNote = "Last-known description", "Venue rules"
	e.PublicSale.Start = pointer(time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC))
	e.Presales = []models.Presale{{Name: "Early tickets"}}
	return models.EventDetail{Item: e, Meta: models.Freshness{DataAsOf: time.Date(2026, 10, 6, 10, 0, 0, 0, time.UTC)}}
}
func TestMariaDBSavedAtomicityPrivacyConcurrencyAndRestart(t *testing.T) {
	f := newMaria(t)
	f.migrate(t)
	pools := []*Pool{f.open(t), f.open(t), f.open(t)}
	auth := accounts.New(pools[0].Accounts(), accountProviderFixture{})
	owner, _ := loginAccount(t, auth, "saved-owner")
	other, _ := loginAccount(t, auth, "other-owner")
	detail := savedDetail("Case_Event")
	var created atomic.Int32
	var wg sync.WaitGroup
	for i := range 12 {
		wg.Go(func() {
			_, fresh, err := pools[i%3].Saved().Save(t.Context(), owner.ID, detail)
			if err != nil {
				t.Error(err)
			}
			if fresh {
				created.Add(1)
			}
		})
	}
	wg.Wait()
	if created.Load() != 1 {
		t.Fatal("concurrent saves created duplicates", created.Load())
	}
	first, err := pools[0].Saved().Get(t.Context(), owner.ID, detail.Item.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := pools[1].Saved().Get(t.Context(), other.ID, detail.Item.ID); !errors.Is(err, saved.ErrNotFound) {
		t.Fatal("foreign owner read save", err)
	}
	if err := pools[1].Saved().Remove(t.Context(), other.ID, detail.Item.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := pools[0].Saved().Get(t.Context(), owner.ID, detail.Item.ID); err != nil {
		t.Fatal("foreign delete changed bookmark")
	}
	if err := pools[0].Close(); err != nil {
		t.Fatal(err)
	}
	restarted := f.open(t)
	s := saved.New(restarted.Saved(), nil) // No provider/cache exists after restart.
	item, fresh, err := s.Save(t.Context(), owner.ID, detail.Item.ID)
	if err != nil || fresh || !item.SavedAt.Equal(first.SavedAt) || !item.Meta.Stale {
		t.Fatal("retry/restart not durable", err)
	}
	want, _ := json.Marshal(detail.Item)
	got, _ := json.Marshal(item.Event)
	if string(want) != string(got) {
		t.Fatal("full snapshot metadata lost", string(got))
	}
	stored, err := s.Detail(t.Context(), detail.Item.ID)
	if err != nil || !stored.Meta.Stale || stored.Item.Images[0].URL != detail.Item.Images[0].URL {
		t.Fatal("cache/provider-free details lost", err)
	}
	if _, fresh, err := s.Save(t.Context(), other.ID, detail.Item.ID); err != nil || !fresh {
		t.Fatal("another owner could not save retained metadata", err)
	}
	for range 2 {
		if err := s.Remove(t.Context(), owner.ID, detail.Item.ID); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := s.Get(t.Context(), owner.ID, detail.Item.ID); !errors.Is(err, saved.ErrNotFound) {
		t.Fatal("remove not durable")
	}
	if _, err := s.Get(t.Context(), other.ID, detail.Item.ID); err != nil {
		t.Fatal("owner removal changed another bookmark")
	}
	if _, _, err := s.Save(t.Context(), owner.ID, detail.Item.ID); err != nil {
		t.Fatal("could not resave without provider", err)
	}
	// Snapshot writes cannot erase newer data with an older cache observation.
	old := detail
	old.Item.Name = "Old title"
	old.Meta.DataAsOf = old.Meta.DataAsOf.Add(-time.Hour)
	if _, _, err := restarted.Saved().Save(t.Context(), owner.ID, old); err != nil {
		t.Fatal(err)
	}
	stored, err = restarted.Saved().Snapshot(t.Context(), detail.Item.ID)
	if err != nil || stored.Item.Name != detail.Item.Name {
		t.Fatal("older observation erased last-good snapshot")
	}
	// An activity FK failure must roll back identity, mapping and snapshot together.
	failed := savedDetail("AtomicFailure")
	if _, _, err := restarted.Saved().Save(t.Context(), accounts.ID(), failed); err == nil {
		t.Fatal("foreign account accepted")
	}
	if _, err := restarted.Events().Get(t.Context(), failed.Item.ID); !errors.Is(err, events.ErrNotFound) {
		t.Fatal("failed local write leaked event identity", err)
	}
	if _, err := restarted.Saved().Snapshot(t.Context(), failed.Item.ID); !errors.Is(err, saved.ErrNotFound) {
		t.Fatal("failed local write leaked snapshot", err)
	}
	for _, query := range []string{`DELETE FROM event_snapshots`, `UPDATE saved_events SET saved_at=UTC_TIMESTAMP(6)`, `DROP TABLE saved_events`} {
		if _, err := restarted.db.ExecContext(t.Context(), query); err == nil {
			t.Fatal("runtime overprivileged", query)
		}
	}
}
func TestMariaDBSavedKeysetPaginationAndCaseIdentity(t *testing.T) {
	f := newMaria(t)
	f.migrate(t)
	p := f.open(t)
	owner, _ := loginAccount(t, accounts.New(p.Accounts(), accountProviderFixture{}), "pagination")
	for _, id := range []string{"a", "A", "z"} {
		if _, _, err := p.Saved().Save(t.Context(), owner.ID, savedDetail(id)); err != nil {
			t.Fatal(err)
		}
	}
	execSQL(t, f.admin, "UPDATE "+f.runtime.Database+".saved_events SET saved_at='2026-10-06 10:00:00.000000'")
	s := saved.New(p.Saved(), nil)
	values := url.Values{"limit": {"1"}}
	ids := []string{}
	for {
		list, err := s.List(t.Context(), owner.ID, values)
		if err != nil || len(list.Items) != 1 {
			t.Fatal("page", err)
		}
		ids = append(ids, list.Items[0].Event.ID)
		if list.NextCursor == nil {
			break
		}
		values.Set("cursor", *list.NextCursor)
		if len(ids) > 3 {
			t.Fatal("pagination loop")
		}
	}
	if fmt.Sprint(ids) != "[ticketmaster:z ticketmaster:a ticketmaster:A]" {
		t.Fatal("case-sensitive tie order", ids)
	}
	if _, err := s.List(t.Context(), accounts.ID(), values); err == nil {
		t.Fatal("cursor crossed owner boundary")
	}
}
func TestMariaDBSavedMigrationPreservesAccountsAndEvents(t *testing.T) {
	f := newMaria(t)
	v1, _ := migrations.ReadFile("migrations/00001_event_identity.sql")
	v2, _ := migrations.ReadFile("migrations/00002_accounts.sql")
	if err := migrateFS(t.Context(), f.migration, fstest.MapFS{"00001_event_identity.sql": {Data: v1}, "00002_accounts.sql": {Data: v2}}); err != nil {
		t.Fatal(err)
	}
	db, err := connect(t.Context(), f.migration)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	a, raw := loginAccount(t, accounts.New(&AccountRepository{db: db, timeout: f.migration.QueryTimeout}, accountProviderFixture{}), "pre-saves")
	event := sampleEvent("BeforeSaves")
	if _, err := (&EventRepository{db: db, timeout: f.migration.QueryTimeout}).Upsert(t.Context(), event); err != nil {
		t.Fatal(err)
	}
	if _, err := Open(t.Context(), f.runtime); err == nil {
		t.Fatal("v3 app accepted v2 schema")
	}
	f.migrate(t)
	p := f.open(t)
	principal, err := accounts.New(p.Accounts(), nil).Authenticate(t.Context(), raw, "session")
	if err != nil || principal.Account.ID != a.ID {
		t.Fatal("migration lost credential/account", err)
	}
	if got, err := p.Events().Get(t.Context(), event.ID); err != nil || got.Name != event.Name {
		t.Fatal("migration lost event", err)
	}
	if err := Migrate(t.Context(), f.migration); err != nil {
		t.Fatal("migration not repeatable", err)
	}
}

// Optional browser verification uses isolated SQL accounts and server-owned
// provider fixtures. It never bypasses production credential or CSRF checks.
func TestMariaDBSavedBrowserReview(t *testing.T) {
	script := os.Getenv("SAVED_BROWSER_SCRIPT")
	if script == "" {
		t.Skip("set SAVED_BROWSER_SCRIPT for browser verification")
	}
	runActivityBrowser(t, script)
}
func TestMariaDBInterestBrowserReview(t *testing.T) {
	script := os.Getenv("INTEREST_BROWSER_SCRIPT")
	if script == "" {
		t.Skip("set INTEREST_BROWSER_SCRIPT for browser verification")
	}
	runActivityBrowser(t, script)
}
func TestMariaDBRecommendationBrowserReview(t *testing.T) {
	script := os.Getenv("RECOMMENDATION_BROWSER_SCRIPT")
	if script == "" {
		t.Skip("set RECOMMENDATION_BROWSER_SCRIPT for optional Chromium verification")
	}
	runActivityBrowser(t, script)
}
func runActivityBrowser(t *testing.T, script string) {
	t.Setenv("TICKETMASTER_KEY", "")
	t.Setenv("IP_GEOLOCATION_ENABLED", "false")
	f := newMaria(t)
	f.migrate(t)
	p := f.open(t)
	auth := accounts.New(p.Accounts(), accountProviderFixture{})
	owner, raw := loginAccount(t, auth, "saved-browser")
	other, otherRaw := loginAccount(t, auth, "saved-browser-other")
	cache := kv.NewMemory()
	defer cache.Close()
	now := time.Now().UTC()
	seed := func(key string, data any) {
		encoded, err := json.Marshal(map[string]any{"data": data, "fetched_at": now, "fresh_until": now.Add(time.Hour), "stale_until": now.Add(24 * time.Hour)})
		if err != nil {
			t.Fatal(err)
		}
		if err := cache.Set(t.Context(), key, encoded, 24*time.Hour); err != nil {
			t.Fatal(err)
		}
	}
	entries := []models.Event{}
	for i, name := range []string{"A good night in the city", "An event with a very long name that still needs readable dates, a venue, and room for private save controls", "Something new for next weekend"} {
		detail := savedDetail(fmt.Sprintf("Browser_%d", i))
		detail.Item.Name = name
		detail.Item.Images = nil
		detail.Meta.DataAsOf = now
		entries = append(entries, detail.Item)
		seed("ticketmaster:v1:event:"+detail.Item.ID, detail.Item)
	}
	seed("ticketmaster:v1:categories:en", []models.Category{{NamedID: models.NamedID{ID: "sports", Name: "Sports"}, Genres: []models.Genre{}}})
	q, err := discovery.ParseQuery(url.Values{"city": {"Chicago"}, "limit": {"2"}}, now)
	if err != nil {
		t.Fatal(err)
	}
	for page := range 2 {
		sum := sha256.Sum256([]byte(q.Values().Encode()))
		items := entries[:2]
		if page == 1 {
			items = entries[2:]
		}
		seed(fmt.Sprintf("ticketmaster:v1:events:%x:%d", sum, page), map[string]any{"items": items, "total": 3, "total_pages": 2})
	}
	start := func(store kv.Store, enabled bool) *httptest.Server {
		server := httptest.NewUnstartedServer(nil)
		config := accounts.Config{Enabled: enabled, BaseURL: "http://" + server.Listener.Addr().String(), ClientID: "fixture", ClientSecret: "fixture"}
		options := []api.Option{}
		if enabled {
			options = append(options, api.WithAccounts(config, auth), api.WithSavedEvents(p.Saved()), api.WithEventInterests(p.Interests()), api.WithEventRecommendations(p.Recommendations()))
		}
		app, err := api.NewAPI(t.Context(), zerolog.Nop(), store, options...)
		if err != nil {
			t.Fatal(err)
		}
		server.Config.Handler = app.Routes()
		server.Start()
		t.Cleanup(server.Close)
		return server
	}
	server := start(cache, true)
	noCache := kv.NewMemory()
	defer noCache.Close()
	fallback := start(noCache, true)
	disabled := start(cache, false)
	command := exec.CommandContext(t.Context(), "node", script)
	command.Env = append(os.Environ(), "SAVED_BASE_URL="+server.URL, "SAVED_FALLBACK_URL="+fallback.URL, "SAVED_DISABLED_URL="+disabled.URL, "SAVED_SEARCH_PATH=/?"+q.Values().Encode(), "SAVED_FIXTURE_TOKEN="+raw, "SAVED_OTHER_TOKEN="+otherRaw, "SAVED_FIXTURE_ID="+owner.ID, "SAVED_OTHER_ID="+other.ID)
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("saved browser review: %v\n%s", err, output)
	}
	t.Log(string(output))
}

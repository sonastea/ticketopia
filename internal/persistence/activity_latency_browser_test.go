package persistence

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"github.com/sonastea/ticketopia/internal/accounts"
	"github.com/sonastea/ticketopia/internal/activity"
	"github.com/sonastea/ticketopia/internal/api"
	"github.com/sonastea/ticketopia/internal/kv"
	"github.com/sonastea/ticketopia/internal/models"
)

// latencyReplica is a restartable app replica: a fresh pool, service and API
// replace the old ones, closing its streams as a process restart would.
type latencyReplica struct {
	t       *testing.T
	f       mariaFixture
	auth    *accounts.Service
	cache   kv.Store
	server  *httptest.Server
	handler atomic.Pointer[http.Handler]
	mu      sync.Mutex
	stop    func()
}

func (r *latencyReplica) start() {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.stop != nil {
		r.stop()
	}
	p, err := Open(r.t.Context(), r.f.runtime)
	if err != nil {
		r.t.Error(err)
		return
	}
	ctx, cancel := context.WithCancel(r.t.Context())
	service := activity.New(ctx, p.Activity(), 256)
	writer := p.Recommendations()
	writer.EnableLiveActivity(true)
	config := accounts.Config{Enabled: true, BaseURL: r.server.URL, ClientID: "fixture", ClientSecret: "fixture"}
	// 600 starts/minute (the configured maximum): every journey shares one
	// loopback client address. Production keeps the 60/minute default.
	app, err := api.NewAPI(ctx, zerolog.Nop(), r.cache, api.WithAccounts(config, r.auth), api.WithLiveActivity(service, 600),
		api.WithEventRecommendations(writer), api.WithSavedEvents(p.Saved()), api.WithEventInterests(p.Interests()), api.WithFollows(p.Follows()))
	if err != nil {
		r.t.Error(err)
		cancel()
		p.Close()
		return
	}
	var routes http.Handler = app.Routes()
	r.handler.Store(&routes)
	r.stop = func() { cancel(); service.Close(); p.Close() }
}

func newLatencyReplica(t *testing.T, f mariaFixture, auth *accounts.Service, cache kv.Store) *latencyReplica {
	r := &latencyReplica{t: t, f: f, auth: auth, cache: cache}
	r.server = httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		(*r.handler.Load()).ServeHTTP(w, req)
	}))
	r.server.Start()
	r.start()
	t.Cleanup(func() {
		r.server.Close()
		r.mu.Lock()
		if r.stop != nil {
			r.stop()
		}
		r.mu.Unlock()
	})
	return r
}

// Opt-in browser-visible latency gate (rollout P1.4): Apply click → confirmed
// matching view rendered, cold starts after restart/eviction, and
// publication/removal/membership commit → rendered update, in Chromium.
func TestMariaDBLiveActivityLatencyBrowser(t *testing.T) {
	script := os.Getenv("LIVE_ACTIVITY_LATENCY_BROWSER_SCRIPT")
	if script == "" {
		t.Skip("set LIVE_ACTIVITY_LATENCY_BROWSER_SCRIPT and PLAYWRIGHT_MODULE")
	}
	t.Setenv("TICKETMASTER_KEY", "")
	t.Setenv("IP_GEOLOCATION_ENABLED", "false")
	n := 1000
	f := newMaria(t)
	f.migrate(t)
	p := f.open(t)
	auth := accounts.New(p.Accounts(), accountProviderFixture{})
	contributors := []string{}
	for i := range 8 {
		account, _ := loginAccount(t, auth, fmt.Sprintf("latency-contributor-%d", i))
		contributors = append(contributors, account.ID)
	}
	seedCapacityWindow(t, f, p, contributors, n)
	viewer, viewerToken := loginAccount(t, auth, "latency-viewer")
	_, otherToken := loginAccount(t, auth, "latency-publisher")
	db := f.runtime.Database
	// Sparse relationships keep each personal view under twenty items, so every
	// membership change is visible rather than ranked out of the view.
	execSQL(t, f.admin, fmt.Sprintf("INSERT INTO %s.saved_events (account_id,event_id) SELECT ?,CONCAT('ticketmaster:LiveCap_',seq-1) FROM %s.seq_1_to_%d WHERE MOD(seq-1,97)=0 AND seq>1", db, db, n), viewer.ID)
	personal := personalFilter([]string{"interested", "saved"})
	if _, err := p.Accounts().ReplaceLivePreferences(t.Context(), viewer.ID, 1, true, personal); err != nil {
		t.Fatal(err)
	}
	cold := []string{}
	for i := range 20 {
		account, raw := loginAccount(t, auth, fmt.Sprintf("latency-cold-%d", i))
		execSQL(t, f.admin, fmt.Sprintf("INSERT INTO %s.saved_events (account_id,event_id) SELECT ?,CONCAT('ticketmaster:LiveCap_',seq-1) FROM %s.seq_1_to_%d WHERE MOD(seq-1+?,97)=0", db, db, n), account.ID, i+1)
		cold = append(cold, raw)
	}
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
	categories := []models.Category{}
	for _, id := range capacityCategories {
		categories = append(categories, models.Category{NamedID: models.NamedID{ID: id, Name: id}, Genres: []models.Genre{}})
	}
	seed("ticketmaster:v1:categories:en", categories)
	for i := range 40 {
		seed("ticketmaster:v1:event:"+capacityDetail(i).Item.ID, capacityDetail(i).Item)
	}
	a := newLatencyReplica(t, f, auth, cache)
	b := newLatencyReplica(t, f, auth, cache)
	control := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if req.Method != "POST" || req.URL.Path != "/restart" {
			http.NotFound(w, req)
			return
		}
		a.start()
		w.WriteHeader(204)
	}))
	defer control.Close()
	command := exec.CommandContext(t.Context(), "node", script)
	command.Env = append(os.Environ(), "LATENCY_BASE_URL="+a.server.URL, "LATENCY_OTHER_REPLICA_URL="+b.server.URL, "LATENCY_CONTROL_URL="+control.URL,
		"LATENCY_VIEWER_TOKEN="+viewerToken, "LATENCY_OTHER_TOKEN="+otherToken, "LATENCY_COLD_TOKENS="+strings.Join(cold, ","), fmt.Sprintf("LATENCY_GROUPS=%d", n))
	output, err := command.CombinedOutput()
	t.Log(string(output))
	if err != nil {
		t.Fatalf("latency browser journey: %v", err)
	}
}

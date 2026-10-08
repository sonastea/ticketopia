package persistence

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"testing"

	"github.com/rs/zerolog"
	"github.com/sonastea/ticketopia/internal/accounts"
	"github.com/sonastea/ticketopia/internal/api"
	"github.com/sonastea/ticketopia/internal/discovery"
	"github.com/sonastea/ticketopia/internal/kv"
)

func TestMariaDBFollowsBrowserReview(t *testing.T) {
	script := os.Getenv("FOLLOWS_BROWSER_SCRIPT")
	if script == "" {
		t.Skip("set FOLLOWS_BROWSER_SCRIPT for isolated Chromium verification")
	}
	t.Setenv("TICKETMASTER_KEY", "")
	t.Setenv("IP_GEOLOCATION_ENABLED", "false")
	f := newMaria(t)
	f.migrate(t)
	pool := f.open(t)
	auth := accounts.New(pool.Accounts(), accountProviderFixture{})
	owner, session := loginAccount(t, auth, "follows-browser")
	_, otherSession := loginAccount(t, auth, "follows-browser-other")
	_, token, err := auth.CreateToken(t.Context(), owner.ID, "fixture client")
	if err != nil {
		t.Fatal(err)
	}
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		kind, field := "artist", "attractions"
		if strings.HasPrefix(r.URL.Path, "/venues") {
			kind, field = "venue", "venues"
		}
		keyword := r.URL.Query().Get("keyword")
		items := []map[string]any{}
		n := 2
		pages := 1
		total := 2
		if keyword == "Nothing" {
			n, total = 0, 0
		}
		if keyword == "Pages" {
			n, pages, total = 20, 2, 21
			if r.URL.Query().Get("page") == "1" {
				n = 1
			}
		}
		for i := range n {
			name := "Private artist"
			if kind == "venue" {
				name = "Private venue"
			}
			if i == 1 {
				name += " with a very long name " + strings.Repeat("long-name-", 12)
			}
			item := map[string]any{"id": fmt.Sprintf("Follow_%d", i), "name": name, "url": "https://example.com/fixture"}
			if kind == "venue" && i == 0 {
				item["city"] = map[string]string{"name": "Boston"}
				item["address"] = map[string]string{"line1": "1 Main Street"}
				item["timezone"] = "America/New_York"
			}
			items = append(items, item)
		}
		page, _ := strconv.Atoi(r.URL.Query().Get("page"))
		_ = json.NewEncoder(w).Encode(map[string]any{"_embedded": map[string]any{field: items}, "page": map[string]int{"number": page, "totalElements": total, "totalPages": pages}})
	}))
	defer provider.Close()
	start := func(enabled, providerEnabled bool) *httptest.Server {
		cache := kv.NewMemory()
		t.Cleanup(func() { _ = cache.Close() })
		server := httptest.NewUnstartedServer(nil)
		config := accounts.Config{Enabled: enabled, BaseURL: "http://" + server.Listener.Addr().String(), ClientID: "fixture", ClientSecret: "fixture"}
		key := ""
		if providerEnabled {
			key = "fixture"
		}
		discover := discovery.New(t.Context(), cache, zerolog.Nop(), discovery.Config{APIKey: key, BaseURL: provider.URL})
		options := []api.Option{api.WithDiscovery(discover)}
		if enabled {
			options = append(options, api.WithAccounts(config, auth), api.WithFollows(pool.Follows()))
		}
		app, err := api.NewAPI(t.Context(), zerolog.Nop(), cache, options...)
		if err != nil {
			t.Fatal(err)
		}
		server.Config.Handler = app.Routes()
		server.Start()
		t.Cleanup(server.Close)
		return server
	}
	server := start(true, true)
	fallback := start(true, false)
	disabled := start(false, false)
	command := exec.CommandContext(t.Context(), "node", script)
	command.Env = append(os.Environ(), "FOLLOWS_BASE_URL="+server.URL, "FOLLOWS_FALLBACK_URL="+fallback.URL, "FOLLOWS_DISABLED_URL="+disabled.URL, "FOLLOWS_SESSION="+session, "FOLLOWS_OTHER_SESSION="+otherSession, "FOLLOWS_API_TOKEN="+token)
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("follows browser review: %v\n%s", err, output)
	}
	t.Log(string(output))
}

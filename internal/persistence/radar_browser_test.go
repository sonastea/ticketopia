package persistence

import (
	"fmt"
	"net/http/httptest"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"github.com/sonastea/ticketopia/internal/accounts"
	"github.com/sonastea/ticketopia/internal/api"
	"github.com/sonastea/ticketopia/internal/discovery"
	"github.com/sonastea/ticketopia/internal/kv"
	"github.com/sonastea/ticketopia/internal/models"
)

func TestMariaDBRadarBrowserReview(t *testing.T) {
	script := os.Getenv("RADAR_BROWSER_SCRIPT")
	if script == "" {
		t.Skip("set RADAR_BROWSER_SCRIPT for isolated Chromium verification")
	}
	t.Setenv("TICKETMASTER_KEY", "")
	t.Setenv("IP_GEOLOCATION_ENABLED", "false")
	f := newMaria(t)
	f.migrate(t)
	pool := f.open(t)
	auth := accounts.New(pool.Accounts(), accountProviderFixture{})
	owner, session := loginAccount(t, auth, "radar-browser")
	_, newSession := loginAccount(t, auth, "radar-browser-new")
	nearby, nearbySession := loginAccount(t, auth, "radar-browser-nearby")
	empty, emptySession := loginAccount(t, auth, "radar-browser-empty")
	for _, id := range []string{owner.ID, nearby.ID, empty.ID} {
		patch := accounts.PreferencesPatch{City: pointer("Berlin"), Country: pointer("DE"), Timezone: pointer("Europe/Berlin")}
		if id == owner.ID {
			patch.CategoryIDs = pointer([]string{"music"})
		}
		if id == empty.ID {
			patch.CategoryIDs = pointer([]string{"unavailable"})
		}
		if _, err := auth.UpdatePreferences(t.Context(), id, patch); err != nil {
			t.Fatal(err)
		}
	}
	if _, _, err := pool.Follows().Follow(t.Context(), owner.ID, followDetail("artist", "Artist")); err != nil {
		t.Fatal(err)
	}
	date := time.Now().AddDate(0, 0, 2).Format(time.DateOnly)
	for i := range 4 {
		e := sampleEvent(fmt.Sprintf("radar-browser-%d", i))
		e.Name = fmt.Sprintf("Radar show %d", i)
		if i == 1 {
			e.Name += " with a very long event title " + strings.Repeat("long-event-name-", 10)
		}
		e.Start.LocalDate = &date
		e.Start.DateTime = nil
		e.Venues[0].CountryCode = "DE"
		e.Venues[0].City = "Berlin"
		e.PriceRanges = nil
		e.Images = nil
		e.PublicSale = models.PublicSale{}
		e.Classifications = []models.Classification{{Segment: &models.NamedID{ID: "music"}}}
		e.Artists = nil
		if i < 3 {
			e.Artists = []models.Artist{{ID: "ticketmaster:Artist", Name: "Private artist"}}
		}
		if err := pool.Discovery().Observe(t.Context(), models.EventDetail{Item: e, Meta: models.Freshness{DataAsOf: time.Now().Add(-24 * time.Hour)}}); err != nil {
			t.Fatal(err)
		}
	}
	start := func(enabled bool) *httptest.Server {
		cache := kv.NewMemory()
		t.Cleanup(func() { _ = cache.Close() })
		server := httptest.NewUnstartedServer(nil)
		config := accounts.Config{Enabled: enabled, BaseURL: "http://" + server.Listener.Addr().String(), ClientID: "fixture", ClientSecret: "fixture"}
		discover := discovery.New(t.Context(), cache, zerolog.Nop(), discovery.Config{Catalog: pool.Discovery()})
		options := []api.Option{api.WithDiscovery(discover)}
		if enabled {
			options = append(options, api.WithAccounts(config, auth), api.WithRadar(pool.Radar(), 6*time.Hour), api.WithSavedEvents(pool.Saved()), api.WithFollows(pool.Follows()), api.WithEventInterests(pool.Interests()))
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
	server := start(true)
	disabled := start(false)
	cmd := exec.CommandContext(t.Context(), "node", script)
	cmd.Env = append(os.Environ(), "RADAR_BASE_URL="+server.URL, "RADAR_DISABLED_URL="+disabled.URL, "RADAR_SESSION="+session, "RADAR_NEW_SESSION="+newSession, "RADAR_NEARBY_SESSION="+nearbySession, "RADAR_EMPTY_SESSION="+emptySession)
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("radar browser review: %v\n%s", err, output)
	}
	t.Log(string(output))
}

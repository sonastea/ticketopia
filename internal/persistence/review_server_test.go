package persistence

import (
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"github.com/sonastea/ticketopia/internal/accounts"
	"github.com/sonastea/ticketopia/internal/activity"
	"github.com/sonastea/ticketopia/internal/api"
	"github.com/sonastea/ticketopia/internal/discovery"
	"github.com/sonastea/ticketopia/internal/discussionfollows"
	"github.com/sonastea/ticketopia/internal/discussions"
	"github.com/sonastea/ticketopia/internal/kv"
	"github.com/sonastea/ticketopia/internal/models"
	"github.com/sonastea/ticketopia/internal/saved"
)

// Interactive manual-review server: production handlers over an isolated schema
// with fixture accounts signed in through the real login service. Credentials
// reach the browser only as Playwright storage-state files; there is no runtime
// auth bypass. Stop with Ctrl-C so the schema and users are dropped.
func TestMariaDBReviewServer(t *testing.T) {
	addr := os.Getenv("REVIEW_SERVER_ADDR")
	if addr == "" {
		t.Skip("set REVIEW_SERVER_ADDR (for example 127.0.0.1:8090) to serve signed-in review fixtures")
	}
	host, _, err := net.SplitHostPort(addr)
	if ip := net.ParseIP(host); err != nil || ip == nil || !ip.IsLoopback() {
		t.Fatal("REVIEW_SERVER_ADDR must be a loopback IP and port")
	}
	stateDir := os.Getenv("REVIEW_SERVER_STATE_DIR")
	if stateDir == "" {
		stateDir = filepath.Join(os.TempDir(), "ticketopia-review")
	}
	t.Setenv("TICKETMASTER_KEY", "")
	t.Setenv("IP_GEOLOCATION_ENABLED", "false")
	f := newMaria(t)
	f.migrate(t)
	p := f.open(t)
	ctx := t.Context()
	must := func(_ any, err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	must3 := func(_, _ any, err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}

	auth := accounts.New(p.Accounts(), accountProviderFixture{})
	people := map[string]accounts.Account{}
	tokens := map[string]string{}
	for _, who := range []struct{ key, name, first string }{
		{"owner", "Riley Review", "Riley"},
		{"other", "Sam Second-Account", "Sam"},
		{"moderator", "Mo Moderator", "Mo"},
	} {
		a, raw := loginAccount(t, auth, "review-"+who.key)
		bio := "Fixture account for manual review."
		must(auth.UpdateProfile(ctx, a.ID, accounts.ProfilePatch{DisplayName: &who.name, ActivityFirstName: &who.first, Bio: &bio}))
		people[who.key], tokens[who.key] = a, raw
	}
	city, country, zone := "Chicago", "US", "America/Chicago"
	must(auth.UpdatePreferences(ctx, people["owner"].ID, accounts.PreferencesPatch{City: &city, Country: &country, Timezone: &zone}))
	operator, err := Open(ctx, f.migration)
	if err != nil {
		t.Fatal(err)
	}
	_, err = operator.SetModerator(ctx, people["moderator"].ID, "review server fixture", true)
	operator.Close()
	if err != nil {
		t.Fatal(err)
	}

	cache := kv.NewMemory()
	defer cache.Close()
	now := time.Now().UTC()
	seed := func(key string, data any) {
		t.Helper()
		encoded, err := json.Marshal(map[string]any{"data": data, "fetched_at": now, "fresh_until": now.Add(time.Hour), "stale_until": now.Add(24 * time.Hour)})
		if err != nil {
			t.Fatal(err)
		}
		if err := cache.Set(ctx, key, encoded, 24*time.Hour); err != nil {
			t.Fatal(err)
		}
	}
	seed("ticketmaster:v1:categories:en", []models.Category{
		{NamedID: models.NamedID{ID: "music", Name: "Music"}, Genres: []models.Genre{}},
		{NamedID: models.NamedID{ID: "sports", Name: "Sports"}, Genres: []models.Genre{}},
		{NamedID: models.NamedID{ID: "arts", Name: "Arts & Theatre"}, Genres: []models.Genre{}},
	})
	chicago, err := time.LoadLocation(zone)
	if err != nil {
		t.Fatal(err)
	}
	names := []string{
		"A good night in the city",
		"An event with a very long name that still needs readable dates, a venue, and room for private save controls on a narrow phone",
		"Something new for next weekend",
		"Saturday matinee",
		"Late show at the club",
		"Season opener",
	}
	details := []models.EventDetail{}
	entries := []models.Event{}
	for i, name := range names {
		d := savedDetail(fmt.Sprintf("Review_%d", i))
		start := time.Date(now.Year(), now.Month(), now.Day()+3+i*4, 19, 30, 0, 0, chicago)
		date, clock := start.Format(time.DateOnly), start.Format(time.TimeOnly)
		sale := now.Add(time.Duration(2+i*24) * time.Hour)
		d.Item.Name = name
		d.Item.Images = nil
		d.Item.Start = models.EventStart{DateTime: &start, LocalDate: &date, LocalTime: &clock, Timezone: &zone}
		d.Item.PublicSale.Start = &sale
		d.Item.Venues = []models.Venue{{ID: fmt.Sprintf("ticketmaster:review-venue-%d", i%2), Place: models.Place{Name: []string{"The Riverside Hall", "A Venue With An Unusually Long Name On The Near West Side"}[i%2], Address: "123 W Example St", City: city, State: "IL", Country: "United States Of America", CountryCode: country}}}
		d.Meta.DataAsOf = now
		details, entries = append(details, d), append(entries, d.Item)
		seed("ticketmaster:v1:event:"+d.Item.ID, d.Item)
	}
	q, err := discovery.ParseQuery(url.Values{"city": {city}}, now)
	if err != nil {
		t.Fatal(err)
	}
	q.Limit = 20
	sum := sha256.Sum256([]byte(q.Values().Encode()))
	seed(fmt.Sprintf("ticketmaster:v1:events:%x:0", sum), map[string]any{"items": entries, "total": len(entries), "total_pages": 1})

	owner, other, moderator := people["owner"].ID, people["other"].ID, people["moderator"].ID
	for i, d := range details {
		if i < 4 {
			must3(p.Saved().Save(ctx, owner, d))
		}
		if i == 0 || i == 2 {
			must3(p.Interests().Set(ctx, owner, d, []string{"public", "private"}[i/2]))
		}
		if i < 2 {
			must3(p.Recommendations().Set(ctx, other, d, "Great sound and an easy trip home afterwards."))
		}
	}
	must3(p.Interests().Set(ctx, other, details[0], "public"))
	must3(p.Recommendations().Set(ctx, owner, details[0], "Arrive early; the opener is worth it."))
	must(p.Reminders().Set(ctx, owner, details[0].Item.ID, 60))
	must(p.Reminders().Set(ctx, owner, details[1].Item.ID, 24*60))

	talk := discussions.New(p.Discussions(), saved.New(p.Saved(), nil))
	follows := discussionfollows.New(p.DiscussionFollows())
	event := details[0].Item.ID
	post := func(who, thread, parent, body string) discussions.Post {
		t.Helper()
		created, _, err := talk.Create(ctx, who, event, thread, parent, body, accounts.ID())
		if err != nil {
			t.Fatal(err)
		}
		return created
	}
	root := post(other, "", "", "Anyone know whether the balcony has a good view of the stage?")
	must(follows.Set(ctx, owner, event, root.ID, "immediate"))
	answer := post(owner, root.ID, root.ID, "Front balcony is great. The back rows are under an overhang, so sound gets a little muddy.")
	post(other, root.ID, answer.ID, "Thanks, that's really helpful. Booking front balcony now.")
	must(p.Discussions().React(ctx, other, answer.ID, true))
	second := post(owner, "", "", "Is there step-free access from the main entrance?")
	must(p.Discussions().React(ctx, moderator, second.ID, true))
	reported := post(other, root.ID, root.ID, "DM me for cheap tickets!!! (fixture post for the moderation queue)")
	must3(p.Moderation().Report(ctx, owner, reported.ID, "spam", "Looks like a ticket scam."))

	listener, err := net.Listen("tcp", addr)
	if err != nil {
		t.Fatal(err)
	}
	base := "http://" + listener.Addr().String()
	config := accounts.Config{Enabled: true, BaseURL: base, ClientID: "fixture", ClientSecret: "fixture"}
	live := activity.New(ctx, p.Activity(), 256)
	t.Cleanup(live.Close)
	recommendations := p.Recommendations()
	recommendations.EnableLiveActivity(true)
	app, err := api.NewAPI(ctx, zerolog.Nop(), cache,
		api.WithSavedEvents(p.Saved()), api.WithReminders(p.Reminders()), api.WithRadar(p.Radar(), time.Hour),
		api.WithFollows(p.Follows()), api.WithEventInterests(p.Interests()), api.WithEventRecommendations(recommendations),
		api.WithLiveActivity(live, 60), api.WithEventDiscussions(p.Discussions()), api.WithEventActivity(p.EventActivity()),
		api.WithDiscussionFollows(p.DiscussionFollows()), api.WithModeration(p.Moderation()), api.WithAccounts(config, auth))
	if err != nil {
		t.Fatal(err)
	}
	server := &http.Server{Handler: app.Routes(), ReadHeaderTimeout: 10 * time.Second}
	served := make(chan error, 1)
	go func() { served <- server.Serve(listener) }()
	t.Cleanup(func() { _ = server.Close() })

	if err := os.MkdirAll(stateDir, 0o700); err != nil {
		t.Fatal(err)
	}
	write := func(name string, v any) {
		t.Helper()
		data, err := json.MarshalIndent(v, "", "  ")
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(stateDir, name), data, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	accountsManifest := map[string]any{}
	for key, a := range people {
		state := filepath.Join(stateDir, key+".json")
		write(key+".json", map[string]any{"origins": []any{}, "cookies": []map[string]any{{
			"name": config.CookieName("session"), "value": tokens[key], "domain": host, "path": "/",
			"expires": -1, "httpOnly": true, "secure": false, "sameSite": "Lax",
		}}})
		accountsManifest[key] = map[string]string{"id": a.ID, "display_name": a.DisplayName, "storage_state": state}
	}
	discussion := base + "/events/" + url.PathEscape(event) + "/discussions/" + root.ID
	write("manifest.json", map[string]any{
		"base_url": base, "accounts": accountsManifest,
		"paths": map[string]string{
			"discover": "/?" + q.Values().Encode(), "event": "/events/" + url.PathEscape(event),
			"saved": "/saved", "radar": "/radar", "me": "/me", "preferences": "/me/preferences",
			"interests": "/me/interests", "reminders": "/me/reminders", "discussions": "/me/discussions",
			"notifications": "/me/notifications", "community": "/community", "follows": "/follows",
			"happening_now": "/happening-now", "reports": "/me/reports", "moderation_queue": "/moderation",
		},
		"event_ids": func() []string {
			ids := []string{}
			for _, d := range details {
				ids = append(ids, d.Item.ID)
			}
			return ids
		}(),
		"discussion_thread_id": root.ID,
	})
	t.Logf("review server listening on %s", base)
	t.Logf("storage states and manifest: %s", stateDir)
	t.Logf("signed-in discussion example (owner): %s", discussion)
	t.Log("press Ctrl-C to stop and drop the isolated schema")

	stop, cancel := signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)
	defer cancel()
	select {
	case <-stop.Done():
	case err := <-served:
		if !errors.Is(err, http.ErrServerClosed) {
			t.Fatal(err)
		}
	}
	for key := range people {
		_ = os.Remove(filepath.Join(stateDir, key+".json"))
	}
	_ = os.Remove(filepath.Join(stateDir, "manifest.json"))
}

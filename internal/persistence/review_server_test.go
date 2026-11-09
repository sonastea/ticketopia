package persistence

import (
	"crypto/sha256"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"maps"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/signal"
	"path/filepath"
	"slices"
	"strings"
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

var reviewScenarioName = flag.String("review.scenario", "baseline", "TestMariaDBReviewServer fixture scenario")

// reviewFixture is the seeded baseline that scenarios add to.
type reviewFixture struct {
	t       *testing.T
	p       *Pool
	people  map[string]accounts.Account
	follows *discussionfollows.Service
	post    func(who, event, thread, parent, body string) discussions.Post
}

// A reviewScenario adds to the baseline. events receives a builder for
// Chicago fixture events and returns extra events for the Discover results
// and event cache; seed adds persisted state and returns manifest paths.
type reviewScenario struct {
	events func(newEvent func(name string) models.EventDetail) []models.EventDetail
	seed   func(f reviewFixture, events []models.EventDetail) map[string]string
}

var reviewScenarios = map[string]reviewScenario{
	"baseline":         {},
	"event-edge-cases": {events: edgeCaseEvents, seed: seedEdgeCases},
}

func reviewScenarioNames() string {
	names := make([]string, 0, len(reviewScenarios))
	for name := range reviewScenarios {
		names = append(names, name)
	}
	slices.Sort(names)
	return strings.Join(names, ", ")
}

func loopbackAddr(addr string) bool {
	host, _, err := net.SplitHostPort(addr)
	ip := net.ParseIP(host)
	return err == nil && ip != nil && ip.IsLoopback()
}

// Interactive manual-review server: production handlers over an isolated schema
// with fixture accounts signed in through the real login service. Credentials
// reach the browser only as Playwright storage-state files; there is no runtime
// auth bypass. Stop with Ctrl-C so the schema and users are dropped.
func TestMariaDBReviewServer(t *testing.T) {
	addr := os.Getenv("REVIEW_SERVER_ADDR")
	if addr == "" {
		t.Skip("set REVIEW_SERVER_ADDR (for example 127.0.0.1:8090) to serve signed-in review fixtures")
	}
	if !loopbackAddr(addr) {
		t.Fatal("REVIEW_SERVER_ADDR must be a loopback IP and port")
	}
	host, _, _ := net.SplitHostPort(addr)
	// The review server creates schemas and users; keep it on a local database.
	if !loopbackAddr(os.Getenv("MARIADB_TEST_ADDR")) {
		t.Fatal("MARIADB_TEST_ADDR must be a loopback IP and port for the review server")
	}
	scenario, ok := reviewScenarios[*reviewScenarioName]
	if !ok {
		t.Fatalf("unknown review scenario %q; choose one of: %s", *reviewScenarioName, reviewScenarioNames())
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
	newEvent := func(i int, name string) models.EventDetail {
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
		return d
	}
	details := []models.EventDetail{}
	for i, name := range names {
		details = append(details, newEvent(i, name))
	}
	var extra []models.EventDetail
	if scenario.events != nil {
		next := len(details)
		extra = scenario.events(func(name string) models.EventDetail {
			next++
			return newEvent(next-1, name)
		})
	}
	entries := []models.Event{}
	for _, d := range slices.Concat(details, extra) {
		entries = append(entries, d.Item)
		seed("ticketmaster:v1:event:"+d.Item.ID, d.Item)
	}
	// Match the owner's account-derived city AND country defaults. ParseQuery
	// supplies the same normalized dates, limit and filters as the HTML handler.
	// Seeds use the start time's dates: after UTC or Chicago midnight the default
	// query changes and, with the provider disabled, `/` shows an error; restart.
	q, err := discovery.ParseQuery(url.Values{"city": {city}, "country": {country}}, now)
	if err != nil {
		t.Fatal(err)
	}
	seedResults := func(q discovery.Query) {
		sum := sha256.Sum256([]byte(q.Values().Encode()))
		seed(fmt.Sprintf("ticketmaster:v1:events:%x:0", sum), map[string]any{"items": entries, "total": len(entries), "total_pages": 1})
	}
	seedResults(q)
	// The first results read learns Chicago's zone. During a US evening the
	// handler repeats the search with the local day, so seed that normalized
	// query too, following localizeDefaultDates' ParseQuery path.
	localValues := q.Values()
	localValues.Set("start_date", now.In(chicago).Format(time.DateOnly))
	localValues.Del("end_date")
	localQuery, err := discovery.ParseQuery(localValues, now)
	if err != nil {
		t.Fatal(err)
	}
	seedResults(localQuery)

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
	postTo := func(who, event, thread, parent, body string) discussions.Post {
		t.Helper()
		created, _, err := talk.Create(ctx, who, event, thread, parent, body, accounts.ID())
		if err != nil {
			t.Fatal(err)
		}
		return created
	}
	post := func(who, thread, parent, body string) discussions.Post {
		t.Helper()
		return postTo(who, event, thread, parent, body)
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
	scenarioPaths := map[string]string{}
	if scenario.seed != nil {
		scenarioPaths = scenario.seed(reviewFixture{t: t, p: p, people: people, follows: follows, post: postTo}, extra)
	}

	listener, err := net.Listen("tcp", addr)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = listener.Close() })
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
	routes := app.Routes()
	// Verify the actual signed-in default-query path, rather than relying only
	// on a manually reproduced cache-key format while the provider is disabled.
	request := httptest.NewRequest(http.MethodGet, base+"/", nil)
	request.AddCookie(&http.Cookie{Name: config.CookieName("session"), Value: tokens["owner"]})
	response := httptest.NewRecorder()
	routes.ServeHTTP(response, request)
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `data-event-id="`+entries[0].ID+`"`) {
		t.Fatalf("owner default Discover did not use seeded results: status %d", response.Code)
	}
	server := &http.Server{Handler: routes, ReadHeaderTimeout: 10 * time.Second}
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
			for _, e := range entries {
				ids = append(ids, e.ID)
			}
			return ids
		}(),
		"discussion_thread_id": root.ID,
		"scenario":             *reviewScenarioName,
		"scenario_paths":       scenarioPaths,
	})
	t.Logf("review server listening on %s (scenario %s)", base, *reviewScenarioName)
	for _, name := range slices.Sorted(maps.Keys(scenarioPaths)) {
		t.Logf("scenario %s: %s%s", name, base, scenarioPaths[name])
	}
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

// edgeCaseEvents covers event-page states the baseline lacks: a long title with
// no price or presale details while rescheduled, and a canceled event.
func edgeCaseEvents(newEvent func(name string) models.EventDetail) []models.EventDetail {
	rescheduled := newEvent("Rescheduled: an evening of extended orchestral suites, three encores, and a surprise guest whose name has not been announced yet")
	rescheduled.Item.Status = "rescheduled"
	rescheduled.Item.PriceRanges, rescheduled.Item.Presales = nil, nil
	rescheduled.Item.Info = strings.Repeat("The original date moved after a venue scheduling conflict; existing tickets remain valid for the new date. ", 4)
	canceled := newEvent("Canceled acoustic set")
	canceled.Item.Status = "canceled"
	return []models.EventDetail{rescheduled, canceled}
}

// seedEdgeCases saves both events and gives the rescheduled one a busy
// discussion the owner follows, so replies from others arrive unread.
func seedEdgeCases(f reviewFixture, events []models.EventDetail) map[string]string {
	t, ctx := f.t, f.t.Context()
	owner, other, moderator := f.people["owner"].ID, f.people["other"].ID, f.people["moderator"].ID
	for _, d := range events {
		if _, _, err := f.p.Saved().Save(ctx, owner, d); err != nil {
			t.Fatal(err)
		}
	}
	busy, canceled := events[0].Item.ID, events[1].Item.ID
	react := func(who, id string) {
		t.Helper()
		if _, err := f.p.Discussions().React(ctx, who, id, true); err != nil {
			t.Fatal(err)
		}
	}
	root := f.post(other, busy, "", "", "Does anyone know if tickets from the original date carry over automatically, or do we need to exchange them?")
	if _, err := f.follows.Set(ctx, owner, busy, root.ID, "immediate"); err != nil {
		t.Fatal(err)
	}
	answer := f.post(moderator, busy, root.ID, root.ID, "They carry over. The venue emailed everyone; check spam if you didn't see it.")
	react(owner, answer.ID)
	react(other, answer.ID)
	f.post(owner, busy, root.ID, answer.ID, "Mine carried over, but my seat section changed. Did anyone else see that?")
	for i, body := range []string{
		"Same here, moved from the floor to the lower balcony.",
		"Mine stayed the same. Maybe it depends on when you bought?",
		"The box office said section changes were only for the front rows because the stage is bigger now.",
		"Thanks all, that explains it. I'll call them Monday to confirm.",
	} {
		who := []string{other, moderator}[i%2]
		f.post(who, busy, root.ID, root.ID, body)
	}
	for i, body := range []string{
		"Is the surprise guest likely to be announced before the show?",
		"Any recommendations for dinner nearby that won't make us late?",
		strings.Repeat("Long post check: parking was a mess at the original date, so plan for extra time. ", 6),
	} {
		who := []string{other, moderator, other}[i]
		post := f.post(who, busy, "", "", body)
		if i == 1 {
			react(owner, post.ID)
		}
	}
	return map[string]string{
		"edge_event":      "/events/" + url.PathEscape(busy),
		"canceled_event":  "/events/" + url.PathEscape(canceled),
		"busy_discussion": "/events/" + url.PathEscape(busy) + "/discussions/" + root.ID,
	}
}

package reviewfixture

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"slices"
	"strings"
	"time"

	"github.com/rs/zerolog"
	"github.com/sonastea/ticketopia/internal/accounts"
	"github.com/sonastea/ticketopia/internal/discovery"
	"github.com/sonastea/ticketopia/internal/discussionfollows"
	"github.com/sonastea/ticketopia/internal/discussions"
	"github.com/sonastea/ticketopia/internal/kv"
	"github.com/sonastea/ticketopia/internal/models"
	"github.com/sonastea/ticketopia/internal/persistence"
	"github.com/sonastea/ticketopia/internal/reminders"
	"github.com/sonastea/ticketopia/internal/saved"
)

// A scenario adds to the baseline. events receives a builder for Chicago
// fixture events and returns extra events for Discover results; seed adds
// persisted state and returns manifest paths for the screens it targets.
type scenario struct {
	description string
	events      func(newEvent func(name string) models.EventDetail) []fixtureEvent
	seed        func(f *fixture, events []models.EventDetail) (map[string]string, error)
}

// fixtureEvent omits the detail-cache entry when uncached, so event pages fall
// back to durable saved snapshots or report the provider as unavailable.
type fixtureEvent struct {
	detail   models.EventDetail
	uncached bool
}

var scenarios = map[string]scenario{
	"baseline": {description: "Six Chicago events, saves, interest, recommendations, reminders, a followed discussion with unread replies, and a reported post. Open paths without storage state for the signed-out view."},
	"event-edge-cases": {description: "Adds a rescheduled long-title event without prices, a canceled event, and a busy followed discussion with unread replies.",
		events: edgeCaseEvents, seed: seedEdgeCases},
	"event-data-gaps": {description: "Adds a saved event last refreshed a week ago with no image or venue, served from its durable snapshot, and an unknown event while the provider is off.",
		events: dataGapEvents, seed: seedDataGaps},
	"reminder-states": {description: "Adds a reminder due in about 30 minutes, a stopped reminder, and a reminder for an account with notifications paused (other). No email is sent.",
		events: reminderEvents, seed: seedReminderStates},
}

type ScenarioInfo struct {
	Name        string `json:"name"`
	Description string `json:"description"`
}

func ScenarioNames() []string {
	return slices.Sorted(func(yield func(string) bool) {
		for name := range scenarios {
			if !yield(name) {
				return
			}
		}
	})
}

func Scenarios() []ScenarioInfo {
	infos := []ScenarioInfo{}
	for _, name := range ScenarioNames() {
		infos = append(infos, ScenarioInfo{name, scenarios[name].description})
	}
	return infos
}

type fixture struct {
	ctx           context.Context
	p, operator   *persistence.Pool
	auth          *accounts.Service
	cache         kv.Store
	now           time.Time
	people        map[string]accounts.Account
	tokens        map[string]string
	details       []models.EventDetail
	entries       []models.Event
	follows       *discussionfollows.Service
	talk          *discussions.Service
	paths         map[string]string
	scenarioPaths map[string]string
	threadID      string
}

const (
	city, country, zone = "Chicago", "US", "America/Chicago"
)

func (f *fixture) cacheSeed(key string, data any, fetched time.Time) error {
	encoded, err := json.Marshal(map[string]any{"data": data, "fetched_at": fetched, "fresh_until": fetched.Add(time.Hour), "stale_until": fetched.Add(24 * time.Hour)})
	if err != nil {
		return err
	}
	return f.cache.Set(f.ctx, key, encoded, 24*time.Hour)
}

func (f *fixture) login(subject string) (accounts.Account, string, error) {
	l, _, err := f.auth.Begin(f.ctx, "/me")
	if err != nil {
		return accounts.Account{}, "", err
	}
	a, raw, _, err := f.auth.Complete(f.ctx, l.State, l.Browser, subject)
	return a, raw, err
}

func (f *fixture) newEvent(chicago *time.Location, i int, name string) models.EventDetail {
	start := time.Date(f.now.Year(), f.now.Month(), f.now.Day()+3+i*4, 19, 30, 0, 0, chicago)
	date, clock, tz := start.Format(time.DateOnly), start.Format(time.TimeOnly), zone
	sale := f.now.Add(time.Duration(2+i*24) * time.Hour)
	low, high := "25.00", "40.00"
	id := fmt.Sprintf("Review_%d", i)
	return models.EventDetail{Item: models.Event{
		ID: "ticketmaster:" + id, Name: name, Status: "onsale",
		Source:      models.Source{Provider: "ticketmaster", ID: id, URL: "https://tickets.example/event"},
		Start:       models.EventStart{DateTime: &start, LocalDate: &date, LocalTime: &clock, Timezone: &tz},
		PublicSale:  models.PublicSale{Start: &sale},
		Presales:    []models.Presale{{Name: "Early tickets"}},
		PriceRanges: []models.PriceRange{{Currency: "USD", Min: &low, Max: &high}},
		Venues: []models.Venue{{ID: fmt.Sprintf("ticketmaster:review-venue-%d", i%2), Place: models.Place{
			Name:    []string{"The Riverside Hall", "A Venue With An Unusually Long Name On The Near West Side"}[i%2],
			Address: "123 W Example St", City: city, State: "IL", Country: "United States Of America", CountryCode: country}}},
		Classifications: []models.Classification{{Primary: true, Segment: &models.NamedID{ID: "sports", Name: "Sports"}, Genre: &models.NamedID{ID: "ball", Name: "Ball sports"}}},
		Info:            "Last-known description", PleaseNote: "Venue rules",
	}, Meta: models.Freshness{DataAsOf: f.now}}
}

// seed creates accounts, events, caches and baseline state, then the scenario.
func (f *fixture) seed(sc scenario) error {
	ctx, p := f.ctx, f.p
	f.people, f.tokens = map[string]accounts.Account{}, map[string]string{}
	for _, who := range []struct{ key, name, first string }{
		{"owner", "Riley Review", "Riley"},
		{"other", "Sam Second-Account", "Sam"},
		{"moderator", "Mo Moderator", "Mo"},
	} {
		a, raw, err := f.login("review-" + who.key)
		if err != nil {
			return fmt.Errorf("fixture login: %w", err)
		}
		bio := "Fixture account for manual review."
		if _, err := f.auth.UpdateProfile(ctx, a.ID, accounts.ProfilePatch{DisplayName: &who.name, ActivityFirstName: &who.first, Bio: &bio}); err != nil {
			return err
		}
		a.DisplayName = who.name
		f.people[who.key], f.tokens[who.key] = a, raw
	}
	owner, other, moderator := f.people["owner"].ID, f.people["other"].ID, f.people["moderator"].ID
	c, ct, z := city, country, zone
	if _, err := f.auth.UpdatePreferences(ctx, owner, accounts.PreferencesPatch{City: &c, Country: &ct, Timezone: &z}); err != nil {
		return err
	}
	if _, err := f.operator.SetModerator(ctx, moderator, "review server fixture", true); err != nil {
		return err
	}

	if err := f.cacheSeed("ticketmaster:v1:categories:en", []models.Category{
		{NamedID: models.NamedID{ID: "music", Name: "Music"}, Genres: []models.Genre{}},
		{NamedID: models.NamedID{ID: "sports", Name: "Sports"}, Genres: []models.Genre{}},
		{NamedID: models.NamedID{ID: "arts", Name: "Arts & Theatre"}, Genres: []models.Genre{}},
	}, f.now); err != nil {
		return err
	}
	chicago, err := time.LoadLocation(zone)
	if err != nil {
		return err
	}
	for i, name := range []string{
		"A good night in the city",
		"An event with a very long name that still needs readable dates, a venue, and room for private save controls on a narrow phone",
		"Something new for next weekend",
		"Saturday matinee",
		"Late show at the club",
		"Season opener",
	} {
		f.details = append(f.details, f.newEvent(chicago, i, name))
	}
	all := []fixtureEvent{}
	for _, d := range f.details {
		all = append(all, fixtureEvent{detail: d})
	}
	var extra []models.EventDetail
	if sc.events != nil {
		next := len(f.details)
		for _, e := range sc.events(func(name string) models.EventDetail {
			next++
			return f.newEvent(chicago, next-1, name)
		}) {
			all = append(all, e)
			extra = append(extra, e.detail)
		}
	}
	for _, e := range all {
		f.entries = append(f.entries, e.detail.Item)
		if !e.uncached {
			if err := f.cacheSeed("ticketmaster:v1:event:"+e.detail.Item.ID, e.detail.Item, f.now); err != nil {
				return err
			}
		}
	}
	// Match the owner's account-derived city AND country defaults. ParseQuery
	// supplies the same normalized dates, limit and filters as the HTML handler.
	// Seeds use the start time's dates: after UTC or Chicago midnight the default
	// query changes and, with the provider disabled, `/` shows an error; restart.
	q, err := discovery.ParseQuery(url.Values{"city": {city}, "country": {country}}, f.now)
	if err != nil {
		return err
	}
	seedResults := func(q discovery.Query) error {
		sum := sha256.Sum256([]byte(q.Values().Encode()))
		return f.cacheSeed(fmt.Sprintf("ticketmaster:v1:events:%x:0", sum), map[string]any{"items": f.entries, "total": len(f.entries), "total_pages": 1}, f.now)
	}
	// The first results read learns Chicago's zone. During a US evening the
	// handler repeats the search with the local day, so seed that normalized
	// query too, following localizeDefaultDates' ParseQuery path.
	localValues := q.Values()
	localValues.Set("start_date", f.now.In(chicago).Format(time.DateOnly))
	localValues.Del("end_date")
	localQuery, err := discovery.ParseQuery(localValues, f.now)
	if err != nil {
		return err
	}
	if err := errors.Join(seedResults(q), seedResults(localQuery)); err != nil {
		return err
	}

	var errs []error
	check := func(_ any, err error) { errs = append(errs, err) }
	check3 := func(_, _ any, err error) { errs = append(errs, err) }
	for i, d := range f.details {
		if i < 4 {
			check3(p.Saved().Save(ctx, owner, d))
		}
		if i == 0 || i == 2 {
			check3(p.Interests().Set(ctx, owner, d, []string{"public", "private"}[i/2]))
		}
		if i < 2 {
			check3(p.Recommendations().Set(ctx, other, d, "Great sound and an easy trip home afterwards."))
		}
	}
	check3(p.Interests().Set(ctx, other, f.details[0], "public"))
	check3(p.Recommendations().Set(ctx, owner, f.details[0], "Arrive early; the opener is worth it."))
	check(p.Reminders().Set(ctx, owner, f.details[0].Item.ID, 60))
	check(p.Reminders().Set(ctx, owner, f.details[1].Item.ID, 24*60))
	if err := errors.Join(errs...); err != nil {
		return err
	}

	f.talk = discussions.New(p.Discussions(), saved.New(p.Saved(), nil))
	f.follows = discussionfollows.New(p.DiscussionFollows())
	event := f.details[0].Item.ID
	root, err := f.post(other, event, "", "", "Anyone know whether the balcony has a good view of the stage?")
	if err != nil {
		return err
	}
	if _, err := f.follows.Set(ctx, owner, event, root.ID, "immediate"); err != nil {
		return err
	}
	answer, err := f.post(owner, event, root.ID, root.ID, "Front balcony is great. The back rows are under an overhang, so sound gets a little muddy.")
	if err != nil {
		return err
	}
	if _, err := f.post(other, event, root.ID, answer.ID, "Thanks, that's really helpful. Booking front balcony now."); err != nil {
		return err
	}
	check(p.Discussions().React(ctx, other, answer.ID, true))
	second, err := f.post(owner, event, "", "", "Is there step-free access from the main entrance?")
	if err != nil {
		return err
	}
	check(p.Discussions().React(ctx, moderator, second.ID, true))
	reported, err := f.post(other, event, root.ID, root.ID, "DM me for cheap tickets!!! (fixture post for the moderation queue)")
	if err != nil {
		return err
	}
	check3(p.Moderation().Report(ctx, owner, reported.ID, "spam", "Looks like a ticket scam."))
	if err := errors.Join(errs...); err != nil {
		return err
	}
	f.threadID = root.ID
	f.paths = map[string]string{
		"discover": "/?" + q.Values().Encode(), "event": "/events/" + url.PathEscape(event),
		"discussion": "/events/" + url.PathEscape(event) + "/discussions/" + root.ID,
		"saved":      "/saved", "radar": "/radar", "me": "/me", "preferences": "/me/preferences",
		"interests": "/me/interests", "reminders": "/me/reminders", "discussions": "/me/discussions",
		"notifications": "/me/notifications", "community": "/community", "follows": "/follows",
		"happening_now": "/happening-now", "reports": "/me/reports", "moderation_queue": "/moderation",
	}
	f.scenarioPaths = map[string]string{}
	if sc.seed != nil {
		if f.scenarioPaths, err = sc.seed(f, extra); err != nil {
			return fmt.Errorf("scenario seed: %w", err)
		}
	}
	return nil
}

func (f *fixture) post(who, event, thread, parent, body string) (discussions.Post, error) {
	created, _, err := f.talk.Create(f.ctx, who, event, thread, parent, body, accounts.ID())
	return created, err
}

func (f *fixture) save(owner string, details ...models.EventDetail) error {
	for _, d := range details {
		if _, _, err := f.p.Saved().Save(f.ctx, owner, d); err != nil {
			return err
		}
	}
	return nil
}

func eventPath(id string) string { return "/events/" + url.PathEscape(id) }

// edgeCaseEvents covers event-page states the baseline lacks: a long title with
// no price or presale details while rescheduled, and a canceled event.
func edgeCaseEvents(newEvent func(name string) models.EventDetail) []fixtureEvent {
	rescheduled := newEvent("Rescheduled: an evening of extended orchestral suites, three encores, and a surprise guest whose name has not been announced yet")
	rescheduled.Item.Status = "rescheduled"
	rescheduled.Item.PriceRanges, rescheduled.Item.Presales = nil, nil
	rescheduled.Item.Info = strings.Repeat("The original date moved after a venue scheduling conflict; existing tickets remain valid for the new date. ", 4)
	canceled := newEvent("Canceled acoustic set")
	canceled.Item.Status = "canceled"
	return []fixtureEvent{{detail: rescheduled}, {detail: canceled}}
}

// seedEdgeCases saves both events and gives the rescheduled one a busy
// discussion the owner follows, so replies from others arrive unread.
func seedEdgeCases(f *fixture, events []models.EventDetail) (map[string]string, error) {
	owner, other, moderator := f.people["owner"].ID, f.people["other"].ID, f.people["moderator"].ID
	if err := f.save(owner, events...); err != nil {
		return nil, err
	}
	busy, canceled := events[0].Item.ID, events[1].Item.ID
	var errs []error
	react := func(who, id string) {
		_, err := f.p.Discussions().React(f.ctx, who, id, true)
		errs = append(errs, err)
	}
	root, err := f.post(other, busy, "", "", "Does anyone know if tickets from the original date carry over automatically, or do we need to exchange them?")
	if err != nil {
		return nil, err
	}
	if _, err := f.follows.Set(f.ctx, owner, busy, root.ID, "immediate"); err != nil {
		return nil, err
	}
	answer, err := f.post(moderator, busy, root.ID, root.ID, "They carry over. The venue emailed everyone; check spam if you didn't see it.")
	if err != nil {
		return nil, err
	}
	react(owner, answer.ID)
	react(other, answer.ID)
	_, err = f.post(owner, busy, root.ID, answer.ID, "Mine carried over, but my seat section changed. Did anyone else see that?")
	errs = append(errs, err)
	for i, body := range []string{
		"Same here, moved from the floor to the lower balcony.",
		"Mine stayed the same. Maybe it depends on when you bought?",
		"The box office said section changes were only for the front rows because the stage is bigger now.",
		"Thanks all, that explains it. I'll call them Monday to confirm.",
	} {
		_, err := f.post([]string{other, moderator}[i%2], busy, root.ID, root.ID, body)
		errs = append(errs, err)
	}
	for i, body := range []string{
		"Is the surprise guest likely to be announced before the show?",
		"Any recommendations for dinner nearby that won't make us late?",
		strings.Repeat("Long post check: parking was a mess at the original date, so plan for extra time. ", 6),
	} {
		post, err := f.post([]string{other, moderator, other}[i], busy, "", "", body)
		errs = append(errs, err)
		if i == 1 && err == nil {
			react(owner, post.ID)
		}
	}
	return map[string]string{
		"edge_event":      eventPath(busy),
		"canceled_event":  eventPath(canceled),
		"busy_discussion": eventPath(busy) + "/discussions/" + root.ID,
	}, errors.Join(errs...)
}

// dataGapEvents adds an event without image, venue or place whose saved
// snapshot is a week old. Its detail cache entry is omitted, so the event page
// falls back to the durable snapshot and shows its age.
func dataGapEvents(newEvent func(name string) models.EventDetail) []fixtureEvent {
	stale := newEvent("Pop-up show, venue to be announced")
	stale.Item.Images, stale.Item.Venues, stale.Item.Place = nil, nil, nil
	stale.Meta.DataAsOf = stale.Meta.DataAsOf.Add(-7 * 24 * time.Hour)
	return []fixtureEvent{{detail: stale, uncached: true}}
}

func seedDataGaps(f *fixture, events []models.EventDetail) (map[string]string, error) {
	if err := f.save(f.people["owner"].ID, events...); err != nil {
		return nil, err
	}
	return map[string]string{
		"stale_event": eventPath(events[0].Item.ID),
		// Neither cached nor saved; with the provider off this is unavailable (503).
		"unavailable_event": eventPath("ticketmaster:Review_unavailable"),
	}, nil
}

// reminderEvents adds an event whose public sale starts in 90 minutes.
func reminderEvents(newEvent func(name string) models.EventDetail) []fixtureEvent {
	soon := newEvent("On sale soon: late-night jazz trio")
	sale := soon.Meta.DataAsOf.Add(90 * time.Minute)
	soon.Item.PublicSale.Start = &sale
	return []fixtureEvent{{detail: soon}}
}

// seedReminderStates schedules reminders through the real worker without a
// clock override: fresh snapshots skip provider refreshes, and every due time
// is in the future, so nothing is sent. A send attempt fails the scenario.
func seedReminderStates(f *fixture, events []models.EventDetail) (map[string]string, error) {
	ctx, owner, other := f.ctx, f.people["owner"].ID, f.people["other"].ID
	on, off := true, false
	if _, err := f.auth.UpdatePreferences(ctx, owner, accounts.PreferencesPatch{EmailReminders: &on, NotificationsPaused: &off}); err != nil {
		return nil, err
	}
	if _, err := f.auth.UpdatePreferences(ctx, other, accounts.PreferencesPatch{EmailReminders: &on, NotificationsPaused: &on}); err != nil {
		return nil, err
	}
	soon, stopped := events[0], f.details[2]
	if err := errors.Join(f.save(owner, soon), f.save(other, f.details[0])); err != nil {
		return nil, err
	}
	var errs []error
	set := func(who, event string, lead int) {
		_, err := f.p.Reminders().Set(ctx, who, event, lead)
		errs = append(errs, err)
	}
	set(owner, soon.Item.ID, 60)
	set(other, f.details[0].Item.ID, 60)
	set(owner, stopped.Item.ID, 60)
	errs = append(errs, f.p.Reminders().Remove(ctx, owner, stopped.Item.ID))
	if err := errors.Join(errs...); err != nil {
		return nil, err
	}
	sender := &refusingSender{}
	worker := reminders.NewWorker(f.p.Reminders(), refusingProvider{}, sender, zerolog.Nop(), time.Minute)
	// Each active reminder is claimed once; its next check is its future due time.
	for range 10 {
		if err := worker.Step(ctx); err != nil {
			return nil, fmt.Errorf("reminder scheduling: %w", err)
		}
	}
	if sender.attempts > 0 {
		return nil, fmt.Errorf("reminder scenario attempted %d email sends", sender.attempts)
	}
	return map[string]string{
		"reminders":              "/me/reminders",
		"due_soon_event":         eventPath(soon.Item.ID),
		"stopped_reminder_event": eventPath(stopped.Item.ID),
		"paused_reminders_other": "/me/reminders",
	}, nil
}

type refusingProvider struct{}

func (refusingProvider) RefreshEvent(context.Context, string) (models.EventDetail, error) {
	return models.EventDetail{}, errors.New("review fixture: provider disabled")
}

type refusingSender struct{ attempts int }

func (s *refusingSender) Send(context.Context, reminders.Delivery) (string, string) {
	s.attempts++
	return "failed", ""
}

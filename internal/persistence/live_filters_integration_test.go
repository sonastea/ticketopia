package persistence

import (
	"encoding/json"
	"fmt"
	"os"
	"slices"
	"testing"
	"time"

	"github.com/sonastea/ticketopia/internal/accounts"
	"github.com/sonastea/ticketopia/internal/activity"
	"github.com/sonastea/ticketopia/internal/models"
)

func TestMariaDBLiveFilterBaseline(t *testing.T) {
	if os.Getenv("LIVE_FILTER_BENCHMARK") != "true" {
		t.Skip("set LIVE_FILTER_BENCHMARK=true for populated-window baseline")
	}
	f := newMaria(t)
	f.migrate(t)
	p := f.open(t)
	owner, _ := loginAccount(t, accounts.New(p.Accounts(), accountProviderFixture{}), "live-filter-baseline")
	seeded := 0
	for _, n := range []int{100, 1000, 10000} {
		seedLiveFilterRange(t, p, owner.ID, seeded, n)
		seeded = n
		times := []time.Duration{}
		for range 20 {
			start := time.Now()
			data, err := p.Activity().Candidates(t.Context())
			times = append(times, time.Since(start))
			if err != nil || len(data.Items) != n {
				t.Fatal(n, len(data.Items), err)
			}
		}
		slices.Sort(times)
		t.Logf("groups=%d projection p50=%s p95=%s samples=20", n, times[9], times[18])
	}
	seedLiveFilterRange(t, p, owner.ID, 10000, 10001)
	if _, err := p.Activity().Candidates(t.Context()); err == nil {
		t.Fatal("overflow served a truncated window")
	}
}

func seedLiveFilterWindow(t *testing.T, p *Pool, owner string, n int) {
	t.Helper()
	seedLiveFilterRange(t, p, owner, 0, n)
}
func seedLiveFilterRange(t *testing.T, p *Pool, owner string, start, n int) {
	t.Helper()
	w := p.Recommendations()
	w.EnableLiveActivity(true)
	for i := start; i < n; i++ {
		d := savedDetail(fmt.Sprintf("LiveFilter_%d", i))
		d.Item.Name = fmt.Sprintf("Recent fixture %d", i)
		d.Item.Venues = []models.Venue{{Place: models.Place{City: "London", CountryCode: "GB"}}}
		d.Item.Classifications = []models.Classification{{Segment: &models.NamedID{ID: "sports", Name: "Sports"}}}
		if i == 0 {
			d.Item.Name = "Berlin music beyond twenty"
			d.Item.Venues[0].City = "Berlin"
			d.Item.Venues[0].CountryCode = "DE"
			d.Item.Classifications[0].Segment = &models.NamedID{ID: "Music", Name: "Music"}
		}
		if _, _, err := w.Set(t.Context(), owner, d, ""); err != nil {
			t.Fatal(err)
		}
	}
}
func TestMariaDBLiveFilterWindowAndPreferences(t *testing.T) {
	f := newMaria(t)
	f.migrate(t)
	p := f.open(t)
	owner, _ := loginAccount(t, accounts.New(p.Accounts(), accountProviderFixture{}), "live-filter-window")
	seedLiveFilterWindow(t, p, owner.ID, 26)
	s := activity.New(t.Context(), p.Activity(), 256)
	defer s.Close()
	all, err := s.Snapshot(t.Context())
	if err != nil || len(all.Items) != 20 {
		t.Fatal(all, err)
	}
	for _, item := range all.Items {
		if item.EventID == "ticketmaster:LiveFilter_0" {
			t.Fatal("fixture not beyond global twenty")
		}
	}
	filter := activity.DefaultPreferences().Filters
	filter.Location = activity.Location{Mode: "city", City: "bérlin", Country: "de"}
	filter.CategoryIDs = []string{"Music", "Music"}
	view, err := s.FilteredSnapshot(t.Context(), filter)
	if err != nil || len(view.Items) != 1 || view.Items[0].EventTitle != "Berlin music beyond twenty" {
		t.Fatal(view, err)
	}
	filter.CategoryIDs = []string{"music"}
	view, err = s.FilteredSnapshot(t.Context(), filter)
	if err != nil || len(view.Items) != 0 {
		t.Fatal("category IDs lost case", view, err)
	}
	filter.CategoryIDs = []string{"Music"}
	filter.Location.City = "Berl%"
	view, err = s.FilteredSnapshot(t.Context(), filter)
	if err != nil || len(view.Items) != 0 {
		t.Fatal("wildcard city matched", view, err)
	}
	filter.Location.City = "Berlin"
	filter.Location.Country = "US"
	view, err = s.FilteredSnapshot(t.Context(), filter)
	if err != nil || len(view.Items) != 0 {
		t.Fatal("country ignored", view, err)
	}
	filter.Location.Country = "DE"
	pref, err := p.Accounts().ReplaceLivePreferences(t.Context(), owner.ID, 1, true, filter)
	if err != nil || pref.Revision != 2 {
		t.Fatal(pref, err)
	}
	filter.CategoryIDs = []string{"Music", "Music"}
	pref, err = p.Accounts().ReplaceLivePreferences(t.Context(), owner.ID, 2, true, filter)
	if err != nil || pref.Revision != 2 {
		t.Fatal("canonical no-op bumped revision", pref, err)
	}
	other := f.open(t)
	pref, err = other.Accounts().LivePreferences(t.Context(), owner.ID)
	if err != nil || pref.Filters.Location.City != "Berlin" {
		t.Fatal("filters not durable", pref, err)
	}
	// Accepted metadata corrects both title and city; stale snapshots cannot undo it.
	d := savedDetail("LiveFilter_0")
	d.Meta.DataAsOf = time.Now().UTC()
	d.Item.Name = "Corrected"
	d.Item.Venues = []models.Venue{{Place: models.Place{City: "Paris", CountryCode: "FR"}}}
	if _, _, err = p.Saved().Save(t.Context(), owner.ID, d); err != nil {
		t.Fatal(err)
	}
	data, err := p.Activity().Candidates(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range data.Items {
		if c.Item.EventID == d.Item.ID && (c.Item.EventTitle != "Corrected" || c.Item.City != "Paris") {
			t.Fatal("stale public facts", c)
		}
	}
	// Actual public DTO never exposes the filter facts or owner metadata.
	body, _ := json.Marshal(view)
	var payload map[string]any
	_ = json.Unmarshal(body, &payload)
	if len(payload) != 2 {
		t.Fatal("unexpected snapshot envelope")
	}
	execSQL(t, f.admin, "UPDATE "+f.runtime.Database+".event_snapshots SET snapshot=JSON_SET(snapshot,'$.name',REPEAT('x',65537)) WHERE event_id=?", d.Item.ID)
	if _, err := p.Activity().Candidates(t.Context()); err != activity.ErrMemoryLimit {
		t.Fatal("oversized public fact must fail, not truncate", err)
	}
	execSQL(t, f.admin, "UPDATE "+f.runtime.Database+".event_snapshots SET snapshot=JSON_SET(snapshot,'$.name',NULL) WHERE event_id=?", d.Item.ID)
	data, err = p.Activity().Candidates(t.Context())
	if err != nil || len(data.Items) != 25 {
		t.Fatal("missing title was published", len(data.Items), err)
	}
}

package persistence

import (
	"encoding/json"
	"fmt"
	"os"
	"reflect"
	"slices"
	"testing"
	"time"

	"github.com/sonastea/ticketopia/internal/accounts"
	"github.com/sonastea/ticketopia/internal/activity"
	"github.com/sonastea/ticketopia/internal/models"
)

func TestMariaDBLiveFacetReuseStillRefreshesNamesAndCorrections(t *testing.T) {
	f := newMaria(t)
	f.migrate(t)
	p := f.open(t)
	auth := accounts.New(p.Accounts(), accountProviderFixture{})
	owner, _ := loginAccount(t, auth, "live-facet-reuse")
	name := "Casey"
	if _, err := auth.UpdateProfile(t.Context(), owner.ID, accounts.ProfilePatch{ActivityFirstName: &name}); err != nil {
		t.Fatal(err)
	}
	seedLiveFilterWindow(t, p, owner.ID, 26)
	r := p.Activity()
	first, err := r.Candidates(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	before := r.QueryCount()
	name = ""
	if _, err := auth.UpdateProfile(t.Context(), owner.ID, accounts.ProfilePatch{ActivityFirstName: &name}); err != nil {
		t.Fatal(err)
	}
	warm, err := r.RefreshCandidates(t.Context(), first)
	if err != nil || len(warm.Items) != 26 {
		t.Fatal("warm projection", err)
	}
	if r.QueryCount()-before != 2 {
		t.Fatal("unchanged facets were reread", r.QueryCount()-before)
	}
	for _, c := range warm.Items {
		if c.Item.ActorFirstName != nil {
			t.Fatal("name survived facet reuse")
		}
	}
	d := savedDetail("LiveFilter_0")
	d.Meta.DataAsOf = time.Now().UTC()
	d.Item.Name = "Corrected metadata"
	d.Item.Artists = []models.Artist{{ID: "ticketmaster:NewArtist", Name: "Changed", Source: models.Source{Provider: "ticketmaster", ID: "NewArtist"}}}
	if _, _, err := p.Saved().Save(t.Context(), owner.ID, d); err != nil {
		t.Fatal(err)
	}
	before = r.QueryCount()
	corrected, err := r.RefreshCandidates(t.Context(), warm)
	if err != nil {
		t.Fatal(err)
	}
	if r.QueryCount()-before != 3 {
		t.Fatal("changed facts not read in one batch", r.QueryCount()-before)
	}
	reference, err := r.Candidates(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(corrected.Items, reference.Items) {
		t.Fatal("reused projection differs from full snapshot reference")
	}
	for _, c := range corrected.Items {
		if c.Item.EventID == d.Item.ID && (c.Item.EventTitle != d.Item.Name || !slices.Equal(c.ArtistIDs, []string{"ticketmaster:NewArtist"})) {
			t.Fatal("old facets survived newer metadata")
		}
	}
}

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

func seedLiveEntityWindow(t *testing.T, p *Pool, owner string) {
	t.Helper()
	seedLiveFilterWindow(t, p, owner, 28)
	for i := range 4 {
		d := savedDetail(fmt.Sprintf("LiveFilter_%d", i))
		d.Meta.DataAsOf = time.Now().UTC()
		d.Item.Name = fmt.Sprintf("Entity match %d", i)
		artist, venue := "A", "Hall"
		if i == 1 {
			artist = "B"
		}
		if i == 2 {
			venue = "Club"
		}
		if i == 3 {
			artist = "C"
		}
		d.Item.Artists = []models.Artist{{ID: "ticketmaster:" + artist, Name: "Artist " + artist, Source: models.Source{Provider: "ticketmaster", ID: artist}}}
		// Duplicate and secondary entities exercise full lists, not primary-only matching.
		d.Item.Artists = append(d.Item.Artists, d.Item.Artists[0])
		d.Item.Venues = []models.Venue{{ID: "ticketmaster:Primary", Place: models.Place{City: "Berlin", CountryCode: "DE"}}, {ID: "ticketmaster:" + venue, Place: models.Place{Name: venue, City: "Berlin", CountryCode: "DE"}}}
		d.Item.Classifications = []models.Classification{{Segment: &models.NamedID{ID: "Music", Name: "Music"}}}
		if _, _, err := p.Saved().Save(t.Context(), owner, d); err != nil {
			t.Fatal(err)
		}
	}
}

func TestMariaDBLiveEntityFiltersAndMetadata(t *testing.T) {
	f := newMaria(t)
	f.migrate(t)
	p := f.open(t)
	owner, _ := loginAccount(t, accounts.New(p.Accounts(), accountProviderFixture{}), "live-entities")
	seedLiveEntityWindow(t, p, owner.ID)
	filter := activity.DefaultPreferences().Filters
	filter.Location = activity.Location{Mode: "city", City: "Berlin", Country: "DE"}
	filter.CategoryIDs = []string{"Music"}
	filter.ArtistIDs = []string{"ticketmaster:A"}
	filter.VenueIDs = []string{"ticketmaster:Hall"}
	s := activity.New(t.Context(), p.Activity(), 256)
	defer s.Close()
	view, err := s.FilteredSnapshot(t.Context(), filter)
	if err != nil || len(view.Items) != 1 || view.Items[0].EventID != "ticketmaster:LiveFilter_0" || view.Items[0].RecentRecommendations != 1 {
		t.Fatal(view, err)
	}
	filter.ArtistIDs = []string{"ticketmaster:B", "ticketmaster:A", "ticketmaster:A"}
	view, err = s.FilteredSnapshot(t.Context(), filter)
	if err != nil || len(view.Items) != 2 {
		t.Fatal(view, err)
	}
	pref, err := p.Accounts().ReplaceLivePreferences(t.Context(), owner.ID, 1, true, filter)
	if err != nil || pref.Revision != 2 {
		t.Fatal(pref, err)
	}
	filter.ArtistIDs = []string{"ticketmaster:A", "ticketmaster:B"}
	pref, err = p.Accounts().ReplaceLivePreferences(t.Context(), owner.ID, 2, true, filter)
	if err != nil || pref.Revision != 2 {
		t.Fatal("noncanonical revision", pref, err)
	}
	other := f.open(t)
	pref, err = other.Accounts().LivePreferences(t.Context(), owner.ID)
	if err != nil || len(pref.Filters.ArtistIDs) != 2 || len(pref.Filters.VenueIDs) != 1 {
		t.Fatal(pref, err)
	}
	filter.VenueIDs = []string{}
	view, err = s.FilteredSnapshot(t.Context(), filter)
	if err != nil || len(view.Items) != 3 {
		t.Fatal(view, err)
	}
	filter.ArtistIDs = []string{"ticketmaster:a"}
	view, err = s.FilteredSnapshot(t.Context(), filter)
	if err != nil || len(view.Items) != 0 {
		t.Fatal("ID case ignored", view, err)
	}
	// Corrected lineups remove a match without changing contribution eligibility.
	d := savedDetail("LiveFilter_0")
	d.Meta.DataAsOf = time.Now().UTC()
	d.Item.Name = "Corrected lineup"
	d.Item.Venues = []models.Venue{{ID: "ticketmaster:Corrected", Place: models.Place{City: "Berlin", CountryCode: "DE"}}}
	if _, _, err := other.Saved().Save(t.Context(), owner.ID, d); err != nil {
		t.Fatal(err)
	}
	stale := d
	stale.Meta.DataAsOf = d.Meta.DataAsOf.Add(-time.Hour)
	stale.Item.Artists = []models.Artist{{ID: "ticketmaster:A"}}
	if _, _, err := other.Saved().Save(t.Context(), owner.ID, stale); err != nil {
		t.Fatal(err)
	}
	filter = activity.DefaultPreferences().Filters
	filter.ArtistIDs = []string{"ticketmaster:A"}
	deadline := time.Now().Add(5 * time.Second)
	for {
		view, err = s.FilteredSnapshot(t.Context(), filter)
		if err == nil && len(view.Items) == 1 && view.Items[0].EventID == "ticketmaster:LiveFilter_2" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("metadata correction not visible", view, err)
		}
		time.Sleep(50 * time.Millisecond)
	}
	filter.ArtistIDs = []string{}
	filter.VenueIDs = []string{"ticketmaster:Corrected"}
	view, err = s.FilteredSnapshot(t.Context(), filter)
	if err != nil || len(view.Items) != 1 || view.Items[0].EventTitle != "Corrected lineup" {
		t.Fatal("mixed metadata revisions", view, err)
	}
}

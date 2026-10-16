package persistence

import (
	"fmt"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/sonastea/ticketopia/internal/accounts"
	"github.com/sonastea/ticketopia/internal/discussions"
	"github.com/sonastea/ticketopia/internal/models"
	"github.com/sonastea/ticketopia/internal/recommendations"
	"github.com/sonastea/ticketopia/internal/saved"
)

func TestMariaDBCommunityProjectionMatching(t *testing.T) {
	f := newMaria(t)
	f.migrate(t)
	p := f.open(t)
	owner, _ := loginAccount(t, accounts.New(p.Accounts(), accountProviderFixture{}), "community-filters")
	d := savedDetail("CommunityFilters")
	d.Item.Place = &models.Place{City: "Fallback", CountryCode: "US"}
	d.Item.Venues = []models.Venue{{Place: models.Place{City: "München_%", CountryCode: "null"}}, {Place: models.Place{City: "Second", CountryCode: "GB"}}}
	d.Item.Classifications = []models.Classification{{Segment: &models.NamedID{ID: "Music"}}, {Segment: &models.NamedID{ID: "sports"}}, {Segment: &models.NamedID{ID: "Music"}}}
	if _, _, err := p.Recommendations().Set(t.Context(), owner.ID, d, ""); err != nil {
		t.Fatal(err)
	}
	s := discussions.New(p.Discussions(), saved.New(p.Saved(), nil))
	if _, _, err := s.Create(t.Context(), owner.ID, d.Item.ID, "", "", "Where to meet?", accounts.ID()); err != nil {
		t.Fatal(err)
	}
	check := func(city, country, category string, want int) {
		t.Helper()
		q := recommendations.Query{City: city, Country: country, CategoryID: category, Limit: 10}
		items, err := p.Recommendations().List(t.Context(), "", "", q)
		if err != nil || len(items) != want {
			t.Fatalf("individual %q/%q/%q: %d, %v", city, country, category, len(items), err)
		}
		groups, err := p.Recommendations().Community(t.Context(), q)
		if err != nil || len(groups) != want {
			t.Fatalf("grouped: %d, %v", len(groups), err)
		}
		posts, count, err := p.Discussions().List(t.Context(), "", "", "", discussions.Query{City: city, Country: country, CategoryID: category, Limit: 10, Snapshot: time.Now().Add(time.Hour)})
		if err != nil || len(posts) != want || count != want {
			t.Fatalf("discussions: %d/%d, %v", len(posts), count, err)
		}
	}
	check("munchen_%", "us", "Music", 1)
	check("", "", "sports", 1)
	check("", "", "music", 0)
	check("Fallback", "", "", 0)
	check("Second", "GB", "", 0)
	check("%", "", "", 0)
	check("", "GB", "", 0)
	// Empty strings suppress fallback; absence of venues enables it.
	for i, venues := range [][]models.Venue{{{}}, nil} {
		d.Item.Venues = venues
		d.Meta.DataAsOf = d.Meta.DataAsOf.Add(time.Second)
		if _, _, err := p.Saved().Save(t.Context(), owner.ID, d); err != nil {
			t.Fatal(err)
		}
		check("Fallback", "US", "Music", i)
	}
	old := d
	d.Item.Place = &models.Place{City: strings.Repeat("京", 120), CountryCode: "DE"}
	d.Item.Classifications = nil
	d.Meta.DataAsOf = d.Meta.DataAsOf.Add(time.Second)
	for _, detail := range []models.EventDetail{d, old, old} {
		if _, _, err := p.Saved().Save(t.Context(), owner.ID, detail); err != nil {
			t.Fatal(err)
		}
	}
	check(d.Item.Place.City, "de", "", 1)
	check("Fallback", "US", "Music", 0)
	check("", "", "Music", 0)
}

func TestMariaDBCommunityProjectionMigration(t *testing.T) {
	f := newMaria(t)
	previous := fstest.MapFS{}
	entries, _ := migrations.ReadDir("migrations")
	for _, entry := range entries {
		if entry.Name() < "00014" {
			data, _ := migrations.ReadFile("migrations/" + entry.Name())
			previous[entry.Name()] = &fstest.MapFile{Data: data}
		}
	}
	if err := migrateFS(t.Context(), f.migration, previous); err != nil {
		t.Fatal(err)
	}
	db, err := connect(t.Context(), f.migration)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	a, _ := seedLegacyAccount(t, db, "community-upgrade")
	// Exercise missing and JSON-null fields independently, plus empty strings.
	for i, location := range []string{
		`"venues":[{"city":"First"},{"city":"Second"}],"place":{"city":"Fallback","country_code":"US"}`,
		`"venues":[{"city":null,"country_code":"DE"}],"place":{"city":"Fallback","country_code":"US"}`,
		`"venues":[{"city":"","country_code":""}],"place":{"city":"Fallback","country_code":"US"}`,
	} {
		d := savedDetail(fmt.Sprintf("CommunityUpgrade%d", i))
		seedLegacySavedEvent(t, db, a.ID, d)
		execSQL(t, db, `UPDATE event_snapshots SET snapshot=? WHERE event_id=?`, `{`+location+`,"classifications":[{"segment":{"id":"Music"}},{"segment":{"id":"Music"}}]}`, d.Item.ID)
		execSQL(t, db, `INSERT INTO event_search_facets VALUES (?,'category','wrong')`, d.Item.ID)
	}
	f.migrate(t)
	p := f.open(t)
	for i, want := range [][2]string{{"First", "US"}, {"Fallback", "DE"}, {"", ""}} {
		id := fmt.Sprintf("ticketmaster:CommunityUpgrade%d", i)
		var city, country string
		if err := p.db.QueryRowContext(t.Context(), `SELECT community_city,community_country FROM event_snapshots WHERE event_id=?`, id).Scan(&city, &country); err != nil || city != want[0] || country != want[1] {
			t.Fatalf("backfill %d: %q/%q %v", i, city, country, err)
		}
		var category string
		if err := p.db.QueryRowContext(t.Context(), `SELECT value FROM event_search_facets WHERE event_id=? AND kind='category'`, id).Scan(&category); err != nil || category != "Music" {
			t.Fatal("snapshot category backfill", category, err)
		}
	}
}

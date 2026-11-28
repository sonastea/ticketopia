package discovery

import (
	"net/url"
	"testing"
	"time"
)

// Venues spell some cities with stray spaces (" Long Beach"); stored places
// must match the metro's spelling exactly.
func TestPlaceCityIsTrimmed(t *testing.T) {
	var raw providerPlace
	raw.City.Name, raw.Country.CountryCode = " Long Beach ", "US "
	if p := normalizePlace(raw); p.City != "Long Beach" || p.CountryCode != "US" {
		t.Fatalf("place not trimmed: %q %q", p.City, p.CountryCode)
	}
	seen := map[string]bool{}
	for _, city := range metros[0].Cities {
		if seen[city] {
			t.Fatalf("duplicate member %s", city)
		}
		seen[city] = true
	}
}

func TestAreaAndFoldParsing(t *testing.T) {
	for _, tt := range []struct {
		raw, area, country string
		cities             int
	}{
		// A member city, typed in any case without a country, reaches its metro;
		// the search's country stays as given.
		{"city=inglewood&area=metro", AreaMetro, "", len(metros[0].Cities)},
		{"city=Los+Angeles&country=us&area=metro", AreaMetro, "US", len(metros[0].Cities)},
		// Choosing only the city is kept, so the web can remember it.
		{"city=Los+Angeles&country=US&area=city", AreaCity, "US", 1},
		// Without a metro, either choice is the ordinary city search.
		{"city=Boston&country=US&area=metro", "", "US", 1},
		{"city=Boston&area=city", "", "", 1},
		// Another country's same-named city is not in the metro.
		{"city=Glendale&country=AU&area=metro", "", "AU", 1},
		{"city=Los+Angeles&country=US", "", "US", 1},
	} {
		q := testQuery(t, tt.raw)
		if q.Area != tt.area || q.Country != tt.country || len(q.Cities()) != tt.cities {
			t.Errorf("%s: area=%q country=%q cities=%d", tt.raw, q.Area, q.Country, len(q.Cities()))
		}
	}
	for _, raw := range []string{"city=Boston&area=nearby", "city=Boston&fold=day", "city=Boston&area=metro&area=city"} {
		values, _ := url.ParseQuery(raw)
		if _, err := ParseQuery(values, time.Now()); err == nil {
			t.Errorf("%s accepted", raw)
		}
	}
	// Area and fold change results, so they bind cursors and cache keys.
	exact, metro := testQuery(t, "city=Los+Angeles&country=US"), testQuery(t, "city=Los+Angeles&country=US&area=metro")
	folded := testQuery(t, "city=Los+Angeles&country=US&fold=run")
	if exact.CountKey() == metro.CountKey() || exact.CountKey() == folded.CountKey() {
		t.Fatal("area or fold shared a count key with the plain search")
	}
	values := metro.Values()
	values.Set("cursor", metro.NextCursor(Cursor{Date: metro.StartDate, ID: "ticketmaster:a", Total: 50}))
	values.Del("area")
	if _, err := ParseQuery(values, time.Now()); err == nil {
		t.Fatal("a metro cursor resumed an exact-city search")
	}
	// Collection stays per city: a metro is collected as its members.
	members := metro.MemberQueries()
	if len(members) != len(metros[0].Cities) || members[1].Area != "" || members[1].City != metros[0].Cities[1] || metro.CollectionScope().Area != "" {
		t.Fatalf("metro members: %+v", members[:2])
	}
}

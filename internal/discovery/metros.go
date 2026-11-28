package discovery

import "strings"

// Area values. An empty Area is an exact-city search, which the JSON API and
// agent tools keep by default. AreaCity is the same search chosen explicitly,
// so the web can remember "only this city" for a city that has a metro.
const (
	AreaCity  = "city"
	AreaMetro = "metro"
)

// Metro is a fixed set of venue cities that people treat as one place. Each
// member is collected and covered as its own city scope; a metro only widens
// which stored places a search reads.
type Metro struct {
	Name    string
	Country string
	Cities  []string
}

// Venue city names as Ticketmaster spells them. Membership is curated, not
// geographic: a name that matches no venue simply contributes no events, and
// each member costs a provider request per refresh.
//
// The Los Angeles area is Los Angeles County south of the mountains plus
// Orange County: the venue cities Ticketmaster lists within about 50 miles of
// downtown (audited 2026-11-27 with a radius venue search). It leaves out the
// Antelope Valley (Palmdale, Lancaster), the Inland Empire (Ontario, Corona)
// and Ventura County (Thousand Oaks).
var metros = []Metro{{
	Name: "Los Angeles area", Country: "US",
	Cities: []string{
		// Los Angeles and its neighbourhoods that venues list as cities.
		"Los Angeles", "Hollywood", "West Hollywood", "North Hollywood", "Studio City",
		"Universal City", "Sherman Oaks", "Van Nuys", "Highland Park", "Venice",
		"Pacific Palisades", "San Pedro",
		// Los Angeles County.
		"Burbank", "Glendale", "Pasadena", "South Pasadena", "Arcadia", "Inglewood",
		"Culver City", "Santa Monica", "Malibu", "Agoura Hills", "Beverly Hills",
		"El Segundo", "Redondo Beach", "Torrance", "Carson", "Long Beach",
		"Huntington Park", "Commerce", "Pico Rivera", "Downey", "Whittier", "Cerritos",
		"La Mirada", "West Covina", "Pomona", "Claremont",
		// Orange County.
		"Anaheim", "Santa Ana", "Costa Mesa", "Irvine", "Fullerton", "Brea",
		"Yorba Linda", "Garden Grove", "Cypress", "Huntington Beach", "Newport Beach",
		"Laguna Beach", "Aliso Viejo", "Mission Viejo", "San Juan Capistrano",
	},
}}

// MetroFor returns the metro containing city. An empty country matches the
// metro's own country, so a typed city without a country still finds it.
func MetroFor(city, country string) (Metro, bool) {
	city = strings.Join(strings.Fields(city), " ")
	for _, m := range metros {
		if country != "" && !strings.EqualFold(country, m.Country) {
			continue
		}
		for _, member := range m.Cities {
			if strings.EqualFold(member, city) {
				return m, true
			}
		}
	}
	return Metro{}, false
}

// normalizeArea keeps a requested area only where it changes the search: a
// metro needs a member city, and choosing only the city is meaningful only
// when the city has a metro. The search's own country is left as given; a
// metro reads and collects its members in the metro's country.
func (q Query) normalizeArea(area string) Query {
	q.Area = ""
	if area != AreaMetro && area != AreaCity {
		return q
	}
	if _, ok := MetroFor(q.City, q.Country); !ok || q.City == "" {
		return q
	}
	q.Area = area
	return q
}

// WithArea returns q searching area, normalized as ParseQuery does. The web
// uses it once a remembered or detected city has filled the search.
func (q Query) WithArea(area string) Query { return q.normalizeArea(area) }

// HasMetro reports whether q's city belongs to a metro.
func (q Query) HasMetro() bool {
	_, ok := MetroFor(q.City, q.Country)
	return ok && q.City != ""
}

// Metro returns the metro this search reads, if it is a metro search.
func (q Query) Metro() (Metro, bool) {
	if q.Area != AreaMetro || q.City == "" {
		return Metro{}, false
	}
	return MetroFor(q.City, q.Country)
}

// Cities lists the venue cities a search reads: a metro's members, or the
// search's own city.
func (q Query) Cities() []string {
	if m, ok := q.Metro(); ok {
		return m.Cities
	}
	if q.City == "" {
		return nil
	}
	return []string{q.City}
}

// MemberQueries are the per-city collection scopes behind a search: one per
// metro member, or the search itself. Each is an ordinary exact-city query.
func (q Query) MemberQueries() []Query {
	m, ok := q.Metro()
	if !ok {
		q.Area, q.Fold = "", ""
		return []Query{q}
	}
	members := make([]Query, 0, len(m.Cities))
	for _, city := range m.Cities {
		member := q
		member.City, member.Country, member.Area, member.Fold = city, m.Country, "", ""
		members = append(members, member)
	}
	return members
}

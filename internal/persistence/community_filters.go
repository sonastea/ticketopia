package persistence

import (
	"encoding/json"

	"github.com/sonastea/ticketopia/internal/models"
)

// Community reads alias the authoritative event snapshot as e. Unlike Discover,
// location matching is first-venue/per-field fallback, never any-venue matching.
// Equality keeps city wildcards literal and category identities case-sensitive.
func communityFilters(city, country, category string) (string, []any) {
	where, args := "", []any{}
	if city != "" {
		where += ` AND e.community_city=?`
		args = append(args, city)
	}
	if country != "" {
		where += ` AND e.community_country=?`
		args = append(args, country)
	}
	if category != "" {
		where += ` AND EXISTS (SELECT 1 FROM event_search_facets cf WHERE cf.event_id=e.event_id AND cf.kind='category' AND cf.value=?)`
		args = append(args, category)
	}
	return where, args
}

// firstOfEvent flags one row per event of a bounded key page, which must
// select event_id. The window sees only the limited keys, never snapshots, so
// the page's joined statement sends each event's snapshot once.
func firstOfEvent(keys, columns string) string {
	return `SELECT ` + columns + `,ROW_NUMBER() OVER (PARTITION BY event_id)=1 AS first FROM (` + keys + `) k`
}

// pageEvents decodes each distinct event of a page once from the one flagged
// row that carries its snapshot. Rows of one event share the decoded slices and
// pointers, which callers only read. A missing or undecodable snapshot is false.
func pageEvents(ids []string, snapshots [][]byte) (map[string]models.Event, bool) {
	events := map[string]models.Event{}
	for i, data := range snapshots {
		if data == nil {
			continue
		}
		var e models.Event
		if json.Unmarshal(data, &e) != nil {
			return nil, false
		}
		events[ids[i]] = e
	}
	for _, id := range ids {
		if _, ok := events[id]; !ok {
			return nil, false
		}
	}
	return events, true
}

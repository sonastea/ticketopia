package persistence

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

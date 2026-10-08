package discovery

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"
)

const musicSegment = "KZFzniwnSyZfZ7v7nJ"

var sourceIDPattern = regexp.MustCompile(`^[A-Za-z0-9_-]{1,128}$`)

type ValidationError struct {
	Field   string
	Message string
}

func (e *ValidationError) Error() string { return e.Field + ": " + e.Message }

type Query struct {
	CategoryID                                         string
	City, Country, Keyword, GenreID, ArtistID, VenueID string
	StartDate, EndDate                                 string
	Limit, Page                                        int
	Sort                                               string
	Stored                                             bool
}

// ParseQuery canonicalizes every supported filter before cache lookup. Default
// dates are day-bucketed so requests do not create a new cache key every second.
func ParseQuery(values url.Values, now time.Time) (Query, error) {
	allowed := map[string]bool{
		"city": true, "country": true, "keyword": true, "category_id": true, "genre_id": true,
		"artist_id": true, "venue_id": true, "start_date": true,
		"end_date": true, "limit": true, "cursor": true, "sort": true,
	}
	for field, list := range values {
		if !allowed[field] || len(list) != 1 {
			return Query{}, &ValidationError{field, "use one value for a supported filter"}
		}
	}
	text := func(key string) string { return strings.Join(strings.Fields(values.Get(key)), " ") }
	q := Query{
		CategoryID: text("category_id"),
		City:       text("city"), Country: strings.ToUpper(text("country")),
		Keyword: text("keyword"), GenreID: text("genre_id"),
		ArtistID: text("artist_id"), VenueID: text("venue_id"), Limit: 20,
		StartDate: text("start_date"), EndDate: text("end_date"),
		Sort: text("sort"),
	}
	if q.Sort == "" {
		q.Sort = "date_asc"
	}
	if q.Sort != "date_asc" && q.Sort != "date_desc" && q.Sort != "name_asc" {
		return q, &ValidationError{"sort", "use date_asc, date_desc, or name_asc"}
	}
	for name, value := range map[string]string{"city": q.City, "keyword": q.Keyword} {
		if len(value) > 120 {
			return q, &ValidationError{name, "must be 120 bytes or fewer"}
		}
	}
	if q.Country != "" && (len(q.Country) != 2 || q.Country[0] < 'A' || q.Country[0] > 'Z' || q.Country[1] < 'A' || q.Country[1] > 'Z') {
		return q, &ValidationError{"country", "use a two-letter country code, such as US"}
	}
	for name, value := range map[string]string{"artist_id": q.ArtistID, "venue_id": q.VenueID} {
		if value != "" {
			if _, err := sourceID(value); err != nil {
				return q, &ValidationError{name, "use a ticketmaster: prefixed ID from an event"}
			}
		}
	}
	if q.GenreID != "" && !sourceIDPattern.MatchString(q.GenreID) {
		return q, &ValidationError{"genre_id", "use an ID from the genre catalog"}
	}
	// Old genre-only links remain music searches. Explicit all-category searches
	// cannot carry a genre; choose its category first.
	if q.CategoryID == "" && q.GenreID != "" {
		q.CategoryID = musicSegment
	}
	if q.CategoryID == "all" {
		q.CategoryID = ""
		if q.GenreID != "" {
			return q, &ValidationError{"genre_id", "choose a category before filtering by genre"}
		}
	}
	if q.CategoryID != "" && !sourceIDPattern.MatchString(q.CategoryID) {
		return q, &ValidationError{"category_id", "use all or an ID from the category catalog"}
	}
	if value := text("limit"); value != "" {
		limit, err := strconv.Atoi(value)
		if err != nil || limit < 1 || limit > 100 {
			return q, &ValidationError{"limit", "must be an integer from 1 to 100"}
		}
		q.Limit = limit
	}
	if q.StartDate == "" {
		q.StartDate = now.UTC().Format(time.DateOnly)
	}
	start, err := time.Parse(time.DateOnly, q.StartDate)
	if err != nil {
		return q, &ValidationError{"start_date", "use YYYY-MM-DD"}
	}
	if q.EndDate == "" {
		q.EndDate = start.AddDate(0, 0, 90).Format(time.DateOnly)
	}
	end, err := time.Parse(time.DateOnly, q.EndDate)
	if err != nil || end.Before(start) || end.Sub(start) > 366*24*time.Hour {
		return q, &ValidationError{"end_date", "use YYYY-MM-DD, from the start date through 366 days later"}
	}
	if encoded := values.Get("cursor"); encoded != "" {
		var cursor pageCursor
		decoded, err := base64.RawURLEncoding.DecodeString(encoded)
		if len(encoded) > 512 || err != nil || json.Unmarshal(decoded, &cursor) != nil || (cursor.Version != 1 && cursor.Version != 2) || cursor.Filters != q.fingerprint() || cursor.Page < 1 || (cursor.Version == 1 && cursor.Page > 999/q.Limit) || cursor.Page > 1000000/q.Limit {
			return q, &ValidationError{"cursor", "invalid cursor or filters changed; start a new search"}
		}
		q.Page = cursor.Page
		q.Stored = cursor.Version == 2
	}
	return q, nil
}

func sourceID(id string) (string, error) {
	raw, ok := strings.CutPrefix(id, "ticketmaster:")
	if !ok || !sourceIDPattern.MatchString(raw) {
		return "", &ValidationError{"event_id", "use a ticketmaster: prefixed ID"}
	}
	return raw, nil
}

func (q Query) Values() url.Values {
	values := url.Values{
		"category_id": {q.CategoryValue()},
		"start_date":  {q.StartDate}, "end_date": {q.EndDate}, "limit": {strconv.Itoa(q.Limit)},
	}
	if q.Sort != "" && q.Sort != "date_asc" {
		values.Set("sort", q.Sort)
	}
	for key, value := range map[string]string{
		"city": q.City, "country": q.Country, "keyword": q.Keyword,
		"genre_id": q.GenreID, "artist_id": q.ArtistID, "venue_id": q.VenueID,
	} {
		if value != "" {
			values.Set(key, value)
		}
	}
	return values
}

// CategoryValue keeps all-category URLs explicit and separates their cache and
// cursor fingerprints from the old implicit music-only queries.
func (q Query) CategoryValue() string {
	if q.CategoryID == "" {
		return "all"
	}
	return q.CategoryID
}

func (q Query) upstream() url.Values {
	values := url.Values{
		"locale": {"en"}, "sort": {"date,name,asc"},
		"size": {strconv.Itoa(q.Limit)}, "page": {strconv.Itoa(q.Page)},
		"localStartDateTime": {q.StartDate + "T00:00:00," + q.EndDate + "T23:59:59"},
	}
	if q.Sort == "date_desc" {
		values.Set("sort", "date,name,desc")
	}
	if q.Sort == "name_asc" {
		values.Set("sort", "name,asc")
	}
	for key, value := range map[string]string{
		"segmentId": q.CategoryID,
		"city":      q.City, "countryCode": q.Country, "keyword": q.Keyword, "genreId": q.GenreID,
		"attractionId": strings.TrimPrefix(q.ArtistID, "ticketmaster:"),
		"venueId":      strings.TrimPrefix(q.VenueID, "ticketmaster:"),
	} {
		if value != "" {
			values.Set(key, value)
		}
	}
	return values
}

type pageCursor struct {
	Version int    `json:"v"`
	Page    int    `json:"p"`
	Filters string `json:"f"`
}

func (q Query) fingerprint() string {
	sum := sha256.Sum256([]byte(q.Values().Encode()))
	return hex.EncodeToString(sum[:])
}

func (q Query) nextCursor() string {
	v := 1
	if q.Stored {
		v = 2
	}
	data, _ := json.Marshal(pageCursor{v, q.Page + 1, q.fingerprint()})
	return base64.RawURLEncoding.EncodeToString(data)
}

func (q Query) NextCursor() string { q.Stored = true; return q.nextCursor() }

// CollectionScope collects broadly once, then filters locally. A city search
// never stores a user's keyword or their private participation/preferences.
func (q Query) CollectionScope() Query {
	q.Keyword, q.CategoryID, q.GenreID, q.Sort = "", "", "", "date_asc"
	if q.City != "" {
		q.ArtistID, q.VenueID = "", ""
	}
	q.Limit, q.Page, q.Stored = 100, 0, false
	return q
}

func (q Query) ScopeID() string {
	q = q.CollectionScope()
	q.City = strings.ToLower(q.City)
	return q.fingerprint()
}

func (q Query) cacheKey() string {
	return fmt.Sprintf("ticketmaster:v1:events:%s:%d", q.fingerprint(), q.Page)
}

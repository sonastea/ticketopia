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
	City, Country, Keyword, GenreID, ArtistID, VenueID string
	StartDate, EndDate                                 string
	Limit, Page                                        int
}

// ParseQuery canonicalizes every supported filter before cache lookup. Default
// dates are day-bucketed so requests do not create a new cache key every second.
func ParseQuery(values url.Values, now time.Time) (Query, error) {
	allowed := map[string]bool{
		"city": true, "country": true, "keyword": true, "genre_id": true,
		"artist_id": true, "venue_id": true, "start_date": true,
		"end_date": true, "limit": true, "cursor": true,
	}
	for field, list := range values {
		if !allowed[field] || len(list) != 1 {
			return Query{}, &ValidationError{field, "use one value for a supported filter"}
		}
	}
	text := func(key string) string { return strings.Join(strings.Fields(values.Get(key)), " ") }
	q := Query{
		City: text("city"), Country: strings.ToUpper(text("country")),
		Keyword: text("keyword"), GenreID: text("genre_id"),
		ArtistID: text("artist_id"), VenueID: text("venue_id"), Limit: 20,
		StartDate: text("start_date"), EndDate: text("end_date"),
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
		if len(encoded) > 512 || err != nil || json.Unmarshal(decoded, &cursor) != nil || cursor.Version != 1 || cursor.Filters != q.fingerprint() || cursor.Page < 1 || cursor.Page > 999/q.Limit {
			return q, &ValidationError{"cursor", "invalid cursor or filters changed; start a new search"}
		}
		q.Page = cursor.Page
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
		"start_date": {q.StartDate}, "end_date": {q.EndDate}, "limit": {strconv.Itoa(q.Limit)},
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

func (q Query) upstream() url.Values {
	values := url.Values{
		"segmentId": {musicSegment}, "locale": {"en"}, "sort": {"date,name,asc"},
		"size": {strconv.Itoa(q.Limit)}, "page": {strconv.Itoa(q.Page)},
		"localStartDateTime": {q.StartDate + "T00:00:00," + q.EndDate + "T23:59:59"},
	}
	for key, value := range map[string]string{
		"city": q.City, "countryCode": q.Country, "keyword": q.Keyword, "genreId": q.GenreID,
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
	data, _ := json.Marshal(pageCursor{1, q.Page + 1, q.fingerprint()})
	return base64.RawURLEncoding.EncodeToString(data)
}

func (q Query) cacheKey() string {
	return fmt.Sprintf("ticketmaster:v1:events:%s:%d", q.fingerprint(), q.Page)
}

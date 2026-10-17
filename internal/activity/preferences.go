package activity

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"reflect"
	"regexp"
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"
)

type Location struct {
	Mode    string `json:"mode"`
	City    string `json:"city,omitempty"`
	Country string `json:"country,omitempty"`
}
type Connections struct {
	Mode    string   `json:"mode"`
	Sources []string `json:"sources"`
}
type Filters struct {
	Location    Location    `json:"location"`
	CategoryIDs []string    `json:"category_ids"`
	ArtistIDs   []string    `json:"artist_ids"`
	VenueIDs    []string    `json:"venue_ids"`
	Connections Connections `json:"connections"`
}
type Preferences struct {
	Version int     `json:"version"`
	Enabled bool    `json:"enabled"`
	Filters Filters `json:"filters"`
}

func DefaultPreferences() Preferences {
	return Preferences{1, true, Filters{Location{Mode: "all"}, []string{}, []string{}, []string{}, Connections{"all_events", []string{}}}}
}

var ErrPreferences = errors.New("provide complete version 1 preferences with Everywhere or city/country and up to 16 category IDs; other filters are not supported")

var categoryID = regexp.MustCompile(`^[A-Za-z0-9_-]{1,64}$`)

func NormalizeFilters(f Filters) (Filters, error) {
	if f.CategoryIDs == nil || f.ArtistIDs == nil || f.VenueIDs == nil || f.Connections.Sources == nil || len(f.CategoryIDs) > 16 || len(f.ArtistIDs) != 0 || len(f.VenueIDs) != 0 || f.Connections.Mode != "all_events" || len(f.Connections.Sources) != 0 {
		return f, ErrPreferences
	}
	switch f.Location.Mode {
	case "all":
		if f.Location.City != "" || f.Location.Country != "" {
			return f, ErrPreferences
		}
	case "city":
		if strings.IndexFunc(f.Location.City, unicode.IsControl) >= 0 {
			return f, ErrPreferences
		}
		f.Location.City = strings.Join(strings.Fields(f.Location.City), " ")
		f.Location.Country = strings.ToUpper(strings.TrimSpace(f.Location.Country))
		if !utf8.ValidString(f.Location.City) || utf8.RuneCountInString(f.Location.City) > 120 || f.Location.City == "" || len(f.Location.Country) != 2 || f.Location.Country[0] < 'A' || f.Location.Country[0] > 'Z' || f.Location.Country[1] < 'A' || f.Location.Country[1] > 'Z' {
			return f, ErrPreferences
		}
	default:
		return f, ErrPreferences
	}
	f.CategoryIDs = append([]string{}, f.CategoryIDs...)
	for _, id := range f.CategoryIDs {
		if !categoryID.MatchString(id) {
			return f, ErrPreferences
		}
	}
	slices.Sort(f.CategoryIDs)
	f.CategoryIDs = slices.Compact(f.CategoryIDs)
	return f, nil
}

// DecodePublicFilters uses the same strict decoder without accepting owner fields.
func DecodePublicFilters(data []byte) (Filters, error) {
	f := DefaultPreferences().Filters
	if len(data) > 4096 {
		return f, ErrPreferences
	}
	data = bytes.TrimSpace(data)
	var fields map[string]json.RawMessage
	if json.Unmarshal(data, &fields) != nil || fields == nil {
		return f, ErrPreferences
	}
	for k := range fields {
		if k != "location" && k != "category_ids" && k != "artist_ids" && k != "venue_ids" {
			return f, ErrPreferences
		}
	}
	// Preserve the original bytes so duplicate keys cannot disappear in a map.
	body := append([]byte(`{"version":1,"enabled":true,"filters":`), data...)
	// Missing public dimensions are filled before strict preference validation.
	var raw map[string]json.RawMessage
	defaults, _ := json.Marshal(f)
	_ = json.Unmarshal(defaults, &raw)
	for k := range fields {
		delete(raw, k)
	}
	if len(data) < 2 || data[len(data)-1] != '}' {
		return f, ErrPreferences
	}
	body = body[:len(body)-1]
	for k, v := range raw {
		if len(fields) > 0 {
			body = append(body, ',')
		}
		key, _ := json.Marshal(k)
		body = append(body, key...)
		body = append(body, ':')
		body = append(body, v...)
		fields[k] = v
	}
	body = append(body, '}', '}')
	p, err := DecodePreferences(body)
	return p.Filters, err
}

// DecodePreferences rejects duplicate keys and null at every depth, and requires
// the full representation. Future capabilities must be enabled deliberately.
func DecodePreferences(data []byte) (Preferences, error) {
	var p Preferences
	if len(data) > 8192 {
		return p, ErrPreferences
	}
	d := json.NewDecoder(bytes.NewReader(data))
	var value func() error
	value = func() error {
		t, e := d.Token()
		if e != nil || t == nil {
			return ErrPreferences
		}
		if delim, ok := t.(json.Delim); ok {
			switch delim {
			case '{':
				keys := map[string]bool{}
				for d.More() {
					k, e := d.Token()
					if e != nil {
						return ErrPreferences
					}
					s, ok := k.(string)
					if !ok || keys[s] {
						return ErrPreferences
					}
					keys[s] = true
					if value() != nil {
						return ErrPreferences
					}
				}
			case '[':
				for d.More() {
					if value() != nil {
						return ErrPreferences
					}
				}
			default:
				return ErrPreferences
			}
			_, e = d.Token()
			return e
		}
		return nil
	}
	if value() != nil {
		return p, ErrPreferences
	}
	if _, e := d.Token(); e != io.EOF {
		return p, ErrPreferences
	}
	d = json.NewDecoder(bytes.NewReader(data))
	d.DisallowUnknownFields()
	if d.Decode(&p) != nil {
		return p, ErrPreferences
	}
	if p.Version != 1 {
		return p, ErrPreferences
	}
	var fields map[string]json.RawMessage
	if json.Unmarshal(data, &fields) != nil || fields["enabled"] == nil {
		return p, ErrPreferences
	}
	// encoding/json accepts case-insensitive field names. This resource does not.
	canonical, _ := json.Marshal(p)
	var actual, expected any
	if json.Unmarshal(data, &actual) != nil || json.Unmarshal(canonical, &expected) != nil || !reflect.DeepEqual(actual, expected) {
		return p, ErrPreferences
	}
	f, err := NormalizeFilters(p.Filters)
	p.Filters = f
	return p, err
}

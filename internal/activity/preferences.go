package activity

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"reflect"
)

type Location struct {
	Mode string `json:"mode"`
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
	return Preferences{1, true, Filters{Location{"all"}, []string{}, []string{}, []string{}, Connections{"all_events", []string{}}}}
}

var ErrPreferences = errors.New("provide the complete version 1 all-events preference; filters are not supported yet")

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
	want := DefaultPreferences()
	want.Enabled = p.Enabled
	if !reflect.DeepEqual(p, want) {
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
	return p, nil
}

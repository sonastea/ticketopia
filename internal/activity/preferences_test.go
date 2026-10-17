package activity

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestPreferenceValidation(t *testing.T) {
	data, _ := json.Marshal(DefaultPreferences())
	valid := string(data)
	for _, body := range []string{valid, strings.Replace(valid, `"enabled":true`, `"enabled":false`, 1)} {
		if _, err := DecodePreferences([]byte(body)); err != nil {
			t.Fatal(err)
		}
	}
	for _, body := range []string{
		`{}`, `null`, valid + `{}`, strings.Repeat(" ", 8193) + valid,
		strings.Replace(valid, `"version":1`, `"version":2`, 1),
		strings.Replace(valid, `"enabled":true,`, "", 1),
		strings.Replace(valid, `"enabled":true`, `"enabled":null`, 1),
		strings.Replace(valid, `"mode":"all"`, `"mode":"all","mode":"all"`, 1),
		strings.Replace(valid, `"mode":"all"`, `"mode":"city","city":"Berlin"`, 1),
		strings.Replace(valid, `"mode":"all"`, `"mode":"radius"`, 1),
		strings.Replace(valid, `"category_ids":[]`, `"category_ids":["invalid/id"]`, 1),
		strings.Replace(valid, `"artist_ids":[]`, `"artist_ids":["ticketmaster:a"]`, 1),
		strings.Replace(valid, `"venue_ids":[]`, `"venue_ids":null`, 1),
		strings.Replace(valid, `"sources":[]`, `"sources":["saved"]`, 1),
		strings.Replace(valid, `"mode":"all_events"`, `"mode":"any"`, 1),
	} {
		if _, err := DecodePreferences([]byte(body)); err == nil {
			t.Fatalf("accepted %s", body)
		}
	}
}
func TestDependencyAwareDefault(t *testing.T) {
	for _, deps := range [][2]bool{{false, false}, {true, false}, {false, true}, {true, true}} {
		for _, raw := range []string{"", "false", "true"} {
			t.Setenv("LIVE_ACTIVITY_ENABLED", raw)
			c, err := ConfigFromEnv(deps[0], deps[1])
			want := raw == "true" || (raw == "" && deps[0] && deps[1])
			if raw == "true" && (!deps[0] || !deps[1]) {
				if err == nil {
					t.Fatal("enabled without dependencies", deps)
				}
				continue
			}
			if err != nil || c.Enabled != want {
				t.Fatal(deps, raw, c, err)
			}
		}
	}
	t.Setenv("LIVE_ACTIVITY_ENABLED", "invalid")
	if _, err := ConfigFromEnv(true, true); err == nil {
		t.Fatal("accepted invalid switch")
	}
}

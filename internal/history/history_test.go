package history

import (
	"reflect"
	"testing"
	"time"

	"github.com/sonastea/ticketopia/internal/models"
)

func TestMeaningfulChanges(t *testing.T) {
	instant := time.Date(2026, 10, 12, 19, 0, 0, 0, time.UTC)
	date, clock, zone := "2026-10-12", "19:00:00", "UTC"
	base := func() models.Event {
		return models.Event{Name: "Original", Status: "onsale", Start: models.EventStart{DateTime: &instant, LocalDate: &date, LocalTime: &clock, Timezone: &zone}, PublicSale: models.PublicSale{Start: &instant}, Venues: []models.Venue{{ID: "ticketmaster:A"}, {ID: "ticketmaster:B"}}}
	}
	for _, tc := range []struct {
		name string
		edit func(*models.Event)
		want []string
	}{
		{"same", func(*models.Event) {}, nil},
		{"presentation", func(e *models.Event) { e.Name = "New title"; e.Images = []models.Image{{URL: "new"}} }, nil},
		{"equivalent instant", func(e *models.Event) { t := instant.In(time.FixedZone("local", 3600)); e.Start.DateTime = &t }, nil},
		{"date", func(e *models.Event) { d := "2026-10-13"; e.Start.LocalDate = &d }, []string{"date"}},
		{"unknown time explicit", func(e *models.Event) { e.Start.DateTime = nil; e.Start.TimeTBA = true }, []string{"date"}},
		{"venue order", func(e *models.Event) { e.Venues[0], e.Venues[1] = e.Venues[1], e.Venues[0] }, nil},
		{"venue identity", func(e *models.Event) { e.Venues[0].ID = "ticketmaster:a" }, []string{"venue"}},
		{"venue rename", func(e *models.Event) { e.Venues[0].Name = "Renamed" }, nil},
		{"sale", func(e *models.Event) { t := instant.Add(time.Hour); e.PublicSale.Start = &t }, []string{"sale_time"}},
		{"sale TBD", func(e *models.Event) { e.PublicSale = models.PublicSale{StartTBD: true} }, []string{"sale_time"}},
		{"cancelled", func(e *models.Event) { e.Status = "cancelled" }, []string{"cancellation"}},
		{"canceled", func(e *models.Event) { e.Status = "canceled" }, []string{"cancellation"}},
		{"postponed", func(e *models.Event) { e.Status = "postponed" }, []string{"postponement"}},
		{"unknown provider status", func(e *models.Event) { e.Status = "unknown" }, nil},
		{"partial date", func(e *models.Event) { e.Start = models.EventStart{LocalDate: &date} }, nil},
		{"sparse", func(e *models.Event) {
			e.Status = ""
			e.Venues = nil
			e.PublicSale = models.PublicSale{}
			e.Start = models.EventStart{}
		}, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			before, after := base(), base()
			tc.edit(&after)
			var got []string
			for _, change := range Changes(before, after) {
				got = append(got, change.Kind)
				if len(change.Before) == 0 || len(change.After) == 0 {
					t.Fatal("change missing evidence")
				}
			}
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("got %v, want %v", got, tc.want)
			}
		})
	}
	before, after := base(), base()
	before.Status, after.Status = "cancelled", "canceled"
	if len(Changes(before, after)) != 0 {
		t.Fatal("cancellation spelling produced duplicate change")
	}
	baseline := KnownFacts(base(), models.Event{})
	if len(Changes(baseline, base())) != 0 {
		t.Fatal("returning known facts produced false changes")
	}
	before, after = base(), base()
	before.Presales = []models.Presale{{Name: "Fan sale", Start: &instant}}
	after.Presales = []models.Presale{{Name: "Renamed", Start: &instant, Description: "Updated copy"}}
	if len(Changes(before, after)) != 0 {
		t.Fatal("presale presentation generated a timing change")
	}
	next := instant.Add(time.Hour)
	after.Presales[0].Start = &next
	if changes := Changes(before, after); len(changes) != 1 || changes[0].Kind != "sale_time" {
		t.Fatal("presale timing change not detected")
	}
}

func TestHistoryConfiguration(t *testing.T) {
	t.Setenv("EVENT_HISTORY_ENABLED", "true")
	t.Setenv("TICKETMASTER_KEY", "fixture")
	t.Setenv("EVENT_HISTORY_CITIES", `[{"city":" Chicago ","country":"us"},{"city":"chicago","country":"US"}]`)
	t.Setenv("EVENT_HISTORY_DAYS", "2")
	t.Setenv("EVENT_HISTORY_INTERVAL", "12h")
	c, err := ConfigFromEnv()
	if err != nil || len(c.Cities) != 1 || c.Cities[0].City != "Chicago" || c.Cities[0].Country != "US" || c.Days != 2 || c.Interval != 12*time.Hour {
		t.Fatalf("invalid normalization: %+v %v", c, err)
	}
	for _, setting := range []struct{ key, value string }{
		{"EVENT_HISTORY_CITIES", `[{"city":"","country":"US"}]`},
		{"EVENT_HISTORY_CITIES", `[{"city":"Boston","country":"USA"}]`},
		{"EVENT_HISTORY_DAYS", "367"},
		{"EVENT_HISTORY_INTERVAL", "1m"},
		{"TICKETMASTER_KEY", ""},
	} {
		t.Run(setting.key+setting.value, func(t *testing.T) {
			t.Setenv(setting.key, setting.value)
			if _, err := ConfigFromEnv(); err == nil {
				t.Fatal("invalid configuration accepted")
			}
		})
	}
	t.Setenv("EVENT_HISTORY_ENABLED", "false")
	t.Setenv("EVENT_HISTORY_CITIES", "invalid unused configuration")
	if c, err := ConfigFromEnv(); err != nil || c.Enabled {
		t.Fatal("disabled history requires no provider/database configuration")
	}
}

package discovery

import (
	"net/url"
	"testing"
	"time"
)

func TestDatePresets(t *testing.T) {
	// Monday 2026-10-19 through Sunday 2026-10-25.
	weekend := map[time.Weekday][2]string{
		time.Monday:    {"2026-10-23", "2026-10-25"},
		time.Tuesday:   {"2026-10-23", "2026-10-25"},
		time.Wednesday: {"2026-10-23", "2026-10-25"},
		time.Thursday:  {"2026-10-23", "2026-10-25"},
		time.Friday:    {"2026-10-23", "2026-10-25"},
		time.Saturday:  {"2026-10-24", "2026-10-25"},
		time.Sunday:    {"2026-10-25", "2026-10-25"},
	}
	for d := 19; d <= 25; d++ {
		// Late evening in Los Angeles is already tomorrow in UTC; presets use the
		// supplied local calendar day.
		zone := time.FixedZone("PDT", -7*3600)
		today := time.Date(2026, 10, d, 23, 30, 0, 0, zone)
		day := today.Format(time.DateOnly)
		got := DatePresets(today)
		if len(got) != 3 || got[0].ID != "tonight" || got[1].ID != "weekend" || got[2].ID != "week" {
			t.Fatalf("presets = %+v", got)
		}
		if got[0].StartDate != day || got[0].EndDate != day {
			t.Errorf("%s tonight = %s..%s", day, got[0].StartDate, got[0].EndDate)
		}
		if want := weekend[today.Weekday()]; got[1].StartDate != want[0] || got[1].EndDate != want[1] {
			t.Errorf("%s (%s) weekend = %s..%s, want %s..%s", day, today.Weekday(), got[1].StartDate, got[1].EndDate, want[0], want[1])
		}
		if got[2].StartDate != day || got[2].EndDate != today.AddDate(0, 0, 6).Format(time.DateOnly) {
			t.Errorf("%s next 7 days = %s..%s", day, got[2].StartDate, got[2].EndDate)
		}
	}
	// Month and year boundaries.
	if got := DatePresets(time.Date(2026, 12, 29, 12, 0, 0, 0, time.UTC)); got[1].StartDate != "2027-01-01" || got[1].EndDate != "2027-01-03" || got[2].EndDate != "2027-01-04" {
		t.Errorf("year-end presets = %+v", got)
	}
}

// Chip links are built from the current query's Values; each must parse back
// through ParseQuery with its dates and every other filter intact.
func TestDatePresetURLsParse(t *testing.T) {
	now := time.Date(2026, 10, 21, 18, 0, 0, 0, time.UTC)
	base, err := ParseQuery(url.Values{
		"city": {"Los Angeles"}, "country": {"us"}, "keyword": {"jazz"},
		"category_id": {musicSegment}, "genre_id": {"KnvZfZ7vAvE"},
		"venue_id": {"ticketmaster:KovZpZA7AAEA"}, "sort": {"name_asc"}, "limit": {"40"},
		"start_date": {"2026-11-01"}, "end_date": {"2026-12-01"},
	}, now)
	if err != nil {
		t.Fatal(err)
	}
	paged := base
	paged.Page, paged.Cursor = 3, &Cursor{Date: "2026-11-02", ID: "ticketmaster:A"}
	for _, p := range DatePresets(now) {
		link := "/?" + paged.WithDates(p.StartDate, p.EndDate).Values().Encode()
		u, err := url.Parse(link)
		if err != nil {
			t.Fatal(err)
		}
		if u.Query().Has("cursor") {
			t.Errorf("%s link kept a cursor: %s", p.ID, link)
		}
		got, err := ParseQuery(u.Query(), now)
		if err != nil {
			t.Fatalf("%s link %s: %v", p.ID, link, err)
		}
		want := base
		want.StartDate, want.EndDate = p.StartDate, p.EndDate
		if got != want {
			t.Errorf("%s parsed %+v, want %+v", p.ID, got, want)
		}
	}
}

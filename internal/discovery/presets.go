package discovery

import "time"

// DatePreset is a named Discover date range. Presets are ordinary
// start_date/end_date values, so chip links need no new query parameter and
// keep working as plain URLs without JavaScript.
type DatePreset struct {
	ID, Label, StartDate, EndDate string
}

// DatePresets returns Tonight, This weekend and Next 7 days for the calendar
// day of today (its time of day and zone are ignored after taking the date).
// The weekend is Friday through Sunday; from Friday it starts today.
func DatePresets(today time.Time) []DatePreset {
	day := time.Date(today.Year(), today.Month(), today.Day(), 0, 0, 0, 0, time.UTC)
	weekend := day
	if wd := day.Weekday(); wd >= time.Monday && wd <= time.Thursday {
		weekend = day.AddDate(0, 0, int(time.Friday-wd))
	}
	sunday := weekend.AddDate(0, 0, (7-int(weekend.Weekday()))%7)
	date := func(t time.Time) string { return t.Format(time.DateOnly) }
	return []DatePreset{
		{ID: "tonight", Label: "Tonight", StartDate: date(day), EndDate: date(day)},
		{ID: "weekend", Label: "This weekend", StartDate: date(weekend), EndDate: date(sunday)},
		{ID: "week", Label: "Next 7 days", StartDate: date(day), EndDate: date(day.AddDate(0, 0, 6))},
	}
}

// WithDates keeps every other filter and restarts from the first page.
func (q Query) WithDates(start, end string) Query {
	q.StartDate, q.EndDate = start, end
	q.Page, q.Stored, q.Cursor = 0, false, nil
	return q
}

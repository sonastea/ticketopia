package discovery

import (
	"testing"
	"time"
)

func TestDatePresets(t *testing.T) {
	for _, tc := range []struct {
		name, day, fridayOrToday, sunday, weekEnd string
	}{
		{"Monday", "2026-10-19", "2026-10-23", "2026-10-25", "2026-10-25"},
		{"Thursday", "2026-10-22", "2026-10-23", "2026-10-25", "2026-10-28"},
		{"Friday", "2026-10-23", "2026-10-23", "2026-10-25", "2026-10-29"},
		{"Saturday", "2026-10-24", "2026-10-24", "2026-10-25", "2026-10-30"},
		{"Sunday", "2026-10-25", "2026-10-25", "2026-10-25", "2026-10-31"},
		{"year rollover", "2026-12-29", "2027-01-01", "2027-01-03", "2027-01-04"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			// Late evening locally is already tomorrow in UTC.
			at, err := time.Parse(time.RFC3339, tc.day+"T23:30:00-07:00")
			if err != nil {
				t.Fatal(err)
			}
			want := [][3]string{
				{"tonight", tc.day, tc.day},
				{"weekend", tc.fridayOrToday, tc.sunday},
				{"week", tc.day, tc.weekEnd},
			}
			got := DatePresets(at)
			if len(got) != len(want) {
				t.Fatalf("presets = %+v", got)
			}
			for i, p := range got {
				if [3]string{p.ID, p.StartDate, p.EndDate} != want[i] {
					t.Errorf("preset %d = %+v, want ID and date range %q", i, p, want[i])
				}
			}
		})
	}
}

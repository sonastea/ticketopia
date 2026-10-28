package history

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/sonastea/ticketopia/internal/models"
)

// Presale names are labels, not unique occurrence IDs. Missing end times must
// not be borrowed from a different same-named window just because it came first.
func TestKnownFactsSparsePresalesMatchKnownStart(t *testing.T) {
	start1 := time.Date(2026, 10, 28, 9, 0, 0, 0, time.UTC)
	end1 := start1.Add(time.Hour)
	start2, end2 := start1.Add(24*time.Hour), end1.Add(24*time.Hour)
	first := models.Presale{Name: "Fan Club Presale", Start: &start1, End: &end1}
	second := models.Presale{Name: "Fan Club Presale", Start: &start2, End: &end2}
	after := models.Event{Presales: []models.Presale{
		{Name: first.Name, Start: &start1},
		{Name: second.Name, Start: &start2},
	}}
	for _, tc := range []struct {
		name string
		old  []models.Presale
	}{
		{"earlier_first", []models.Presale{first, second}},
		{"later_first", []models.Presale{second, first}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			before := models.Event{Presales: tc.old}
			merged := KnownFacts(before, after)
			changes := Changes(before, after)
			if merged.Presales[0].End == nil || merged.Presales[1].End == nil || !merged.Presales[0].End.Equal(end1) || !merged.Presales[1].End.Equal(end2) || len(changes) != 0 {
				data, _ := json.Marshal(merged.Presales)
				t.Fatalf("sparse unchanged presales matched by first name instead of known start: merged=%s, changes=%+v; want each original end and no changes", data, changes)
			}
		})
	}
}

// A matching wall clock in a different zone is not the same instant. The
// comparison baseline must not fabricate that combination from two observations.
func TestKnownFactsChangedTimezoneDoesNotRetainOldInstant(t *testing.T) {
	date, clock := "2026-10-28", "15:00:00"
	oldZone, newZone := "America/New_York", "America/Chicago"
	oldInstant := time.Date(2026, 10, 28, 19, 0, 0, 0, time.UTC)
	before := models.Event{Start: models.EventStart{DateTime: &oldInstant, LocalDate: &date, LocalTime: &clock, Timezone: &oldZone}}
	after := models.Event{Start: models.EventStart{LocalDate: &date, LocalTime: &clock, Timezone: &newZone}}
	merged := KnownFacts(before, after)
	if merged.Start.DateTime != nil && merged.Start.DateTime.Equal(oldInstant) {
		data, _ := json.Marshal(merged.Start)
		t.Fatalf("changed timezone inherited incompatible UTC instant: %s; 15:00 Chicago is 20:00Z, not 19:00Z", data)
	}
}

// A response can retain a known local time even when its date/UTC instant is
// missing. Such a response is sparse, but its supplied time is not unknown.
func TestKnownFactsTimeOnlyObservationRetainsSuppliedTime(t *testing.T) {
	date, oldClock, newClock, zone := "2026-10-28", "15:00:00", "16:00:00", "America/Chicago"
	oldInstant := time.Date(2026, 10, 28, 20, 0, 0, 0, time.UTC)
	before := models.Event{Start: models.EventStart{DateTime: &oldInstant, LocalDate: &date, LocalTime: &oldClock, Timezone: &zone}}
	after := models.Event{Start: models.EventStart{LocalTime: &newClock, Timezone: &zone}}
	merged := KnownFacts(before, after)
	changes := Changes(before, after)
	if merged.Start.LocalTime == nil || *merged.Start.LocalTime != newClock || len(changes) != 1 || changes[0].Kind != "date" {
		data, _ := json.Marshal(merged.Start)
		t.Fatalf("explicit local-time change discarded with missing date: merged=%s, changes=%+v; want supplied 16:00:00 and one date change", data, changes)
	}
}

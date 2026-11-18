package account

import (
	"testing"
	"time"

	"github.com/sonastea/ticketopia/internal/interests"
	"github.com/sonastea/ticketopia/internal/models"
	"github.com/sonastea/ticketopia/internal/recommendations"
)

func TestRecentActivityRanksEventsByLatestPublicAction(t *testing.T) {
	at := func(day int) time.Time { return time.Date(2026, 11, day, 0, 0, 0, 0, time.UTC) }
	event := func(id string) models.Event { return models.Event{ID: id} }
	got := RecentActivity(
		[]interests.Item{{Event: event("both"), InterestedAt: at(9)}, {Event: event("interest"), InterestedAt: at(5)}, {Event: event("old"), InterestedAt: at(1)}},
		[]recommendations.Item{{Event: event("both"), RecommendedAt: at(2)}, {Event: event("recommended"), RecommendedAt: at(7)}},
	)
	want := []struct {
		id          string
		recommended bool
	}{{"both", true}, {"recommended", true}, {"interest", false}}
	if len(got) != len(want) {
		t.Fatalf("got %d items, want %d", len(got), len(want))
	}
	for i, w := range want {
		// A newer interest lifts an older recommendation of the same event,
		// which appears once and keeps its recommended label.
		if got[i].Event.ID != w.id || got[i].Recommended != w.recommended {
			t.Fatalf("item %d = %s/%v, want %s/%v", i, got[i].Event.ID, got[i].Recommended, w.id, w.recommended)
		}
	}
}

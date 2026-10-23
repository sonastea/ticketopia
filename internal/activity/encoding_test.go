package activity

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"
)

// richSnapshot mirrors a full public view: 20 groups with long multilingual
// titles, as in the rich deployment fixture.
func richSnapshot() Snapshot {
	now := time.Now().UTC()
	name := "Ada"
	s := Snapshot{AsOf: now}
	for i := range 20 {
		s.Items = append(s.Items, Item{EventID: fmt.Sprintf("ticketmaster:LiveCap_%d", i), EventTitle: strings.Repeat("Ünïcödé Fëstïväl 東京 ", 12), City: "São Paulo", ActorFirstName: &name, RecentRecommendations: 2, LatestRecommendedAt: now, ExpiresAt: now.Add(time.Hour)})
	}
	return s
}

// The spliced view encoding must stay byte-identical to the marshalled one,
// including HTML escaping and non-ASCII text, so content hashes and clients see
// no change.
func TestSnapshotEncodingMatchesMarshal(t *testing.T) {
	s := richSnapshot()
	s.Items[0].EventTitle = `<Tour> & "Friends" ` + " "
	s.Items[1].ActorFirstName = nil
	for _, items := range [][]Item{s.Items, {}} {
		view := Snapshot{AsOf: s.AsOf, Items: items}
		view.encode()
		view.AsOf = view.AsOf.Add(time.Second)
		view.encode()
		want, err := json.Marshal(struct {
			AsOf  time.Time `json:"as_of"`
			Items []Item    `json:"items"`
		}{view.AsOf, items})
		if err != nil {
			t.Fatal(err)
		}
		got, err := json.Marshal(view)
		if err != nil || string(got) != string(want) {
			t.Fatalf("encoded %s, want %s (%v)", got, want, err)
		}
		if direct, _ := view.MarshalJSON(); string(direct) != string(want) {
			t.Fatalf("frame payload %s, want %s", direct, want)
		}
	}
}

// Each two-second generation refreshes AsOf on every warm cohort view.
func BenchmarkViewGenerationEncode(b *testing.B) {
	s := richSnapshot()
	s.encode()
	b.ReportAllocs()
	for b.Loop() {
		s.AsOf = s.AsOf.Add(time.Microsecond)
		s.encode()
	}
}

// Each stream writes the shared view and its own counts as frame payloads.
func BenchmarkStreamPayloads(b *testing.B) {
	s := richSnapshot()
	s.encode()
	c := Counts{AsOf: s.AsOf}
	for i := range 101 {
		c.Items = append(c.Items, Count{EventID: fmt.Sprintf("ticketmaster:LiveCap_%d", i), RecommendationCount: i, InterestedCount: 2 * i})
	}
	b.Run("snapshot", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			if _, err := s.MarshalJSON(); err != nil {
				b.Fatal(err)
			}
		}
	})
	b.Run("counts", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			if _, err := json.Marshal(&c); err != nil {
				b.Fatal(err)
			}
		}
	})
}

package activity

import (
	"fmt"
	"runtime"
	"strings"
	"testing"
	"time"
)

// calibrationWindow builds a generation as the repository does: fresh strings
// per scanned row, shared cities/facets through the Interner, no preallocation.
func calibrationWindow(n, maxArtists int, longEvery int) Candidates {
	in := NewInterner(Candidates{})
	data := Candidates{AsOf: time.Now()}
	for i := range n {
		title := fmt.Sprintf("Capacity fixture %d", i)
		if longEvery > 0 && i%longEvery == 0 {
			title = strings.Repeat("A long multilingual event title — 音楽 · Ñandú · Привет · ", 6) + fmt.Sprint(i)
		}
		name := strings.Clone("Alexandra")
		c := Candidate{Item: Item{EventID: fmt.Sprintf("ticketmaster:LiveCap_%d", i), EventTitle: title,
			City: strings.Clone([]string{"London", "São Paulo", "Kraków", "Mexico City"}[i%4]), ActorFirstName: &name,
			RecentRecommendations: 1, LatestRecommendedAt: time.Now(), ExpiresAt: time.Now().Add(time.Hour)},
			CityKey: fmt.Sprintf("0FEA0E8B0F82101F%04X:0F210FC0", i%16), Revision: time.Now()}
		c.CategoryIDs = append(c.CategoryIDs, strings.Clone([]string{"Music", "Sports", "Arts", "Film"}[i%4]))
		for k := range 1 + i%maxArtists {
			c.ArtistIDs = append(c.ArtistIDs, fmt.Sprintf("ticketmaster:CapArtist_%d", (i*31+k*97)%512))
		}
		c.VenueIDs = append(c.VenueIDs, fmt.Sprintf("ticketmaster:CapVenue_%d", (i*7)%256))
		if i%3 == 0 {
			c.VenueIDs = append(c.VenueIDs, fmt.Sprintf("ticketmaster:CapVenue_%d", (i*11+5)%256))
		}
		in.Intern(&c)
		data.Items = append(data.Items, c)
	}
	return data
}

// Accounting must never be below the retained heap of a generation and its
// index, or the memory bound is fiction; nor several times above it, or windows
// fail closed long before memory is actually at risk.
func TestAccountingCoversRetainedHeap(t *testing.T) {
	for _, profile := range []struct {
		name             string
		n, artists, long int
	}{{"rich 10000", 10000, 12, 4}, {"typical 10000", 10000, 3, 16}, {"one artist 10000", 10000, 1, 0}, {"rich 1000", 1000, 12, 4}} {
		t.Run(profile.name, func(t *testing.T) {
			var before, after runtime.MemStats
			runtime.GC()
			runtime.ReadMemStats(&before)
			x, err := buildIndex(calibrationWindow(profile.n, profile.artists, profile.long), nil)
			if err != nil {
				t.Fatal(err)
			}
			runtime.GC()
			runtime.ReadMemStats(&after)
			measured := int(after.HeapAlloc) - int(before.HeapAlloc)
			runtime.KeepAlive(x)
			t.Logf("accounted %.2f MiB, retained %.2f MiB (%.2fx)", float64(x.bytes)/(1<<20), float64(measured)/(1<<20), float64(x.bytes)/float64(measured))
			if x.bytes < measured || x.bytes > 3*measured {
				t.Fatalf("accounting %d is not calibrated to retained heap %d", x.bytes, measured)
			}
		})
	}
}

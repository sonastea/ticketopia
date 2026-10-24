package persistence

import (
	"encoding/json"
	"fmt"
	"testing"

	"github.com/sonastea/ticketopia/internal/discovery"
	"github.com/sonastea/ticketopia/internal/models"
)

// Happening Now enriches up to 20 live items, and StoredEvents caps any
// caller at discovery.MaxSnapshotBatch. Snapshots carry Ticketmaster-sized
// image sets so decode cost reflects what the page actually reads.
func BenchmarkMariaDBSnapshots(b *testing.B) {
	f := newMaria(b)
	f.migrate(b)
	p := f.open(b)
	tx, err := p.db.BeginTx(b.Context(), nil)
	if err != nil {
		b.Fatal(err)
	}
	ids := make([]string, discovery.MaxSnapshotBatch)
	for i := range ids {
		e := benchmarkEvent(i)
		for w := range 10 {
			e.Images = append(e.Images, models.Image{URL: fmt.Sprintf("https://s1.ticketm.net/dam/a/%03d/%06d_RETINA_PORTRAIT_%d.jpg", w, i, w), Width: 640 + 64*w, Height: 360 + 36*w, Ratio: "16_9"})
		}
		low, high := "39.50", "129.00"
		e.PriceRanges = []models.PriceRange{{Type: "standard", Currency: "EUR", Min: &low, Max: &high}}
		d := models.EventDetail{Item: e, Meta: models.Freshness{DataAsOf: benchmarkAt}}
		args, data, err := prepareSnapshot(d)
		if err == nil {
			err = upsertSnapshotTx(b.Context(), tx, d, args, data)
		}
		if err != nil {
			_ = tx.Rollback()
			b.Fatal(err)
		}
		ids[i] = e.ID
	}
	if err := tx.Commit(); err != nil {
		b.Fatal(err)
	}
	r := p.Discovery()
	for _, n := range []int{20, discovery.MaxSnapshotBatch} {
		b.Run(fmt.Sprintf("read/%d", n), func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				events, err := r.Snapshots(b.Context(), ids[:n])
				if err != nil || len(events) != n {
					b.Fatal("snapshots", len(events), err)
				}
			}
		})
		// Decode alone, from the same stored bytes, separates JSON cost from
		// the round trip and row scanning.
		var payloads [][]byte
		var size int
		for _, id := range ids[:n] {
			var data []byte
			if err := p.db.QueryRowContext(b.Context(), `SELECT snapshot FROM event_snapshots WHERE event_id=?`, id).Scan(&data); err != nil {
				b.Fatal(err)
			}
			payloads, size = append(payloads, data), size+len(data)
		}
		b.Run(fmt.Sprintf("decode/%d", n), func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				for _, data := range payloads {
					var event models.Event
					if err := json.Unmarshal(data, &event); err != nil {
						b.Fatal(err)
					}
				}
			}
			b.ReportMetric(float64(size)/float64(n), "snapshot-B")
		})
	}
}

package persistence

import (
	"fmt"
	"testing"
	"time"

	"github.com/sonastea/ticketopia/internal/radar"
)

// Repository-level reads pin both the horizon and cursor boundary.
func BenchmarkMariaDBRadar(b *testing.B) {
	for _, volume := range benchmarkVolumes(b) {
		b.Run(fmt.Sprint(volume), func(b *testing.B) {
			f := newQueryFixture(b, volume)
			r := f.p.Radar()
			q := radar.Query{Limit: 20}
			for range volume / 100 {
				d, err := r.readAt(b.Context(), benchmarkOwner(0), benchmarkAt, 6*time.Hour, q, benchmarkAt)
				if err != nil || len(d.Candidates) < 20 {
					b.Fatal("cursor setup", err)
				}
				q.Cursor = &radar.Cursor{At: benchmarkAt, After: d.Candidates[19].Event.ID, Digest: d.Revision}
			}
			for _, tc := range []struct {
				name string
				q    radar.Query
			}{{"first", radar.Query{Limit: 20}}, {"later", q}} {
				b.Run(tc.name, func(b *testing.B) {
					b.ReportAllocs()
					for b.Loop() {
						d, err := r.readAt(b.Context(), benchmarkOwner(0), benchmarkAt, 6*time.Hour, tc.q, benchmarkAt)
						if err != nil || len(d.Candidates) < 20 {
							b.Fatal("radar", err)
						}
					}
				})
			}
		})
	}
}

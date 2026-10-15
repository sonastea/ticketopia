package persistence

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"github.com/sonastea/ticketopia/internal/api"
	"github.com/sonastea/ticketopia/internal/discovery"
	"github.com/sonastea/ticketopia/internal/kv"
)

// Count real repository reads at the service boundary; these are not counts of
// individual SQL statements. Use the same pinned coverage clock as query plans.
type publicBenchmarkCatalog struct {
	discovery.Catalog
	repository *DiscoveryRepository
	reads      int
}

func (c *publicBenchmarkCatalog) Search(ctx context.Context, q discovery.Query, freshFor time.Duration) (discovery.CatalogRead, error) {
	c.reads++
	return c.repository.searchAt(ctx, q, freshFor, benchmarkAt)
}

func BenchmarkMariaDBPublicHTTP(b *testing.B) {
	for _, volume := range benchmarkVolumes(b) {
		b.Run(fmt.Sprint(volume), func(b *testing.B) {
			f := newQueryFixture(b, volume)
			cache := kv.NewMemory()
			b.Cleanup(func() { cache.Close() })
			catalog := &publicBenchmarkCatalog{Catalog: f.p.Discovery(), repository: f.p.Discovery()}
			service := discovery.New(b.Context(), cache, zerolog.Nop(), discovery.Config{Catalog: catalog})
			b.Cleanup(service.WaitRefreshes)
			a, err := api.NewAPI(b.Context(), zerolog.Nop(), cache, api.WithDiscovery(service))
			if err != nil {
				b.Fatal(err)
			}
			routes := a.Routes()
			path := "/api/v1/events?" + benchmarkDiscoverQuery().Values().Encode()
			initial := httptest.NewRecorder()
			routes.ServeHTTP(initial, httptest.NewRequest("GET", path, nil))
			if initial.Code != 200 || initial.Header().Get("ETag") == "" {
				b.Fatalf("initial response: %d %s", initial.Code, initial.Body)
			}
			for _, conditional := range []bool{false, true} {
				status := http.StatusOK
				if conditional {
					status = http.StatusNotModified
				}
				b.Run(fmt.Sprint(status), func(b *testing.B) {
					req := httptest.NewRequest("GET", path, nil)
					if conditional {
						req.Header.Set("If-None-Match", initial.Header().Get("ETag"))
					}
					catalog.reads = 0
					var bodyBytes, responseBytes int64
					b.ReportAllocs()
					for b.Loop() {
						w := httptest.NewRecorder()
						routes.ServeHTTP(w, req)
						if w.Code != status {
							b.Fatalf("status %d: %s", w.Code, w.Body)
						}
						bodyBytes += int64(w.Body.Len())
						// Application response bytes, excluding transport framing and
						// net/http's automatic Date header. Same accounting for both.
						responseBytes += int64(w.Body.Len() + len(fmt.Sprintf("HTTP/1.1 %d %s\r\n", status, http.StatusText(status))) + 2)
						for key, values := range w.Header() {
							for _, value := range values {
								responseBytes += int64(len(key) + len(value) + 4)
							}
						}
					}
					b.ReportMetric(float64(bodyBytes)/float64(b.N), "body-B/op")
					b.ReportMetric(float64(responseBytes)/float64(b.N), "response-B/op")
					b.ReportMetric(float64(catalog.reads)/float64(b.N), "repository-reads/op")
				})
			}
		})
	}
}

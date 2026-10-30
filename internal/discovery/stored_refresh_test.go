package discovery

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"github.com/rs/zerolog"
	"github.com/sonastea/ticketopia/internal/kv"
	"github.com/sonastea/ticketopia/internal/models"
)

type observationCatalog struct {
	Catalog
	stored                models.EventDetail
	storedErr, observeErr error
	observes, histories   atomic.Int32
}

func (c *observationCatalog) Snapshot(context.Context, string) (models.EventDetail, error) {
	return c.stored, c.storedErr
}

func (c *observationCatalog) Observe(ctx context.Context, _ models.EventDetail) error {
	c.observes.Add(1)
	if err := storedTestDelay(ctx, 10*time.Millisecond); err != nil {
		return err
	}
	return c.observeErr
}

func (c *observationCatalog) History(context.Context, string, time.Time, int) (*models.EventHistory, error) {
	c.histories.Add(1)
	return &models.EventHistory{}, nil
}

// Provider singleflight must cover the observation as well: otherwise every
// cold/expired reader independently enters the same metadata write transaction.
// Sharing must propagate write failures, not automatically replay a possibly
// committed write. Fresh stored details must still avoid refresh work entirely.
func TestStoredEventSharesRefreshObservation(t *testing.T) {
	writeErr := errors.New("observation failed")
	for _, tc := range []struct {
		name                 string
		missing, fresh       bool
		observeErr           error
		wantProvider, wantDB int32
	}{{"cold", true, false, nil, 1, 1}, {"expired", false, false, nil, 1, 1}, {"fresh", false, true, nil, 0, 0}, {"write_failure", true, false, writeErr, 1, 1}} {
		t.Run(tc.name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				const readers = 16
				const id = "ticketmaster:shared-refresh"
				at := time.Now()
				if !tc.fresh {
					at = at.Add(-24 * time.Hour)
				}
				catalog := &observationCatalog{stored: models.EventDetail{Item: models.Event{ID: id}, Meta: models.Freshness{DataAsOf: at}}, observeErr: tc.observeErr}
				if tc.missing {
					catalog.storedErr = ErrNotFound
				}
				var provider atomic.Int32
				client := &http.Client{Transport: storedTestTransport(func(r *http.Request) (*http.Response, error) {
					provider.Add(1)
					if err := storedTestDelay(r.Context(), 10*time.Millisecond); err != nil {
						return nil, err
					}
					return &http.Response{StatusCode: http.StatusOK, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(`{"id":"shared-refresh","name":"Show"}`)), Request: r}, nil
				})}
				cache := kv.NewMemory()
				defer cache.Close()
				s := New(t.Context(), cache, zerolog.Nop(), Config{APIKey: "fixture", Catalog: catalog, HTTPClient: client})
				var wg sync.WaitGroup
				for range readers {
					wg.Go(func() {
						detail, err := s.Event(t.Context(), id)
						if !errors.Is(err, tc.observeErr) || (err == nil && detail.Item.ID != id) {
							t.Errorf("shared read: id=%q err=%v want=%v", detail.Item.ID, err, tc.observeErr)
						}
					})
				}
				wg.Wait()
				t.Logf("readers=%d provider=%d observations=%d histories=%d", readers, provider.Load(), catalog.observes.Load(), catalog.histories.Load())
				if provider.Load() != tc.wantProvider || catalog.observes.Load() != tc.wantDB {
					t.Errorf("provider/observation calls = %d/%d, want %d/%d", provider.Load(), catalog.observes.Load(), tc.wantProvider, tc.wantDB)
				}
			})
		})
	}
}

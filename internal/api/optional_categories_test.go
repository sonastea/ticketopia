package api

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"github.com/sonastea/ticketopia/internal/activity"
	"github.com/sonastea/ticketopia/internal/discovery"
	"github.com/sonastea/ticketopia/internal/kv"
)

// Happening Now is a durable read with an optional category selector, as
// Discover is. A category provider that has not answered must not hold the page
// until the shared ten-second refresh finishes; optionalCategories bounds the
// wait to one second.
func TestHappeningNowBoundsOptionalCategoryWait(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	started, release := make(chan struct{}), make(chan struct{})
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/classifications.json" {
			http.NotFound(w, r)
			return
		}
		close(started)
		select {
		case <-release:
			_, _ = io.WriteString(w, `{"_embedded":{"classifications":[]}}`)
		case <-r.Context().Done():
		}
	}))
	defer upstream.Close()
	defer close(release)
	cache := kv.NewMemory()
	defer cache.Close()
	provider := discovery.New(ctx, cache, zerolog.Nop(), discovery.Config{APIKey: "test", BaseURL: upstream.URL})
	defer provider.WaitRefreshes()
	// Cancel shared refreshes before waiting for them during cleanup.
	defer cancel()
	live := activity.New(ctx, &liveRepository{}, 2)
	defer live.Close()
	a := &api{events: provider, liveActivity: live, shutdown: ctx.Done()}
	routes := a.Routes()
	response := httptest.NewRecorder()
	done := make(chan struct{})
	go func() {
		defer close(done)
		routes.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/happening-now", nil))
	}()
	select {
	case <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("taxonomy request did not start")
	}
	select {
	case <-done:
		if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), "Alex recommended") || response.Header().Get("Cache-Control") != "private, no-store" {
			t.Fatalf("optional taxonomy failure changed durable content: status=%d headers=%v", response.Code, response.Header())
		}
	case <-time.After(2 * time.Second):
		// Release then join before accessing the recorder or closing services.
		cancel()
		<-done
		t.Fatal("optional taxonomy held Happening Now beyond Discover's one-second budget (still blocked at two seconds)")
	}
}

package api

import (
	"bufio"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"github.com/sonastea/ticketopia/internal/activity"
	"github.com/sonastea/ticketopia/internal/kv"
)

type liveRepository struct {
	polls atomic.Int32
	fail  atomic.Bool
}

func (r *liveRepository) Snapshot(context.Context) (activity.Snapshot, error) {
	r.polls.Add(1)
	if r.fail.Load() {
		return activity.Snapshot{}, activity.ErrUnavailable
	}
	name := "Alex"
	now := time.Now().UTC()
	return activity.Snapshot{AsOf: now, Items: []activity.Item{{EventID: "ticketmaster:test", EventTitle: "A concert", City: "London", ActorFirstName: &name, RecentRecommendations: 1, LatestRecommendedAt: now.Truncate(time.Minute), ExpiresAt: now.Add(10 * time.Minute)}}}, nil
}
func (r *liveRepository) Counts(_ context.Context, ids []string) (activity.Counts, error) {
	c := activity.Counts{AsOf: time.Now().UTC(), Items: []activity.Count{}}
	for _, id := range ids {
		c.Items = append(c.Items, activity.Count{EventID: id, InterestedCount: 7})
	}
	return c, nil
}
func (*liveRepository) Cleanup(context.Context) error { return nil }

func TestLivePublicRoutesAndStream(t *testing.T) {
	t.Setenv("IP_GEOLOCATION_ENABLED", "false")
	repo := &liveRepository{}
	service := activity.New(t.Context(), repo, 2)
	defer service.Close()
	app, err := NewAPI(t.Context(), zerolog.Nop(), kv.NewMemory(), WithLiveActivity(service, 60))
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(app.Routes())
	defer server.Close()
	for _, test := range []struct {
		path   string
		status int
	}{{"/api/v1/community/activity", 200}, {"/api/v1/community/activity?filter=" + url.QueryEscape(`{"category_ids":["Music"]}`), 200}, {"/api/v1/community/activity?filter=" + url.QueryEscape(`{"connections":{"mode":"any","sources":["saved"]}}`), 400}, {"/api/v1/community/activity?filter=" + url.QueryEscape(`{"location":{"mode":"radius"}}`), 400}, {"/api/v1/community/activity?city=London", 400}, {"/api/v1/events/activity-counts", 400}, {"/api/v1/events/activity-counts?event_id=ticketmaster:test&event_id=ticketmaster:test", 200}, {"/api/v1/events/activity-counts?event_id=bad", 400}} {
		response, err := http.Get(server.URL + test.path)
		if err != nil {
			t.Fatal(err)
		}
		if response.StatusCode != test.status || response.Header.Get("Cache-Control") != "private, no-store" {
			t.Fatal(test.path, response.Status, response.Header)
		}
		response.Body.Close()
	}
	ctx, cancel := context.WithTimeout(t.Context(), 8*time.Second)
	defer cancel()
	request, _ := http.NewRequestWithContext(ctx, "GET", server.URL+"/api/v1/community/activity/stream?event_id=ticketmaster:test", nil)
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.Header.Get("X-Accel-Buffering") != "no" || !strings.HasPrefix(response.Header.Get("Content-Type"), "text/event-stream") {
		t.Fatal(response.Header)
	}
	scanner := bufio.NewScanner(response.Body)
	found := false
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "id:") {
			t.Fatal("unexpected replay cursor")
		}
		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		var body struct {
			Items []map[string]any `json:"items"`
		}
		if json.Unmarshal([]byte(strings.TrimPrefix(line, "data: ")), &body) != nil {
			t.Fatal("invalid JSON")
		}
		if len(body.Items) == 0 {
			continue
		}
		if body.Items[0]["actor_first_name"] == nil {
			continue
		}
		allowed := map[string]bool{"event_id": true, "event_title": true, "city": true, "actor_first_name": true, "recent_recommendations": true, "latest_recommended_at": true, "expires_at": true}
		for key := range body.Items[0] {
			if !allowed[key] {
				t.Fatal("unexpected public identity field", key)
			}
		}
		found = true
		break
	}
	if !found {
		t.Fatal("no initial activity snapshot", scanner.Err())
	}
	cancel()
}

func TestLiveBoundsAndSharedPolling(t *testing.T) {
	query := strings.Repeat("event_id=ticketmaster:test&", 102)
	ids, err := activityIDs(query, true, true)
	if err != nil || len(ids) != 1 {
		t.Fatal("duplicates consumed limit", err)
	}
	repo := &liveRepository{}
	service := activity.New(t.Context(), repo, 2)
	a, releaseA, err := service.Subscribe(nil)
	if err != nil {
		t.Fatal(err)
	}
	defer releaseA()
	b, releaseB, err := service.Subscribe(nil)
	if err != nil {
		t.Fatal(err)
	}
	defer releaseB()
	if _, _, err := service.Subscribe(nil); err == nil {
		t.Fatal("capacity not bounded")
	}
	for _, ch := range []<-chan activity.Update{a, b} {
		select {
		case <-ch:
		case <-time.After(4 * time.Second):
			t.Fatal("shared poll missed")
		}
	}
	if repo.polls.Load() != 1 {
		t.Fatal("polled once per subscriber", repo.polls.Load())
	}
	service.Close()
	if _, _, err := service.Subscribe(nil); err == nil {
		t.Fatal("subscribed after shutdown")
	}
}

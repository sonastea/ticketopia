package persistence

import (
	"fmt"
	"net/http/httptest"
	"os"
	"os/exec"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"github.com/sonastea/ticketopia/internal/api"
	"github.com/sonastea/ticketopia/internal/discovery"
	"github.com/sonastea/ticketopia/internal/history"
	"github.com/sonastea/ticketopia/internal/kv"
	"github.com/sonastea/ticketopia/internal/models"
)

func TestMariaDBHistoryBrowserReview(t *testing.T) {
	script := os.Getenv("HISTORY_BROWSER_SCRIPT")
	if script == "" {
		t.Skip("set HISTORY_BROWSER_SCRIPT for isolated Chromium verification")
	}
	t.Setenv("TICKETMASTER_KEY", "")
	t.Setenv("IP_GEOLOCATION_ENABLED", "false")
	f := newMaria(t)
	f.migrate(t)
	pool := f.open(t)
	for _, city := range []string{"Berlin", "Partial", "Older", "Empty"} {
		at := time.Now().Add(-time.Hour)
		if city == "Older" {
			at = at.Add(-24 * time.Hour)
		}
		events := []models.Event{}
		if city != "Empty" {
			for i := range 7 {
				e := sampleEvent(fmt.Sprintf("browser-%s-%d", city, i))
				e.Name = fmt.Sprintf("The Band · %s show %d", city, i)
				date := "2026-10-08"
				e.Start.LocalDate = &date
				e.Venues[0].City = city
				e.Venues[0].CountryCode = "DE"
				e.Venues[0].Name = "Old <venue>"
				events = append(events, e)
			}
		}
		for offset := range 3 {
			if city == "Partial" && offset > 0 {
				continue
			}
			task := history.NewTask(history.Scope{City: city, Country: "DE"}, fmt.Sprintf("2026-10-%02d", 8+offset))
			if err := pool.History().Schedule(t.Context(), []history.Task{task}); err != nil {
				t.Fatal(err)
			}
			claim := collectionClaim(t, pool.History(), task.ID)
			items := []models.Event{}
			if offset == 0 {
				items = events
			}
			if err := pool.History().Page(t.Context(), claim, 0, models.EventList{Items: items, Total: len(items), Meta: models.Freshness{DataAsOf: at}}); err != nil {
				t.Fatal(err)
			}
			if _, err := pool.History().Finish(t.Context(), claim, "complete", "", 6*time.Hour); err != nil {
				t.Fatal(err)
			}
			if city == "Older" {
				execSQL(t, f.admin, `UPDATE `+f.runtime.Database+`.collection_tasks SET last_success=TIMESTAMPADD(HOUR,-24,UTC_TIMESTAMP(6)) WHERE task_id=?`, task.ID)
			}
		}
		if city == "Berlin" {
			e := events[0]
			date := "2026-10-09"
			e.Start.LocalDate = &date
			e.Venues[0].ID = "ticketmaster:new"
			e.Venues[0].Name = "New venue"
			if err := pool.Discovery().Observe(t.Context(), models.EventDetail{Item: e, Meta: models.Freshness{DataAsOf: time.Now()}}); err != nil {
				t.Fatal(err)
			}
		}
	}
	cache := kv.NewMemory()
	defer cache.Close()
	s := discovery.New(t.Context(), cache, zerolog.Nop(), discovery.Config{Catalog: pool.Discovery()})
	app, err := api.NewAPI(t.Context(), zerolog.Nop(), cache, api.WithDiscovery(s))
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(app.Routes())
	defer server.Close()
	command := exec.CommandContext(t.Context(), "node", script)
	command.Env = append(os.Environ(), "HISTORY_BASE_URL="+server.URL)
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("history browser review: %v\n%s", err, output)
	}
	t.Log(string(output))
}

package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/sonastea/ticketopia/internal/accounts"
	"github.com/sonastea/ticketopia/internal/models"
	"github.com/sonastea/ticketopia/internal/radar"
)

type radarStub struct {
	owner     string
	d         radar.Dataset
	err       error
	lastOwner string
}

func (r *radarStub) Read(_ context.Context, owner string, _ time.Time, _ time.Duration) (radar.Dataset, error) {
	r.lastOwner = owner
	if r.err != nil {
		return radar.Dataset{}, r.err
	}
	if owner != r.owner {
		return radar.Dataset{}, nil
	}
	return r.d, nil
}
func TestRadarWebAPIPrivacyParityPaginationAndRecovery(t *testing.T) {
	a, auth, browser, bearer, _ := accountAPI(t)
	date := time.Now().AddDate(0, 0, 1).Format(time.DateOnly)
	r := &radarStub{owner: auth.account.ID, d: radar.Dataset{Preferences: accounts.Preferences{City: "Berlin", Country: "DE", Timezone: "UTC", CategoryIDs: []string{"music"}}, Meta: models.Freshness{Coverage: &models.Coverage{Status: "partial", TotalDays: 90}}}}
	for _, id := range []string{"a", "b"} {
		r.d.Candidates = append(r.d.Candidates, radar.Candidate{Event: models.Event{ID: "ticketmaster:" + id, Name: "Show " + id, Start: models.EventStart{LocalDate: &date}, Artists: []models.Artist{{ID: "ticketmaster:Artist", Name: "Private artist <script>"}}, Classifications: []models.Classification{{Segment: &models.NamedID{ID: "music", Name: "Music"}}}}, DataAsOf: time.Now().Add(-24 * time.Hour)})
	}
	r.d.Follows = []radar.Signal{{Kind: "artist", ID: "ticketmaster:Artist"}}
	a.radar = radar.New(r, nil, 6*time.Hour)
	for _, tc := range []struct {
		path, cookie, token string
		status              int
	}{
		{"/api/v1/me/radar", "", "", 401}, {"/api/v1/me/radar", browser, "", 200}, {"/api/v1/me/radar", "", bearer, 200},
		{"/api/v1/me/radar?owner=other", "", bearer, 400}, {"/api/v1/me/radar?limit=101", "", bearer, 400}, {"/api/v1/me/radar?limit=1&limit=2", "", bearer, 400},
		{"/radar", browser, "", 200}, {"/radar?limit=0", browser, "", 400}, {"/radar?city=Paris", browser, "", 400},
	} {
		w := accountRequest(a, "GET", tc.path, "", tc.cookie, tc.token, "", "", "")
		if w.Code != tc.status || w.Header().Get("Cache-Control") != "private, no-store" {
			t.Fatal(tc, w.Code, w.Body.String())
		}
	}
	w := accountRequest(a, "GET", "/api/v1/me/radar?limit=1", "", "", bearer, "", "", "")
	var first radar.List
	if json.Unmarshal(w.Body.Bytes(), &first) != nil || len(first.Items) != 1 || first.NextCursor == nil {
		t.Fatal(w.Body.String())
	}
	web := accountRequest(a, "GET", "/radar?limit=1", "", browser, "", "", "", "")
	if !strings.Contains(web.Body.String(), "You follow Private artist &lt;script&gt;") || !strings.Contains(web.Body.String(), "You chose Music") || !strings.Contains(web.Body.String(), "Showing last-known details") || !strings.Contains(web.Body.String(), "Partial coverage") || !strings.Contains(web.Body.String(), first.Items[0].Event.ID) || strings.Contains(web.Body.String(), "Show b") || strings.Contains(web.Body.String(), "href=\"#event-context\"") {
		t.Fatal("web/API mismatch", web.Body.String())
	}
	path := "/api/v1/me/radar?limit=1&cursor=" + url.QueryEscape(*first.NextCursor)
	w = accountRequest(a, "GET", path, "", "", bearer, "", "", "")
	var next radar.List
	_ = json.Unmarshal(w.Body.Bytes(), &next)
	if w.Code != 200 || len(next.Items) != 1 || next.Items[0].Event.ID != "ticketmaster:b" || next.NextCursor != nil {
		t.Fatal(w.Code, w.Body.String())
	}
	r.d.Preferences.CategoryIDs = []string{"sports"}
	w = accountRequest(a, "GET", path, "", "", bearer, "", "", "")
	if w.Code != 409 || !strings.Contains(w.Body.String(), "radar_changed") {
		t.Fatal(w.Code, w.Body.String())
	}
	web = accountRequest(a, "GET", strings.Replace(path, "/api/v1/me/radar", "/radar", 1), "", browser, "", "", "", "")
	if web.Code != 409 || !strings.Contains(web.Body.String(), "Refresh Radar") {
		t.Fatal("no cursor recovery", web.Body.String())
	}
	auth.account.ID = accounts.ID()
	w = accountRequest(a, "GET", "/api/v1/me/radar", "", "", bearer, "", "", "")
	if w.Code != 200 || strings.Contains(w.Body.String(), "Private artist") || r.lastOwner != auth.account.ID {
		t.Fatal("owner leakage", w.Body.String())
	}
	r.err = errors.New("database-secret")
	w = accountRequest(a, "GET", "/api/v1/me/radar", "", "", bearer, "", "", "")
	if w.Code != 503 || strings.Contains(w.Body.String(), "database-secret") || !strings.Contains(w.Body.String(), "radar_unavailable") {
		t.Fatal(w.Code, w.Body.String())
	}
	guest := accountRequest(a, "GET", "/radar", "", "", "", "", "", "")
	if guest.Code != 303 || !strings.Contains(guest.Header().Get("Location"), "return_to=%2Fradar") {
		t.Fatal("guest return", guest.Code, guest.Header())
	}
	for _, safe := range []string{"/radar", "/radar?limit=1"} {
		if safeAuthReturn(safe) != safe || safeReturnURL(safe) != safe || safeSaveReturn(safe) != safe || safeRecommendationReturn(safe) != safe {
			t.Fatal("radar task context lost", safe)
		}
	}
	for _, unsafe := range []string{"https://evil.example/radar", "/radar/other", "/radar?limit=0", "/radar?owner=other", "/radar#x", "/radar?cursor="} {
		if safeRadarReturn(unsafe) != "/radar" {
			t.Fatal("unsafe return", unsafe)
		}
	}
	a.authConfig.Enabled = false
	w = accountRequest(a, "GET", "/api/v1/me/radar", "", "", bearer, "", "", "")
	if w.Code != 503 {
		t.Fatal(w.Code)
	}
	web = accountRequest(a, "GET", "/radar", "", "", "", "", "", "")
	if web.Code != 200 || !strings.Contains(web.Body.String(), "aren't enabled") {
		t.Fatal(web.Code, web.Body.String())
	}
}

package api

import (
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/rs/zerolog"
	"github.com/sonastea/ticketopia/internal/activity"
	"github.com/sonastea/ticketopia/internal/kv"
)

func TestHappeningNowNativeAndReturn(t *testing.T) {
	t.Setenv("IP_GEOLOCATION_ENABLED", "false")
	service := activity.New(t.Context(), &liveRepository{}, 2)
	defer service.Close()
	app, err := NewAPI(t.Context(), zerolog.Nop(), kv.NewMemory(), WithLiveActivity(service, 60))
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		path   string
		status int
		want   string
	}{
		{"/happening-now", 200, "Alex recommended"},
		// This minimal repository has no city resolver: do not silently widen.
		{"/happening-now?location_mode=city&live_city=Berlin&live_country=de", 503, "Berlin, DE"},
		{"/happening-now?location_mode=radius", 400, "not supported"},
		{"/happening-now?private=saved", 400, "Unknown activity filter"},
		{"/happening-now?location_mode=city&location_mode=all", 400, "one value"},
	} {
		response := httptest.NewRecorder()
		app.Routes().ServeHTTP(response, httptest.NewRequest("GET", test.path, nil))
		if response.Code != test.status || !strings.Contains(response.Body.String(), test.want) {
			t.Fatalf("%s: status %d; expected %d and %q", test.path, response.Code, test.status, test.want)
		}
		if response.Header().Get("Cache-Control") != "private, no-store" {
			t.Fatal(response.Header())
		}
	}
	for _, raw := range []string{"/happening-now", "/happening-now?location_mode=city&live_city=Berlin&live_country=DE"} {
		if safeReturnURL(raw) != raw {
			t.Fatal(raw)
		}
	}
	for _, raw := range []string{"/happening-now-other", "/happening-now?private=owner", "/happening-now#fragment", "//elsewhere/happening-now"} {
		if safeReturnURL(raw) != "/" {
			t.Fatal(raw)
		}
	}
}

func TestHappeningNowDisabled(t *testing.T) {
	t.Setenv("IP_GEOLOCATION_ENABLED", "false")
	app, err := NewAPI(t.Context(), zerolog.Nop(), kv.NewMemory())
	if err != nil {
		t.Fatal(err)
	}
	response := httptest.NewRecorder()
	app.Routes().ServeHTTP(response, httptest.NewRequest("GET", "/happening-now", nil))
	body := response.Body.String()
	if response.Code != 200 || !strings.Contains(body, "unavailable on this server") || strings.Contains(body, `href="/happening-now"`) || strings.Contains(body, "data-live-activity") {
		t.Fatal(body)
	}
}

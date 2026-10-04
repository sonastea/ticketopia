package api

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"html"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/rs/zerolog"
	"github.com/sonastea/ticketopia/internal/discovery"
	"github.com/sonastea/ticketopia/internal/kv"
	"github.com/sonastea/ticketopia/internal/location"
)

func TestClientIPTrustIsExplicit(t *testing.T) {
	for _, tc := range []struct{ name, trusted, peer, xff, want string }{
		{"direct", "", "8.8.8.8:1234", "1.1.1.1", "8.8.8.8"},
		{"local proxy not implicitly trusted", "", "127.0.0.1:1234", "8.8.8.8", "127.0.0.1"},
		{"trusted proxy", "127.0.0.1/32", "127.0.0.1:1234", "8.8.8.8", "8.8.8.8"},
		{"untrusted peer", "127.0.0.1/32", "9.9.9.9:1234", "8.8.8.8", "9.9.9.9"},
		{"spoofed leftmost address", "127.0.0.1/32,10.0.0.2/32", "127.0.0.1:1234", "1.1.1.1, 8.8.8.8, 10.0.0.2", "8.8.8.8"},
		{"malformed chain", "127.0.0.1/32", "127.0.0.1:1234", "8.8.8.8, garbage", "127.0.0.1"},
		{"IPv6", "::1/128", "[::1]:1234", "2606:4700:4700::1111", "2606:4700:4700::1111"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			extract, err := clientIPExtractor(tc.trusted)
			if err != nil {
				t.Fatal(err)
			}
			req := httptest.NewRequest(http.MethodGet, "/", nil)
			req.RemoteAddr = tc.peer
			req.Header.Set("X-Forwarded-For", tc.xff)
			req.Header.Set("X-Real-IP", "4.4.4.4")
			if got := extract(req); got != tc.want {
				t.Fatalf("got %s, want %s", got, tc.want)
			}
		})
	}
	if _, err := clientIPExtractor("not-a-cidr"); err == nil {
		t.Fatal("invalid proxy configuration accepted")
	}
}

func TestLocationDefaultsOverrideRememberAndShareEventCache(t *testing.T) {
	var geoCalls atomic.Int32
	geo := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		geoCalls.Add(1)
		_, _ = fmt.Fprint(w, `{"success":true,"city":"Boston","country_code":"US"}`)
	}))
	defer geo.Close()
	var eventCalls sync.Map
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "classifications") {
			_, _ = fmt.Fprint(w, `{"id":"KZFzniwnSyZfZ7v7nJ"}`)
			return
		}
		city, country, page := r.URL.Query().Get("city"), r.URL.Query().Get("countryCode"), r.URL.Query().Get("page")
		if city == "" {
			t.Error("web request made an unscoped event search")
		}
		counter, _ := eventCalls.LoadOrStore(city+":"+country+":"+page, &atomic.Int32{})
		counter.(*atomic.Int32).Add(1)
		_, _ = fmt.Fprintf(w, `{"page":{"number":%s,"size":20,"totalElements":40,"totalPages":2},"_embedded":{"events":[{"id":"show-%s-%s","name":"A show"}]}}`, page, city, page)
	}))
	defer upstream.Close()
	cache := kv.NewMemory()
	defer cache.Close()
	a := &api{
		events:    discovery.New(t.Context(), cache, zerolog.Nop(), discovery.Config{APIKey: "test", BaseURL: upstream.URL}),
		locations: location.New(t.Context(), cache, zerolog.Nop(), location.Config{BaseURL: geo.URL}),
	}
	routes := a.Routes()
	request := func(path, ip string, cookie *http.Cookie, partial bool) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodGet, "https://ticketopia.test"+path, nil)
		req.RemoteAddr = ip + ":1234"
		if cookie != nil {
			req.AddCookie(cookie)
		}
		if partial {
			req.Header.Set("HX-Request", "true")
		}
		rec := httptest.NewRecorder()
		routes.ServeHTTP(rec, req)
		return rec
	}
	first := request("/", "8.8.8.8", nil, false)
	if first.Code != 200 || !strings.Contains(first.Body.String(), "Estimated from your IP address") || !strings.Contains(first.Body.String(), `value="Boston"`) || first.Header().Get("Cache-Control") != "private, no-store" || len(first.Result().Cookies()) != 0 {
		t.Fatalf("automatic city response failed: %d %s", first.Code, first.Body.String())
	}
	request("/", "8.8.8.8", nil, false)
	request("/", "8.8.4.4", nil, false)
	boston, _ := eventCalls.Load("Boston:US:0")
	if geoCalls.Load() != 2 || boston.(*atomic.Int32).Load() != 1 {
		t.Fatal("IP and city-level event caches were not reused")
	}
	manual := request("/?city=London&country=GB", "8.8.8.8", nil, false)
	cookies := manual.Result().Cookies()
	if manual.Code != 200 || geoCalls.Load() != 2 || len(cookies) != 1 || cookies[0].Name != cityCookie || !cookies[0].HttpOnly || !cookies[0].Secure || cookies[0].SameSite != http.SameSiteLaxMode || cookies[0].MaxAge != 30*24*60*60 {
		t.Fatalf("manual city preference failed: %d %+v", manual.Code, cookies)
	}
	remembered := request("/", "1.1.1.1", cookies[0], false)
	if remembered.Code != 200 || !strings.Contains(remembered.Body.String(), "Your remembered city") || !strings.Contains(remembered.Body.String(), `value="London"`) || geoCalls.Load() != 2 {
		t.Fatal("remembered city was overridden by IP lookup")
	}
	override := request("/?city=Paris&country=FR", "1.1.1.1", cookies[0], false)
	if override.Code != 200 || !strings.Contains(override.Body.String(), `value="Paris"`) || geoCalls.Load() != 2 {
		t.Fatal("explicit search did not override remembered city")
	}
	link := regexp.MustCompile(`href="([^"]*cursor[^"]*)"`).FindStringSubmatch(first.Body.String())
	if len(link) != 2 {
		t.Fatal("missing next-page link")
	}
	more := request(html.UnescapeString(link[1]), "1.1.1.1", cookies[0], true)
	if more.Code != 200 || !strings.Contains(more.Body.String(), "ticketmaster:show-Boston-1") || geoCalls.Load() != 2 || len(more.Result().Cookies()) != 0 {
		t.Fatalf("pagination changed city or remembered state: %d %s", more.Code, more.Body.String())
	}
	for _, path := range []string{"/?city=", "/?country=US"} {
		result := request(path, "8.8.8.8", cookies[0], false)
		if result.Code != 200 || !strings.Contains(result.Body.String(), "Enter a city below") || strings.Contains(result.Body.String(), `id="event-list"`) || geoCalls.Load() != 2 {
			t.Fatalf("explicitly missing city was silently replaced: %s", result.Body.String())
		}
	}
}

func TestUnknownLocationNeverFetchesWorldwideEvents(t *testing.T) {
	var geoCalls, upstreamCalls atomic.Int32
	geo := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		geoCalls.Add(1)
		w.WriteHeader(503)
	}))
	defer geo.Close()
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		upstreamCalls.Add(1)
	}))
	defer upstream.Close()
	cache := kv.NewMemory()
	defer cache.Close()
	a := &api{
		events:    discovery.New(t.Context(), cache, zerolog.Nop(), discovery.Config{APIKey: "test", BaseURL: upstream.URL}),
		locations: location.New(t.Context(), cache, zerolog.Nop(), location.Config{BaseURL: geo.URL}),
	}
	for _, ip := range []string{"127.0.0.1:1234", "8.8.8.8:1234"} {
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req.RemoteAddr = ip
		req.Header.Set("X-Forwarded-For", "8.8.8.8")
		req.AddCookie(&http.Cookie{Name: cityCookie, Value: "broken"})
		rec := httptest.NewRecorder()
		a.Routes().ServeHTTP(rec, req)
		if rec.Code != 200 || !strings.Contains(rec.Body.String(), "Enter a city below") || strings.Contains(rec.Body.String(), `id="event-list"`) {
			t.Fatalf("location fallback failed: %d %s", rec.Code, rec.Body.String())
		}
	}
	if geoCalls.Load() != 1 || upstreamCalls.Load() != 0 {
		t.Fatalf("unknown location made unrelated calls: geo=%d events=%d", geoCalls.Load(), upstreamCalls.Load())
	}
	req := httptest.NewRequest(http.MethodGet, "/?limit=invalid", nil)
	req.RemoteAddr = "1.1.1.1:1234"
	rec := httptest.NewRecorder()
	a.Routes().ServeHTTP(rec, req)
	if rec.Code != 400 || geoCalls.Load() != 1 || upstreamCalls.Load() != 0 {
		t.Fatal("invalid search resolved location or called Ticketmaster")
	}
}

func TestCityCookieRejectsInvalidLocations(t *testing.T) {
	a := &api{}
	routes := a.Routes()
	for _, value := range []string{`{"city":"","country":"US"}`, `{"city":"Boston","country":"USA"}`, `null`, strings.Repeat("x", 600)} {
		encoded := base64.RawURLEncoding.EncodeToString([]byte(value))
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req.AddCookie(&http.Cookie{Name: cityCookie, Value: encoded})
		ctx := routes.NewContext(req, httptest.NewRecorder())
		if _, ok := rememberedCity(ctx); ok {
			t.Fatalf("invalid city cookie accepted: %s", value)
		}
	}
	data, _ := json.Marshal(location.City{Name: " London  ", Country: "gb"})
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.AddCookie(&http.Cookie{Name: cityCookie, Value: base64.RawURLEncoding.EncodeToString(data)})
	if city, ok := rememberedCity(routes.NewContext(req, httptest.NewRecorder())); !ok || city.Name != "London" || city.Country != "GB" {
		t.Fatalf("valid city cookie failed normalization: %+v", city)
	}
}

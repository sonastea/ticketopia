package discovery

import (
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"sync/atomic"
	"testing"
	"time"
)

func TestCatalogSearchCacheDetailAndKindBoundPagination(t *testing.T) {
	var calls atomic.Int32
	s := newTestService(t, func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		q := r.URL.Query()
		if q.Get("keyword") != "Same name" || q.Get("size") != "1" || q.Get("sort") != "name,asc" {
			t.Error("catalog search filters lost")
		}
		if r.URL.Path == "/attractions.json" {
			fmt.Fprintf(w, `{"_embedded":{"attractions":[{"id":"Case_ID","name":"Same name","url":"https://example.com/artist"}]},"page":{"number":%s,"totalElements":2,"totalPages":2}}`, q.Get("page"))
		} else if r.URL.Path == "/venues.json" {
			if q.Get("city") != "Boston" || q.Get("countryCode") != "US" {
				t.Error("venue location filters lost")
			}
			fmt.Fprint(w, `{"_embedded":{"venues":[{"id":"Case_ID","name":"Same name","timezone":"America/New_York","address":{"line1":"1 Main St"},"city":{"name":"Boston"}}]},"page":{"number":0,"totalElements":1,"totalPages":1}}`)
		} else {
			t.Error("search/detail caused unexpected provider request", r.URL.Path)
		}
	})
	v := url.Values{"keyword": {" Same  name "}, "limit": {"1"}}
	first, err := s.Catalog(t.Context(), "artist", v)
	if err != nil || len(first.Items) != 1 || first.NextCursor == nil || first.Items[0].ID != "ticketmaster:Case_ID" {
		t.Fatal(first, err)
	}
	detail, err := s.CatalogDetail(t.Context(), "artist", first.Items[0].ID)
	if err != nil || detail.Item.Name != first.Items[0].Name || calls.Load() != 1 {
		t.Fatal("search-to-detail cache not reused", err)
	}
	_, err = s.Catalog(t.Context(), "artist", v)
	if err != nil || calls.Load() != 1 {
		t.Fatal("equivalent search missed cache", err)
	}
	v.Set("cursor", *first.NextCursor)
	if _, err := s.Catalog(t.Context(), "artist", v); err != nil {
		t.Fatal(err)
	}
	if calls.Load() != 2 {
		t.Fatal("pagination did not advance")
	}
	if _, err := s.Catalog(t.Context(), "venue", v); err == nil || calls.Load() != 2 {
		t.Fatal("artist cursor crossed kind")
	}
	v.Del("cursor")
	v.Set("city", "Boston")
	v.Set("country", "us")
	venues, err := s.Catalog(t.Context(), "venue", v)
	if err != nil || venues.Items[0].Kind != "venue" || venues.Items[0].Location.Address != "1 Main St" {
		t.Fatal("venue normalization", venues, err)
	}
	venue, err := s.CatalogDetail(t.Context(), "venue", "ticketmaster:Case_ID")
	if err != nil || venue.Item.Timezone != "America/New_York" || calls.Load() != 3 {
		t.Fatal("same ID collided across kinds", err)
	}
}
func TestCatalogValidationAndMalformedProvider(t *testing.T) {
	s := newTestService(t, func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprint(w, `{"_embedded":{"attractions":[{"id":"wrong/id","name":"Bad"}]},"page":{"number":0,"totalElements":1,"totalPages":1}}`)
	})
	for _, raw := range []string{"", "keyword=", "keyword=a&limit=101", "keyword=a&keyword=b", "keyword=a&city=Boston", "keyword=a&account_id=other", "keyword=a&cursor=bad", "keyword=%00bad"} {
		v, _ := url.ParseQuery(raw)
		if _, err := ParseCatalogQuery("artist", v); err == nil {
			t.Fatal("invalid search accepted", raw)
		}
	}
	if _, err := s.Catalog(t.Context(), "artist", url.Values{"keyword": {"bad"}}); err == nil {
		t.Fatal("invalid provider ID accepted")
	}
	if _, err := s.CatalogDetail(t.Context(), "venue", "ticketmaster:bad/id"); err == nil {
		t.Fatal("invalid target ID accepted")
	}
}
func TestCatalogStaleFallbackAndSharedBudget(t *testing.T) {
	failed := false
	s := newTestService(t, func(w http.ResponseWriter, _ *http.Request) {
		if failed {
			w.WriteHeader(503)
			return
		}
		fmt.Fprint(w, `{"_embedded":{"attractions":[{"id":"one","name":"One"}]},"page":{"number":0,"totalElements":1,"totalPages":1}}`)
	})
	now := time.Now()
	s.now = func() time.Time { return now }
	v := url.Values{"keyword": {"one"}}
	first, err := s.Catalog(t.Context(), "artist", v)
	if err != nil {
		t.Fatal(err)
	}
	failed = true
	now = now.Add(25 * time.Hour)
	stale, err := s.Catalog(t.Context(), "artist", v)
	if err != nil || !stale.Meta.Stale || !stale.Meta.DataAsOf.Equal(first.Meta.DataAsOf) {
		t.Fatal("retained search lost", err)
	}
	s.gate.budget = 1
	_, err = s.Catalog(t.Context(), "artist", url.Values{"keyword": {"uncached"}})
	var unavailable *UnavailableError
	if !errors.As(err, &unavailable) {
		t.Fatal("catalog bypassed provider gate", err)
	}
}

func TestCatalogDirectDetailsAndSharedRequestBudget(t *testing.T) {
	var calls atomic.Int32
	s := newTestService(t, func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		switch r.URL.Path {
		case "/attractions/Artist.json":
			fmt.Fprint(w, `{"id":"Artist","name":"Direct artist","url":"https://example.com/artist"}`)
		case "/venues/Venue.json":
			fmt.Fprint(w, `{"id":"Venue","name":"Direct venue","city":{"name":"Boston"}}`)
		case "/attractions/Wrong.json":
			fmt.Fprint(w, `{"id":"Other","name":"Wrong identity"}`)
		default:
			w.WriteHeader(404)
		}
	})
	artist, err := s.CatalogDetail(t.Context(), "artist", "ticketmaster:Artist")
	if err != nil || artist.Item.Source.ID != "Artist" || artist.Item.Kind != "artist" {
		t.Fatal("direct artist metadata lost", err)
	}
	venue, err := s.CatalogDetail(t.Context(), "venue", "ticketmaster:Venue")
	if err != nil || venue.Item.Location.City != "Boston" || venue.Item.Kind != "venue" {
		t.Fatal("direct venue metadata lost", err)
	}
	if _, err := s.CatalogDetail(t.Context(), "artist", "ticketmaster:Wrong"); err == nil {
		t.Fatal("mismatched provider identity accepted")
	}
	if _, err := s.CatalogDetail(t.Context(), "artist", "ticketmaster:Missing"); !errors.Is(err, ErrNotFound) {
		t.Fatal("404 not distinguished", err)
	}
	before := calls.Load()
	s.gate.budget = 1
	if _, err := s.Catalog(t.Context(), "venue", url.Values{"keyword": {"uncached"}}); err == nil || calls.Load() != before {
		t.Fatal("search did not share detail request budget", err)
	}
	if _, err := s.Event(t.Context(), "ticketmaster:uncached"); err == nil || calls.Load() != before {
		t.Fatal("event request did not share catalog budget", err)
	}
}

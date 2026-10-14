package radar

import (
	"context"
	"errors"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/sonastea/ticketopia/internal/accounts"
	"github.com/sonastea/ticketopia/internal/discovery"
	"github.com/sonastea/ticketopia/internal/models"
)

type repository struct {
	d     Dataset
	err   error
	reads int
	query Query
	at    time.Time
}

func (r *repository) Read(_ context.Context, _ string, at time.Time, _ time.Duration, q Query) (Dataset, error) {
	r.reads++
	r.query, r.at = q, at
	return r.d, r.err
}

type provider struct {
	calls  int
	values discovery.Query
}

func (p *provider) Events(_ context.Context, q discovery.Query) (models.EventList, error) {
	p.calls++
	p.values = q
	return models.EventList{}, errors.New("provider unavailable")
}

var instant = time.Date(2026, 10, 8, 12, 0, 0, 0, time.UTC)

const owner = "00000000000000000000000000000001"

func ptr[T any](v T) *T { return &v }
func event(id string) Candidate {
	return Candidate{Event: models.Event{ID: "ticketmaster:" + id, Name: id, Start: models.EventStart{LocalDate: ptr("2026-10-09"), LocalTime: ptr("20:00:00")}}, DataAsOf: instant}
}
func prefs() accounts.Preferences {
	return accounts.Preferences{City: "Berlin", Country: "DE", Timezone: "Europe/Berlin", CategoryIDs: []string{"music"}}
}
func setup(d Dataset) (*Service, *repository) {
	d.Revision = strings.Repeat("a", 64)
	r := &repository{d: d}
	s := New(r, nil, 6*time.Hour)
	s.now = func() time.Time { return instant }
	return s, r
}

func TestReasonsUseExactIdentitiesAndDistinctSignals(t *testing.T) {
	e := event("one").Event
	e.Artists = []models.Artist{{ID: "ticketmaster:Artist", Name: "The Band"}, {ID: "ticketmaster:Artist", Name: "The Band"}, {ID: "ticketmaster:artist"}}
	e.Venues = []models.Venue{{ID: "ticketmaster:venue"}}
	e.Classifications = []models.Classification{{Segment: &models.NamedID{ID: "music", Name: "Music"}}, {Segment: &models.NamedID{ID: "music", Name: "Music"}}}
	reasons := match(e, map[string]bool{"artist:ticketmaster:Artist": true, "venue:ticketmaster:venue": true}, map[string]bool{"music": true})
	if len(reasons) != 3 || reasons[0].Text != "You follow The Band" || reasons[1].Text != "At a venue you follow" || reasons[2].Text != "You chose Music" {
		t.Fatal(reasons)
	}
}

func TestPageContractFreshnessAndCursor(t *testing.T) {
	c := event("one")
	s, r := setup(Dataset{Preferences: prefs(), Candidates: []Candidate{c}, HasMore: true, Meta: models.Freshness{DataAsOf: instant.Add(-time.Hour)}})
	first, err := s.List(t.Context(), owner, url.Values{"limit": {"1"}})
	if err != nil || first.NextCursor == nil || len(first.Items) != 1 || r.query.Limit != 1 {
		t.Fatal(first, err)
	}
	// The repository supplies the next ranked page; time changes freshness but
	// neither the revision nor the pinned cutoff/window.
	r.d.Candidates = []Candidate{event("two")}
	r.d.HasMore = false
	s.now = func() time.Time { return instant.Add(7 * time.Hour) }
	q := url.Values{"limit": {"1"}, "cursor": {*first.NextCursor}}
	second, err := s.List(t.Context(), owner, q)
	if err != nil || second.Items[0].Event.ID != "ticketmaster:two" || second.NextCursor != nil || !second.Meta.Stale || !second.Items[0].Meta.Stale || !r.at.Equal(instant) {
		t.Fatal(second, err, r.at)
	}
	if _, err = s.List(t.Context(), accounts.ID(), q); err == nil {
		t.Fatal("foreign cursor")
	}
	q.Set("limit", "2")
	if _, err = s.List(t.Context(), owner, q); err == nil {
		t.Fatal("changed limit")
	}
	q.Set("limit", "1")
	r.d.Revision = strings.Repeat("b", 64)
	if _, err = s.List(t.Context(), owner, q); !errors.Is(err, ErrChanged) {
		t.Fatal(err)
	}
	r.err = InvalidCursor()
	var validation *accounts.ValidationError
	if _, err = s.List(t.Context(), owner, q); !errors.As(err, &validation) {
		t.Fatal(err)
	}
	r.err = ErrChanged
	if _, err = s.List(t.Context(), owner, q); !errors.Is(err, ErrChanged) {
		t.Fatal(err)
	}
	r.err = errors.New("database secret")
	if _, err = s.List(t.Context(), owner, q); !errors.Is(err, ErrUnavailable) {
		t.Fatal(err)
	}
	s.now = func() time.Time { return instant.Add(CursorLifetime + time.Second) }
	if _, err = s.List(t.Context(), owner, q); err == nil {
		t.Fatal("expired cursor")
	}
}

func TestModesAndRefreshBoundary(t *testing.T) {
	s, r := setup(Dataset{Preferences: prefs(), Refresh: true, Candidates: []Candidate{event("one")}, HasMore: true})
	p := &provider{}
	s.discovery = p
	list, err := s.List(t.Context(), owner, url.Values{"limit": {"1"}})
	if err != nil || list.Mode != "personalized" || p.calls != 1 || r.reads != 2 {
		t.Fatal(list, err, p.calls, r.reads)
	}
	if p.values.City != "Berlin" || p.values.EndDate != "2027-01-05" || p.values.ArtistID != "" || p.values.CategoryID != "" {
		t.Fatal(p.values)
	}
	if _, err = s.List(t.Context(), owner, url.Values{"limit": {"1"}, "cursor": {*list.NextCursor}}); err != nil || p.calls != 1 {
		t.Fatal(err, p.calls)
	}
	r.d.Preferences.CategoryIDs = nil
	r.d.Candidates[0].Unconfirmed = true
	list, err = s.List(t.Context(), owner, nil)
	if err != nil || list.Mode != "nearby" || list.Items[0].Reasons[0].Code != "nearby" || !list.Items[0].Meta.Stale {
		t.Fatal(list, err)
	}
	r.d.Preferences.City = ""
	list, err = s.List(t.Context(), owner, nil)
	if err != nil || list.Mode != "needs_location" || len(list.Items) != 0 {
		t.Fatal(list, err)
	}
	for _, raw := range []string{"limit=0", "limit=101", "limit=1&limit=2", "cursor=", "owner=other", "city=Paris", "limit="} {
		q, _ := url.ParseQuery(raw)
		if _, err = s.List(t.Context(), owner, q); err == nil {
			t.Fatal("accepted", raw)
		}
	}
	if _, err = s.List(t.Context(), "other", nil); !errors.Is(err, accounts.ErrUnauthenticated) {
		t.Fatal(err)
	}
}

func TestAccountTimezoneDateWindow(t *testing.T) {
	p := prefs()
	p.Timezone = "America/Los_Angeles"
	scope := DateScope(p, time.Date(2026, 10, 8, 1, 0, 0, 0, time.UTC))
	if scope.StartDate != "2026-10-07" || scope.EndDate != "2027-01-04" {
		t.Fatal(scope)
	}
}

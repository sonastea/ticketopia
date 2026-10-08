package radar

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/url"
	"slices"
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
}

func (r *repository) Read(context.Context, string, time.Time, time.Duration) (Dataset, error) {
	r.reads++
	return r.d, r.err
}

type provider struct {
	calls  int
	values discovery.Query
	err    error
}

func (p *provider) Events(_ context.Context, v discovery.Query) (models.EventList, error) {
	p.calls++
	p.values = v
	return models.EventList{}, p.err
}

var instant = time.Date(2026, 10, 8, 12, 0, 0, 0, time.UTC)

const owner = "00000000000000000000000000000001"

func ptr[T any](v T) *T { return &v }
func event(id string) Candidate {
	return Candidate{Event: models.Event{ID: "ticketmaster:" + id, Name: id, Start: models.EventStart{LocalDate: ptr("2026-10-09"), LocalTime: ptr("20:00:00")}}, DataAsOf: instant}
}
func setup(d Dataset) (*Service, *repository) {
	r := &repository{d: d}
	s := New(r, nil, 6*time.Hour)
	s.now = func() time.Time { return instant }
	return s, r
}
func prefs() accounts.Preferences {
	return accounts.Preferences{City: "Berlin", Country: "DE", Timezone: "Europe/Berlin", CategoryIDs: []string{"music"}}
}

func TestRankingReasonsAndUnknownMetadata(t *testing.T) {
	d := Dataset{Preferences: prefs(), Follows: []Signal{{"venue", "ticketmaster:venue"}, {"artist", "ticketmaster:Artist"}}}
	category := event("category")
	category.Event.Classifications = []models.Classification{{Segment: &models.NamedID{ID: "music", Name: "Music"}}, {Segment: &models.NamedID{ID: "music", Name: "Music"}}}
	venue := event("venue")
	venue.Event.Venues = []models.Venue{{ID: "ticketmaster:venue"}}
	artist := event("artist")
	artist.Event.Artists = []models.Artist{{ID: "ticketmaster:Artist", Name: "The Band"}, {ID: "ticketmaster:Artist", Name: "The Band"}}
	both := artist
	both.Event = artist.Event
	both.Event.ID = "ticketmaster:both"
	both.Event.Venues = venue.Event.Venues
	both.Event.Classifications = category.Event.Classifications
	wrongCase := event("case")
	wrongCase.Event.Artists = []models.Artist{{ID: "ticketmaster:artist", Name: "The Band"}}
	unknown := event("unknown")
	past := category
	past.Event = category.Event
	past.Event.ID = "ticketmaster:past"
	past.Event.Start.DateTime = ptr(instant.Add(-time.Hour))
	cancelled := category
	cancelled.Event = category.Event
	cancelled.Event.ID = "ticketmaster:cancelled"
	cancelled.Event.Status = "cancelled"
	undated := category
	undated.Event = category.Event
	undated.Event.ID = "ticketmaster:undated"
	undated.Event.Start.LocalDate = nil
	d.Candidates = []Candidate{category, venue, artist, both, wrongCase, unknown, past, cancelled, undated}
	s, _ := setup(d)
	list, err := s.List(t.Context(), owner, nil)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"ticketmaster:both", "ticketmaster:artist", "ticketmaster:venue", "ticketmaster:category"}
	for i, item := range list.Items {
		if i >= len(want) || item.Event.ID != want[i] {
			t.Fatalf("ordering: %+v", list)
		}
	}
	if len(list.Items) != 4 || len(list.Items[0].Reasons) != 3 || len(list.Items[3].Reasons) != 1 {
		t.Fatalf("deduplication: %+v", list)
	}
	if list.Items[2].Reasons[0].Text != "At a venue you follow" || list.Items[1].Reasons[0].Text != "You follow The Band" {
		t.Fatal("unhelpful reasons", list.Items)
	}
	if list.Items[0].Event.PriceRanges != nil {
		t.Fatal("invented price")
	}
	// Labels can be unavailable without losing identity-based matches.
	d.Candidates[0].Event.Classifications = []models.Classification{{Segment: &models.NamedID{ID: "music"}}}
	s, _ = setup(d)
	list, err = s.List(t.Context(), owner, nil)
	if err != nil || list.Items[3].Reasons[0].Text != "Matches one of your preferred categories" {
		t.Fatal("missing label", list, err)
	}
}
func TestColdStartEmptyAndFreshness(t *testing.T) {
	s, r := setup(Dataset{Preferences: prefs()})
	r.d.Preferences.CategoryIDs = nil
	list, err := s.List(t.Context(), owner, nil)
	if err != nil || list.Mode != "nearby" || len(list.Items) != 0 {
		t.Fatal(list, err)
	}
	c := event("one")
	c.DataAsOf = instant.Add(-24 * time.Hour)
	r.d.Candidates = []Candidate{c}
	list, err = s.List(t.Context(), owner, nil)
	if err != nil || !list.Meta.Stale || !list.Items[0].Meta.Stale || list.Items[0].Reasons[0].Code != "nearby" {
		t.Fatal(list, err)
	}
	r.d.Candidates[0].DataAsOf = instant
	r.d.Candidates[0].Unconfirmed = true
	list, _ = s.List(t.Context(), owner, nil)
	if !list.Items[0].Meta.Stale {
		t.Fatal("omitted event not labeled last-known")
	}
	r.d.Preferences.CategoryIDs = []string{"unavailable-category"}
	list, _ = s.List(t.Context(), owner, nil)
	if list.Mode != "personalized" || len(list.Items) != 0 {
		t.Fatal("unknown preferences broadened", list)
	}
	r.d.Preferences.City = ""
	list, _ = s.List(t.Context(), owner, nil)
	if list.Mode != "needs_location" || len(list.Items) != 0 {
		t.Fatal("missing location", list)
	}
	r.err = errors.New("database secret")
	if _, err := s.List(t.Context(), owner, nil); !errors.Is(err, ErrUnavailable) {
		t.Fatal(err)
	}
}
func TestPaginationVersionOwnershipExpiryAndReordering(t *testing.T) {
	d := Dataset{Preferences: prefs()}
	d.Preferences.CategoryIDs = nil
	for _, id := range []string{"c", "a", "b"} {
		d.Candidates = append(d.Candidates, event(id))
	}
	s, r := setup(d)
	v := url.Values{"limit": {"1"}}
	first, err := s.List(t.Context(), owner, v)
	if err != nil || first.Items[0].Event.ID != "ticketmaster:a" || first.NextCursor == nil {
		t.Fatal(first, err)
	}
	v.Set("cursor", *first.NextCursor)
	slices.Reverse(r.d.Candidates)
	s.now = func() time.Time { return instant.Add(7 * time.Hour) }
	second, err := s.List(t.Context(), owner, v)
	if err != nil || second.Items[0].Event.ID != "ticketmaster:b" || !second.Meta.Stale {
		t.Fatal(second, err)
	}
	v.Set("cursor", *second.NextCursor)
	third, err := s.List(t.Context(), owner, v)
	if err != nil || third.Items[0].Event.ID != "ticketmaster:c" || third.NextCursor != nil {
		t.Fatal(third, err)
	}
	if _, err := s.List(t.Context(), accounts.ID(), v); err == nil {
		t.Fatal("foreign cursor accepted")
	}
	v.Set("limit", "2")
	if _, err := s.List(t.Context(), owner, v); err == nil {
		t.Fatal("changed limit accepted")
	}
	v.Set("limit", "1")
	r.d.Candidates[0].Event.Name = "Renamed"
	if _, err := s.List(t.Context(), owner, v); !errors.Is(err, ErrChanged) {
		t.Fatal("changed catalog", err)
	}
	r.d.Candidates = d.Candidates
	r.d.Follows = []Signal{{"artist", "ticketmaster:new"}}
	if _, err := s.List(t.Context(), owner, v); !errors.Is(err, ErrChanged) {
		t.Fatal("changed follows", err)
	}
	r.d.Preferences.City = ""
	if _, err := s.List(t.Context(), owner, v); !errors.Is(err, ErrChanged) {
		t.Fatal("removed location", err)
	}
	s.now = func() time.Time { return instant.Add(CursorLifetime + time.Second) }
	if _, err := s.List(t.Context(), owner, v); err == nil {
		t.Fatal("expired cursor accepted")
	}
}
func TestInputAndRefreshBudgetBoundary(t *testing.T) {
	s, r := setup(Dataset{Preferences: prefs(), Refresh: true})
	for _, id := range []string{"a", "b"} {
		c := event(id)
		c.Event.Classifications = []models.Classification{{Segment: &models.NamedID{ID: "music"}}}
		r.d.Candidates = append(r.d.Candidates, c)
	}
	p := &provider{err: errors.New("provider unavailable")}
	s.discovery = p
	list, err := s.List(t.Context(), owner, url.Values{"limit": {"1"}})
	if err != nil || list.Items == nil || p.calls != 1 || r.reads != 2 {
		t.Fatal(list, err, p.calls, r.reads)
	}
	if p.values.City != "Berlin" || p.values.EndDate != "2027-01-05" || p.values.ArtistID != "" || p.values.VenueID != "" || p.values.CategoryID != "" {
		t.Fatal("not broad bounded collection", p.values)
	}
	if _, err := s.List(t.Context(), owner, url.Values{"limit": {"1"}, "cursor": {*list.NextCursor}}); err != nil || p.calls != 1 || r.reads != 3 {
		t.Fatal("continuation performed provider work", err, p.calls, r.reads)
	}
	for _, raw := range []string{"limit=0", "limit=101", "limit=1&limit=2", "cursor=", "owner=other", "city=Paris", "cursor=%%", "limit="} {
		v, _ := url.ParseQuery(raw)
		if raw == "cursor=%%" {
			v = url.Values{"cursor": {"%%"}}
		}
		if _, err := s.List(t.Context(), owner, v); err == nil {
			t.Fatal("accepted", raw)
		}
	}
	if _, err := s.List(t.Context(), "other", nil); !errors.Is(err, accounts.ErrUnauthenticated) {
		t.Fatal(err)
	}
	// A forged, well-formed cursor cannot refer to an absent anchor.
	c := Cursor{1, owner, 20, instant, strings.Repeat("0", 64), "ticketmaster:none"}
	data, _ := json.Marshal(c)
	_, err = s.List(t.Context(), owner, url.Values{"cursor": {base64.RawURLEncoding.EncodeToString(data)}})
	if !errors.Is(err, ErrChanged) {
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

func TestTimeOrderingAndForgedAnchor(t *testing.T) {
	d := Dataset{Preferences: prefs()}
	d.Preferences.CategoryIDs = nil
	known, missing, tba := event("known"), event("missing"), event("tba")
	missing.Event.Start.LocalTime = nil
	tba.Event.Start.TimeTBA = true
	tba.Event.Start.LocalTime = ptr("01:00:00")
	d.Candidates = []Candidate{tba, missing, known}
	s, r := setup(d)
	list, err := s.List(t.Context(), owner, url.Values{"limit": {"1"}})
	if err != nil || list.Items[0].Event.ID != "ticketmaster:known" || list.NextCursor == nil {
		t.Fatal("unknown time sorted before known time", list, err)
	}
	data, _ := base64.RawURLEncoding.DecodeString(*list.NextCursor)
	var c Cursor
	_ = json.Unmarshal(data, &c)
	c.After = "ticketmaster:absent"
	data, _ = json.Marshal(c)
	if _, err := s.List(t.Context(), owner, url.Values{"limit": {"1"}, "cursor": {base64.RawURLEncoding.EncodeToString(data)}}); err == nil || errors.Is(err, ErrChanged) {
		t.Fatal("absent anchor not rejected as invalid", err)
	}
	// Even an insertion after the anchor invalidates the full set version.
	r.d.Candidates = append(r.d.Candidates, event("inserted"))
	if _, err := s.List(t.Context(), owner, url.Values{"limit": {"1"}, "cursor": {*list.NextCursor}}); !errors.Is(err, ErrChanged) {
		t.Fatal("inserted event was silently mixed into old pagination", err)
	}
}

package recommendations

import (
	"context"
	"errors"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/sonastea/ticketopia/internal/accounts"
	"github.com/sonastea/ticketopia/internal/models"
)

type repositoryFixture struct {
	item   *Item
	items  []Item
	writes int
	err    error
	query  Query
}

func (r *repositoryFixture) Get(context.Context, string, string) (Item, error) {
	if r.err != nil {
		return Item{}, r.err
	}
	if r.item == nil {
		return Item{}, ErrNotFound
	}
	return *r.item, nil
}
func (r *repositoryFixture) Set(_ context.Context, owner string, d models.EventDetail, reason string) (Item, bool, error) {
	r.writes++
	created := r.item == nil
	if created {
		at := time.Now().UTC()
		r.item = &Item{Event: d.Item, Meta: d.Meta, Profile: accounts.PublicProfile{ID: owner}, RecommendedAt: at, UpdatedAt: at}
	}
	if r.item.Reason != reason {
		r.item.UpdatedAt = time.Now().UTC()
	}
	r.item.Reason = reason
	return *r.item, created, nil
}
func (r *repositoryFixture) Remove(context.Context, string, string) error { r.item = nil; return r.err }
func (r *repositoryFixture) List(_ context.Context, _, _ string, q Query) ([]Item, error) {
	r.query = q
	return r.items, r.err
}
func (r *repositoryFixture) Count(context.Context, string) (int, error) { return len(r.items), r.err }

type detailFixture struct {
	calls  int
	detail models.EventDetail
	err    error
}

func (d *detailFixture) Detail(context.Context, string) (models.EventDetail, error) {
	d.calls++
	return d.detail, d.err
}

func TestRecommendationValidationReplacementAndOfflineEdits(t *testing.T) {
	owner, id := accounts.ID(), "ticketmaster:Case"
	r := &repositoryFixture{}
	d := &detailFixture{detail: models.EventDetail{Item: models.Event{ID: id}, Meta: models.Freshness{DataAsOf: time.Now().UTC()}}}
	s := New(r, d)
	for _, reason := range []string{strings.Repeat("a", 501), strings.Repeat("京", 501), "bad\x00reason", string([]byte{0xff})} {
		if _, _, err := s.Set(t.Context(), owner, id, reason); err == nil {
			t.Fatal("invalid reason accepted")
		}
	}
	if _, _, err := s.Set(t.Context(), "unknown", id, ""); !errors.Is(err, accounts.ErrUnauthenticated) {
		t.Fatal(err)
	}
	if _, _, err := s.Set(t.Context(), owner, "unknown", ""); err == nil {
		t.Fatal("invalid event accepted")
	}
	if d.calls != 0 || r.writes != 0 {
		t.Fatal("invalid writes reached storage/provider")
	}
	item, created, err := s.Set(t.Context(), owner, id, "  A useful\nreason  ")
	if err != nil || !created || item.Reason != "A useful\nreason" || d.calls != 1 {
		t.Fatal(item, created, err)
	}
	first := item.RecommendedAt
	edited := item.UpdatedAt
	d.err = errors.New("provider is gone")
	item, created, err = s.Set(t.Context(), owner, id, "A useful\nreason")
	if err != nil || created || r.writes != 1 || !item.UpdatedAt.Equal(edited) {
		t.Fatal("retry changed existing recommendation", err)
	}
	item, created, err = s.Set(t.Context(), owner, id, strings.Repeat("京", 500))
	if err != nil || created || d.calls != 1 || !item.RecommendedAt.Equal(first) {
		t.Fatal("offline edit failed or bumped publication", err)
	}
	item, _, err = s.Set(t.Context(), owner, id, "")
	if err != nil || item.Reason != "" {
		t.Fatal("PUT omission did not clear optional reason", err)
	}
	r.err = ErrUnavailable
	if _, _, err := s.Set(t.Context(), owner, id, "new"); !errors.Is(err, ErrUnavailable) || d.calls != 1 {
		t.Fatal("storage failure fell through to provider")
	}
	r.err = nil
	r.item = nil
	d.detail.Item.ID = "ticketmaster:Wrong"
	d.err = nil
	if _, _, err := s.Set(t.Context(), owner, id, ""); !errors.Is(err, ErrUnavailable) {
		t.Fatal("mismatched detail published")
	}
}
func TestRecommendationCollectionsRejectCrossScopeCursors(t *testing.T) {
	owner := accounts.ID()
	at := time.Now().UTC()
	r := &repositoryFixture{items: []Item{{Event: models.Event{ID: "ticketmaster:a"}, Profile: accounts.PublicProfile{ID: owner}, RecommendedAt: at}, {Event: models.Event{ID: "ticketmaster:b"}, Profile: accounts.PublicProfile{ID: owner}, RecommendedAt: at}}}
	s := New(r, nil)
	values := url.Values{"limit": {"1"}, "city": {" Berlin "}, "country": {"de"}, "category_id": {"sports"}}
	list, err := s.Community(t.Context(), values)
	if err != nil || len(list.Items) != 1 || list.NextCursor == nil {
		t.Fatal(list, err)
	}
	values.Set("cursor", *list.NextCursor)
	if _, err := s.Community(t.Context(), values); err != nil || r.query.City != "Berlin" || r.query.Country != "DE" {
		t.Fatal(err)
	}
	for key, value := range map[string]string{"city": "Chicago", "country": "US", "category_id": "music"} {
		copy, _ := url.ParseQuery(values.Encode())
		copy.Set(key, value)
		if _, err := s.Community(t.Context(), copy); err == nil {
			t.Fatal("cursor crossed", key)
		}
	}
	for _, scope := range []string{"event:ticketmaster:a", "public:" + owner, "own:" + owner} {
		if _, err := ParseQuery(url.Values{"cursor": {*list.NextCursor}}, scope, false); err == nil {
			t.Fatal("community cursor crossed", scope)
		}
	}
	for _, values := range []url.Values{{"limit": {"0"}}, {"limit": {"101"}}, {"limit": {"1", "2"}}, {"city": {"a\x00b"}}, {"city": {strings.Repeat("京", 121)}}, {"country": {"USA"}}, {"category_id": {"bad/id"}}, {"cursor": {"!"}}, {"cursor": {strings.Repeat("a", 1025)}}, {"owner": {"other"}}} {
		if _, err := s.Community(t.Context(), values); err == nil {
			t.Fatal("invalid query accepted", values)
		}
	}
	r.items = nil
	list, err = s.Event(t.Context(), "ticketmaster:Unknown", nil)
	if err != nil || list.Items == nil || list.Count == nil || *list.Count != 0 {
		t.Fatal("unknown event fabricated or null collection", list, err)
	}
}

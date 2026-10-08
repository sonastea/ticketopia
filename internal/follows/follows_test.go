package follows

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/url"
	"testing"
	"time"

	"github.com/sonastea/ticketopia/internal/accounts"
	"github.com/sonastea/ticketopia/internal/discovery"
	"github.com/sonastea/ticketopia/internal/models"
)

type fixtureRepo struct {
	Repository
	existing *Item
	detail   models.CatalogDetail
	items    []Item
	reads    int
}

func (r *fixtureRepo) Get(context.Context, string, string, string) (Item, error) {
	if r.existing != nil {
		return *r.existing, nil
	}
	return Item{}, ErrNotFound
}
func (r *fixtureRepo) Snapshot(context.Context, string, string) (models.CatalogDetail, error) {
	r.reads++
	if r.detail.Item.ID == "" {
		return models.CatalogDetail{}, ErrNotFound
	}
	return r.detail, nil
}
func (r *fixtureRepo) Follow(_ context.Context, _ string, d models.CatalogDetail) (Item, bool, error) {
	item := Item{Target: d.Item, FollowedAt: time.Now(), Meta: d.Meta}
	r.existing = &item
	return item, true, nil
}
func (r *fixtureRepo) List(context.Context, string, Query) ([]Item, error) { return r.items, nil }

type fixtureDiscovery struct {
	detail models.CatalogDetail
	calls  int
	err    error
}

func (d *fixtureDiscovery) CatalogDetail(context.Context, string, string) (models.CatalogDetail, error) {
	d.calls++
	return d.detail, d.err
}

func TestFollowRetryFallbackIdentityAndOwnershipValidation(t *testing.T) {
	detail := models.CatalogDetail{Item: models.FollowTarget{Kind: "artist", ID: "ticketmaster:One", Name: "One"}, Meta: models.Freshness{DataAsOf: time.Now()}}
	r := &fixtureRepo{detail: detail}
	d := &fixtureDiscovery{err: discovery.ErrNotFound}
	s := New(r, d)
	owner := accounts.ID()
	item, created, err := s.Follow(t.Context(), owner, "artist", detail.Item.ID)
	if err != nil || !created || d.calls != 1 || r.reads != 1 {
		t.Fatal("retained follow failed", err)
	}
	retry, created, err := s.Follow(t.Context(), owner, "artist", detail.Item.ID)
	if err != nil || created || !retry.FollowedAt.Equal(item.FollowedAt) || d.calls != 1 {
		t.Fatal("retry touched provider or timestamp", err)
	}
	if _, _, err := s.Follow(t.Context(), "foreign-owner", "artist", detail.Item.ID); !errors.Is(err, accounts.ErrUnauthenticated) {
		t.Fatal("invalid owner accepted")
	}
	r.existing = nil
	d.err = nil
	d.detail = detail
	d.detail.Item.Kind = "venue"
	if _, _, err := s.Follow(t.Context(), owner, "artist", detail.Item.ID); !errors.Is(err, ErrUnavailable) {
		t.Fatal("wrong provider resource followed", err)
	}
	r.detail = models.CatalogDetail{}
	d.err = discovery.ErrNotFound
	if _, _, err := s.Follow(t.Context(), owner, "artist", detail.Item.ID); !errors.Is(err, ErrNotFound) {
		t.Fatal("missing target not distinguished", err)
	}
}
func TestFollowCursorOwnerKindAndBinaryIdentity(t *testing.T) {
	owner := accounts.ID()
	at := time.Now().UTC()
	r := &fixtureRepo{items: []Item{{Target: models.FollowTarget{Kind: "venue", ID: "ticketmaster:a"}, FollowedAt: at}, {Target: models.FollowTarget{Kind: "artist", ID: "ticketmaster:A"}, FollowedAt: at}}}
	s := New(r, nil)
	list, err := s.List(t.Context(), owner, url.Values{"limit": {"1"}})
	if err != nil || len(list.Items) != 1 || list.NextCursor == nil {
		t.Fatal("pagination failed", err)
	}
	v := url.Values{"limit": {"1"}, "cursor": {*list.NextCursor}}
	q, err := ParseQuery(v, owner)
	if err != nil || q.Cursor.ID != "ticketmaster:a" || q.Cursor.Kind != "venue" {
		t.Fatal("cursor identity lost", err)
	}
	if _, err := ParseQuery(v, accounts.ID()); err == nil {
		t.Fatal("cursor crossed owner")
	}
	v.Set("kind", "artist")
	if _, err := ParseQuery(v, owner); err == nil {
		t.Fatal("cursor crossed filter")
	}
	for _, raw := range []string{"kind=user", "limit=0", "limit=101", "limit=1&limit=2", "owner=other", "cursor=bad"} {
		values, _ := url.ParseQuery(raw)
		if _, err := ParseQuery(values, owner); err == nil {
			t.Fatal("invalid collection query accepted", raw)
		}
	}
	bad, _ := json.Marshal(Cursor{1, owner, "", time.Time{}, "artist", "ticketmaster:a"})
	if _, err := ParseQuery(url.Values{"cursor": {base64.RawURLEncoding.EncodeToString(bad)}}, owner); err == nil {
		t.Fatal("invalid time accepted")
	}
}

package saved

import (
	"context"
	"errors"
	"net/url"
	"testing"
	"time"

	"github.com/sonastea/ticketopia/internal/accounts"
	"github.com/sonastea/ticketopia/internal/discovery"
	"github.com/sonastea/ticketopia/internal/models"
)

type repoStub struct {
	Repository
	existing Item
	getErr   error
	snapshot models.EventDetail
	items    []Item
	writes   int
}

func (r *repoStub) Get(context.Context, string, string) (Item, error) { return r.existing, r.getErr }
func (r *repoStub) Snapshot(context.Context, string) (models.EventDetail, error) {
	if r.snapshot.Item.ID == "" {
		return models.EventDetail{}, ErrNotFound
	}
	return r.snapshot, nil
}
func (r *repoStub) Save(_ context.Context, _ string, d models.EventDetail) (Item, bool, error) {
	r.writes++
	return Item{Event: d.Item}, true, nil
}
func (r *repoStub) List(context.Context, string, Query) ([]Item, error) { return r.items, nil }

type providerStub struct {
	calls  int
	err    error
	detail models.EventDetail
}

func (p *providerStub) Event(context.Context, string) (models.EventDetail, error) {
	p.calls++
	return p.detail, p.err
}

func TestSaveRetriesAndDurableFallback(t *testing.T) {
	owner := accounts.ID()
	event := models.Event{ID: "ticketmaster:Stable", Name: "A show"}
	repo := &repoStub{existing: Item{Event: event, SavedAt: time.Now()}, snapshot: models.EventDetail{Item: event, Meta: models.Freshness{DataAsOf: time.Now()}}}
	provider := &providerStub{err: discovery.ErrNotFound}
	s := New(repo, provider)
	item, created, err := s.Save(t.Context(), owner, event.ID)
	if err != nil || created || item.Event.ID != event.ID || provider.calls != 0 || repo.writes != 0 {
		t.Fatal("retry depended on provider or rewrote save", err)
	}
	repo.getErr = ErrNotFound
	_, created, err = s.Save(t.Context(), owner, event.ID)
	if err != nil || !created || repo.writes != 1 {
		t.Fatal("last-known snapshot could not be saved", err)
	}
	detail, err := s.Detail(t.Context(), event.ID)
	if err != nil || !detail.Meta.Stale {
		t.Fatal("fallback must be stale", err)
	}
	repo.getErr = errors.New("storage failed")
	if _, _, err := s.Save(t.Context(), owner, event.ID); err == nil || repo.writes != 1 {
		t.Fatal("storage failed open")
	}
	if _, _, err := s.Save(t.Context(), "foreign-input", event.ID); !errors.Is(err, accounts.ErrUnauthenticated) {
		t.Fatal("unverified owner", err)
	}
	if _, _, err := s.Save(t.Context(), owner, "ticketmaster:x/evil"); err == nil {
		t.Fatal("invalid event accepted")
	}
}
func TestSavedPaginationValidation(t *testing.T) {
	owner := accounts.ID()
	repo := &repoStub{items: []Item{{Event: models.Event{ID: "ticketmaster:z"}, SavedAt: time.Now().UTC()}, {Event: models.Event{ID: "ticketmaster:a"}, SavedAt: time.Now().UTC()}}}
	s := New(repo, nil)
	list, err := s.List(t.Context(), owner, url.Values{"limit": {"1"}})
	if err != nil || len(list.Items) != 1 || list.NextCursor == nil {
		t.Fatal("no continuation", err)
	}
	q, err := ParseQuery(url.Values{"cursor": {*list.NextCursor}}, owner)
	if err != nil || q.Cursor.ID != "ticketmaster:z" {
		t.Fatal("cursor lost stable tie breaker", err)
	}
	for _, values := range []url.Values{{"limit": {"0"}}, {"limit": {"101"}}, {"limit": {"1", "2"}}, {"owner": {"another"}}, {"cursor": {""}}, {"cursor": {"invalid"}}} {
		if _, err := ParseQuery(values, owner); err == nil {
			t.Fatal("invalid query accepted", values)
		}
	}
	if _, err := ParseQuery(url.Values{"cursor": {*list.NextCursor}}, accounts.ID()); err == nil {
		t.Fatal("cursor usable by another owner")
	}
	repo.items = nil
	list, err = s.List(t.Context(), owner, nil)
	if err != nil || list.Items == nil || list.NextCursor != nil {
		t.Fatal("empty envelope invalid")
	}
}

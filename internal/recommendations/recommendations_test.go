package recommendations

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
)

type repositoryFixture struct {
	item   *Item
	items  []Item
	groups []EventGroup
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
func (r *repositoryFixture) Write(_ context.Context, owner string, d models.EventDetail, w Write) (Item, bool, error) {
	r.writes++
	created := r.item == nil
	if !created && w.KeepExisting {
		return *r.item, false, nil
	}
	if created {
		at := time.Now().UTC()
		r.item = &Item{Event: d.Item, Meta: d.Meta, Profile: accounts.PublicProfile{ID: owner}, RecommendedAt: at, UpdatedAt: at}
	}
	if created || r.item.Reason != w.Reason {
		r.item.Via = w.Via
	}
	if r.item.Reason != w.Reason {
		r.item.UpdatedAt = time.Now().UTC()
	}
	r.item.Reason = w.Reason
	return *r.item, created, nil
}
func (r *repositoryFixture) Remove(context.Context, string, string) error { r.item = nil; return r.err }
func (r *repositoryFixture) WithdrawAttributed(context.Context, string, string) (bool, error) {
	if r.item == nil || r.item.Via == "" || r.item.Reason != "" {
		return false, r.err
	}
	r.item = nil
	return true, r.err
}
func (r *repositoryFixture) List(_ context.Context, _, _ string, q Query) ([]Item, error) {
	r.query = q
	return r.items, r.err
}
func (r *repositoryFixture) EventList(ctx context.Context, event string, q Query) ([]Item, int, error) {
	items, err := r.List(ctx, "", event, q)
	return items, len(r.items), err
}
func (r *repositoryFixture) Community(_ context.Context, q Query) ([]EventGroup, error) {
	r.query = q
	return r.groups, r.err
}

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
	list, err := s.Recent(t.Context(), values)
	if err != nil || len(list.Items) != 1 || list.NextCursor == nil {
		t.Fatal(list, err)
	}
	values.Set("cursor", *list.NextCursor)
	if _, err := s.Recent(t.Context(), values); err != nil || r.query.City != "Berlin" || r.query.Country != "DE" {
		t.Fatal(err)
	}
	for key, value := range map[string]string{"city": "Chicago", "country": "US", "category_id": "music"} {
		copy, _ := url.ParseQuery(values.Encode())
		copy.Set(key, value)
		if _, err := s.Recent(t.Context(), copy); err == nil {
			t.Fatal("cursor crossed", key)
		}
	}
	for _, scope := range []string{"event:ticketmaster:a", "public:" + owner, "own:" + owner} {
		if _, err := ParseQuery(url.Values{"cursor": {*list.NextCursor}}, scope, false); err == nil {
			t.Fatal("community cursor crossed", scope)
		}
	}
	for _, values := range []url.Values{{"limit": {"0"}}, {"limit": {"101"}}, {"limit": {"1", "2"}}, {"city": {"a\x00b"}}, {"city": {strings.Repeat("京", 121)}}, {"country": {"USA"}}, {"category_id": {"bad/id"}}, {"cursor": {"!"}}, {"cursor": {strings.Repeat("a", 1025)}}, {"owner": {"other"}}} {
		if _, err := s.Recent(t.Context(), values); err == nil {
			t.Fatal("invalid query accepted", values)
		}
	}
	r.items = nil
	list, err = s.Event(t.Context(), "ticketmaster:Unknown", nil)
	if err != nil || list.Items == nil || list.Count == nil || *list.Count != 0 {
		t.Fatal("unknown event fabricated or null collection", list, err)
	}
}

func TestGroupedFeedCursorValidationAndPublicObservationPrivacy(t *testing.T) {
	at := time.Now().UTC()
	r := &repositoryFixture{groups: []EventGroup{{Event: models.Event{ID: "ticketmaster:a"}, FirstRecommendedAt: at}, {Event: models.Event{ID: "ticketmaster:b"}, FirstRecommendedAt: at}}}
	s := New(r, nil)
	values := url.Values{"limit": {"1"}, "city": {" Berlin "}, "country": {"de"}, "category_id": {"sports"}}
	feed, err := s.Community(t.Context(), values)
	if err != nil || len(feed.Items) != 1 || feed.NextCursor == nil {
		t.Fatal(feed, err)
	}
	values.Set("cursor", *feed.NextCursor)
	if _, err := s.Community(t.Context(), values); err != nil || r.query.FeedCursor.EventID != "ticketmaster:a" || !r.query.FeedCursor.Before.Equal(at) || r.query.City != "Berlin" || r.query.Country != "DE" {
		t.Fatal(r.query, err)
	}
	for key, value := range map[string]string{"city": "Chicago", "country": "US", "category_id": "music"} {
		copy, _ := url.ParseQuery(values.Encode())
		copy.Set(key, value)
		if _, err := s.Community(t.Context(), copy); err == nil {
			t.Fatal("grouped cursor crossed", key)
		}
	}
	if _, err := s.Recent(t.Context(), values); err == nil {
		t.Fatal("grouped cursor used in individual feed")
	}
	for _, values := range []url.Values{{"cursor": {}}, {"cursor": {"", ""}}, {"cursor": {"!"}}, {"limit": {"0"}}, {"country": {"USA"}}, {"owner": {"other"}}} {
		if _, err := s.Community(t.Context(), values); err == nil {
			t.Fatal("invalid grouped query accepted", values)
		}
	}
	r.items = []Item{{Event: models.Event{ID: "ticketmaster:a"}, Profile: accounts.PublicProfile{ID: accounts.ID()}, RecommendedAt: at}, {Event: models.Event{ID: "ticketmaster:b"}, Profile: accounts.PublicProfile{ID: accounts.ID()}, RecommendedAt: at}}
	individual, err := s.Recent(t.Context(), url.Values{"limit": {"1"}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Community(t.Context(), url.Values{"cursor": {*individual.NextCursor}}); err == nil {
		t.Fatal("individual cursor used in grouped feed")
	}
	r.groups = nil
	feed, err = s.Community(t.Context(), nil)
	if err != nil || feed.Items == nil || feed.NextCursor != nil {
		t.Fatal("empty grouped collection", feed, err)
	}
	r.err = ErrUnavailable
	if _, err = s.Community(t.Context(), nil); !errors.Is(err, ErrUnavailable) {
		t.Fatal("outage fabricated empty grouped collection", err)
	}
	data, err := json.Marshal(Item{Observation: &Observation{NewPublications: 123, Reactivations: 456}})
	if err != nil || strings.Contains(string(data), "Observation") || strings.Contains(string(data), "publications") || strings.Contains(string(data), "reactivations") {
		t.Fatal("operator data leaked", string(data), err)
	}
}

// Recommending without a reason never erases an existing one: an agent's
// KeepExisting publication returns the person's recommendation unchanged,
// and new agent publications carry the app's name.
func TestPublishKeepsExistingAndAttributes(t *testing.T) {
	owner, id := accounts.ID(), "ticketmaster:Keep"
	r := &repositoryFixture{}
	d := &detailFixture{detail: models.EventDetail{Item: models.Event{ID: id}, Meta: models.Freshness{DataAsOf: time.Now().UTC()}}}
	s := New(r, d)
	if _, _, err := s.Set(t.Context(), owner, id, "My own words"); err != nil {
		t.Fatal(err)
	}
	writes := r.writes
	item, created, err := s.Publish(t.Context(), owner, id, Write{Via: "Agent", KeepExisting: true})
	if err != nil || created || item.Reason != "My own words" || item.Via != "" || r.writes != writes {
		t.Fatalf("kept: %+v created=%v writes=%d err=%v", item, created, r.writes, err)
	}
	r.item = nil
	item, created, err = s.Publish(t.Context(), owner, id, Write{Via: "Agent", KeepExisting: true})
	if err != nil || !created || item.Reason != "" || item.Via != "Agent" {
		t.Fatalf("agent publication: %+v created=%v err=%v", item, created, err)
	}
	if _, _, err := s.Publish(t.Context(), owner, id, Write{Via: strings.Repeat("x", MaxVia+1)}); err == nil {
		t.Fatal("overlong app name accepted")
	}
	// The JSON carries via only when set.
	data, _ := json.Marshal(Item{})
	if strings.Contains(string(data), `"via"`) {
		t.Fatalf("empty via serialized: %s", data)
	}
	data, _ = json.Marshal(item)
	if !strings.Contains(string(data), `"via":"Agent"`) {
		t.Fatalf("via missing: %s", data)
	}
}

// Agents withdraw only what an app published without a reason; the person's
// own recommendations and reasons are never erased by an agent.
func TestWithdrawForAgentKeepsThePersonsWords(t *testing.T) {
	owner, id := accounts.ID(), "ticketmaster:Withdraw"
	r := &repositoryFixture{}
	s := New(r, nil)
	if err := s.WithdrawForAgent(t.Context(), owner, id); err != nil {
		t.Fatalf("missing recommendation: %v", err)
	}
	for _, item := range []Item{{Reason: "Mine"}, {}, {Reason: "Drafted", Via: "Agent"}} {
		r.item = &item
		if err := s.WithdrawForAgent(t.Context(), owner, id); !errors.Is(err, ErrPersonal) || r.item == nil {
			t.Fatalf("%+v: %v", item, err)
		}
	}
	r.item = &Item{Via: "Agent"}
	if err := s.WithdrawForAgent(t.Context(), owner, id); err != nil || r.item != nil {
		t.Fatalf("agent publication: %v", err)
	}
}

// A person's review must still hold: a changed or vanished recommendation is
// refused before writing. A draft publication always reaches the repository
// (which consumes the draft), even when the reason is unchanged.
func TestPublishChecksTheReviewAndAlwaysConsumesDrafts(t *testing.T) {
	owner, id := accounts.ID(), "ticketmaster:Review"
	r := &repositoryFixture{}
	d := &detailFixture{detail: models.EventDetail{Item: models.Event{ID: id}, Meta: models.Freshness{DataAsOf: time.Now().UTC()}}}
	s := New(r, d)
	if _, _, err := s.Publish(t.Context(), owner, id, Write{Reason: "x", Expect: &Current{Active: true, ReasonHash: ReasonHash("")}}); !errors.Is(err, ErrChanged) || r.writes != 0 {
		t.Fatalf("expected an existing recommendation: %v", err)
	}
	s.Set(t.Context(), owner, id, "Same")
	writes := r.writes
	for _, expect := range []Current{{Active: false}, {Active: true, ReasonHash: ReasonHash("Other")}} {
		if _, _, err := s.Publish(t.Context(), owner, id, Write{Reason: "New", Expect: &expect}); !errors.Is(err, ErrChanged) || r.writes != writes {
			t.Fatalf("%+v: %v", expect, err)
		}
	}
	if _, _, err := s.Publish(t.Context(), owner, id, Write{Reason: "Same", Via: "Agent", ViaHost: "agent.example", Draft: "draft", Expect: &Current{Active: true, ReasonHash: ReasonHash("Same")}}); err != nil || r.writes != writes+1 {
		t.Fatalf("unchanged draft publication skipped the repository: %v writes=%d", err, r.writes)
	}
	if _, _, err := s.Publish(t.Context(), owner, id, Write{ViaHost: "agent.example"}); err == nil {
		t.Fatal("host without an app name accepted")
	}
}

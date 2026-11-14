package appmcp

import (
	"context"
	"encoding/json"
	"errors"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/sonastea/ticketopia/internal/accounts"
	"github.com/sonastea/ticketopia/internal/discovery"
	"github.com/sonastea/ticketopia/internal/follows"
	"github.com/sonastea/ticketopia/internal/interests"
	"github.com/sonastea/ticketopia/internal/models"
	"github.com/sonastea/ticketopia/internal/radar"
)

type fakeRadar struct{ *fakeAccount }

func (f fakeRadar) List(_ context.Context, owner string, v url.Values) (radar.List, error) {
	f.record("radar.List " + owner + " " + v.Encode())
	return radar.List{Items: []radar.Item{}, Mode: "personal"}, nil
}

type fakeFollows struct{ *fakeAccount }

func (f fakeFollows) List(_ context.Context, owner string, v url.Values) (follows.List, error) {
	f.record("follows.List " + owner + " " + v.Encode())
	return follows.List{Items: []follows.Item{}}, nil
}
func (f fakeFollows) Follow(_ context.Context, owner, kind, id string) (follows.Item, bool, error) {
	f.record("follows.Follow " + owner + " " + kind + " " + id)
	if err := follows.Validate(kind, id); err != nil {
		return follows.Item{}, false, err
	}
	return follows.Item{Target: models.FollowTarget{Kind: kind, ID: id, Name: "Band"}, FollowedAt: time.Now()}, true, nil
}
func (f fakeFollows) Remove(_ context.Context, owner, kind, id string) error {
	f.record("follows.Remove " + owner + " " + kind + " " + id)
	return nil
}

type fakeCatalog struct{ *fakeAccount }

func (f fakeCatalog) Catalog(_ context.Context, kind string, v url.Values) (models.CatalogList, error) {
	f.record("catalog " + kind + " " + v.Encode())
	if _, err := discovery.ParseCatalogQuery(kind, v); err != nil {
		return models.CatalogList{}, err
	}
	return models.CatalogList{Items: []models.FollowTarget{{Kind: kind, ID: "ticketmaster:K8", Name: "Band"}}, Total: 1}, nil
}

type fakeInterests struct{ *fakeAccount }

func (f fakeInterests) List(_ context.Context, owner string, public bool, v url.Values) (interests.List, error) {
	f.record("interests.List " + owner + " " + v.Encode())
	if public {
		f.record("interests.List public")
	}
	return interests.List{Items: []interests.Item{}}, nil
}
func (f fakeInterests) Set(_ context.Context, owner, id string, visibility *string, fallback string) (interests.Item, bool, error) {
	chosen := fallback
	if visibility != nil {
		chosen = *visibility
	}
	f.record("interests.Set " + owner + " " + id + " " + chosen)
	return interests.Item{Event: models.Event{ID: id}, Visibility: chosen, InterestedAt: time.Now()}, true, nil
}
func (f fakeInterests) Remove(_ context.Context, owner, id string) error {
	f.record("interests.Remove " + owner + " " + id)
	return nil
}

type fakeAccounts struct{ *fakeAccount }

func (f fakeAccounts) Account(_ context.Context, id string) (accounts.Account, error) {
	f.record("accounts.Account " + id)
	a := f.account
	a.ID = id
	return a, f.accountErr
}

// The radar, follow and Interested tools act only for the token's account,
// with the API's query rules; anonymous calls reach no service.
func TestPersonalToolsUseOnlyTheTokenAccount(t *testing.T) {
	f := &fakeAccount{account: accounts.Account{InterestVisibility: "public"}}
	base := privateServer(t, f).URL
	event := map[string]any{"event_id": "ticketmaster:x"}
	target := map[string]any{"kind": "artist", "target_id": "ticketmaster:K8"}
	calls := map[string]map[string]any{"my_radar": {}, "find_artists": {"keyword": "band"}, "find_venues": {"keyword": "hall"},
		"my_follows": {}, "follow_artist_or_venue": target, "unfollow_artist_or_venue": target,
		"my_interested_events": {}, "mark_interested": event, "unmark_interested": event}
	for name, args := range calls {
		reply := tool(t, base, "", name, args)
		if !reply.Result.IsError || !strings.Contains(reply.Result.Content[0].Text, "sign in required") {
			t.Fatalf("%s without token: %+v", name, reply.Result)
		}
	}
	if len(f.calls) != 0 {
		t.Fatalf("anonymous calls reached services: %v", f.calls)
	}

	steps := []struct {
		name string
		args map[string]any
		want string
	}{
		{"my_radar", map[string]any{"limit": 5}, "radar.List owner-id limit=5"},
		{"find_artists", map[string]any{"keyword": "band", "limit": 3}, "catalog artist keyword=band&limit=3"},
		{"find_venues", map[string]any{"keyword": "hall", "city": "Chicago", "country": "US"}, "catalog venue city=Chicago&country=US&keyword=hall"},
		{"my_follows", map[string]any{"kind": "venue"}, "follows.List owner-id kind=venue"},
		{"follow_artist_or_venue", target, "follows.Follow owner-id artist ticketmaster:K8"},
		{"unfollow_artist_or_venue", target, "follows.Remove owner-id artist ticketmaster:K8"},
		{"my_interested_events", map[string]any{}, "interests.List owner-id "},
		// New choices are private even when the account defaults to public.
		{"mark_interested", event, "interests.Set owner-id ticketmaster:x private"},
		{"unmark_interested", event, "interests.Remove owner-id ticketmaster:x"},
	}
	for _, step := range steps {
		f.calls = nil
		reply := tool(t, base, "owner-token", step.name, step.args)
		if reply.Result.IsError || !strings.Contains(strings.Join(f.calls, "|"), step.want) {
			t.Fatalf("%s: calls %v, want %q; result %+v", step.name, f.calls, step.want, reply.Result)
		}
		if strings.Contains(strings.Join(f.calls, "|"), "public") && strings.Contains(step.want, "List") {
			t.Fatalf("%s read the public list", step.name)
		}
	}
	// An agent cannot publish a choice, even by sending a visibility.
	f.calls = nil
	tool(t, base, "owner-token", "mark_interested", map[string]any{"event_id": "ticketmaster:x", "visibility": "public"})
	if strings.Contains(strings.Join(f.calls, "|"), "public") {
		t.Fatalf("agent published a choice: %v", f.calls)
	}
	// The resulting state is returned, so agents need not re-read it.
	reply := tool(t, base, "owner-token", "follow_artist_or_venue", target)
	var state struct {
		Following, Created bool
		Item               struct{ Target struct{ Name string } }
	}
	if json.Unmarshal(reply.Result.StructuredContent, &state) != nil || !state.Following || !state.Created || state.Item.Target.Name != "Band" {
		t.Fatalf("follow state: %s", reply.Result.StructuredContent)
	}
}

// Catalog searches use the catalog API's wording; follow errors use the
// account API's.
func TestPersonalToolErrors(t *testing.T) {
	f := &fakeAccount{}
	base := privateServer(t, f).URL
	if reply := tool(t, base, "owner-token", "find_artists", map[string]any{"keyword": ""}); !reply.Result.IsError ||
		!strings.HasPrefix(reply.Result.Content[0].Text, "catalog: ") {
		t.Fatalf("empty keyword: %+v", reply.Result)
	}
	if reply := tool(t, base, "owner-token", "follow_artist_or_venue", map[string]any{"kind": "band", "target_id": "ticketmaster:K8"}); !reply.Result.IsError ||
		!strings.HasPrefix(reply.Result.Content[0].Text, "private: ") {
		t.Fatalf("bad kind: %+v", reply.Result)
	}
}

func TestReadOnlyTokensCannotFollowOrMarkInterested(t *testing.T) {
	f := &fakeAccount{}
	base := privateServer(t, f).URL
	event := map[string]any{"event_id": "ticketmaster:x"}
	target := map[string]any{"kind": "venue", "target_id": "ticketmaster:K8"}
	for name, args := range map[string]map[string]any{"follow_artist_or_venue": target, "unfollow_artist_or_venue": target,
		"mark_interested": event, "unmark_interested": event} {
		if reply := tool(t, base, "reader-token", name, args); !reply.Result.IsError || !strings.Contains(reply.Result.Content[0].Text, "read-only") {
			t.Fatalf("%s with read-only token: %+v", name, reply.Result)
		}
	}
	if len(f.calls) != 0 {
		t.Fatalf("read-only writes reached services: %v", f.calls)
	}
	for _, name := range []string{"my_radar", "my_follows", "my_interested_events"} {
		if reply := tool(t, base, "reader-token", name, map[string]any{}); reply.Result.IsError {
			t.Fatalf("read-only %s: %+v", name, reply.Result)
		}
	}
}

// Signed in, a search without city, country or keyword uses the account's
// saved city and says so; keyword searches, explicit locations, anonymous
// callers and accounts without a city search as before.
func TestFindEventsDefaultsToTheAccountCity(t *testing.T) {
	f := &fakeAccount{account: accounts.Account{Preferences: accounts.Preferences{City: "Chicago", Country: "US"}}}
	base := privateServer(t, f).URL
	search := func(token string, args map[string]any) (string, map[string]any) {
		t.Helper()
		f.events.query = discovery.Query{}
		reply := tool(t, base, token, "find_events", args)
		if reply.Result.IsError {
			t.Fatalf("find_events %v: %+v", args, reply.Result)
		}
		var out map[string]any
		_ = json.Unmarshal(reply.Result.StructuredContent, &out)
		if _, ok := out["items"]; !ok {
			t.Fatalf("event list fields missing: %s", reply.Result.StructuredContent)
		}
		return f.events.query.City + "/" + f.events.query.Country, out
	}
	city, out := search("owner-token", map[string]any{"category_id": "KZFzniwnSyZfZ7v7nJ"})
	location, _ := out["default_location"].(map[string]any)
	if city != "Chicago/US" || location["city"] != "Chicago" || location["source"] != "account" {
		t.Fatalf("default: %s %v", city, out["default_location"])
	}
	for name, c := range map[string]struct {
		token string
		args  map[string]any
		want  string
	}{
		"keyword searches everywhere": {"owner-token", map[string]any{"keyword": "band"}, "/"},
		"explicit city":               {"owner-token", map[string]any{"city": "Denver", "country": "US"}, "Denver/US"},
		"country only":                {"owner-token", map[string]any{"country": "CA"}, "/CA"},
		"anonymous":                   {"", map[string]any{}, "/"},
	} {
		got, out := search(c.token, c.args)
		if got != c.want || out["default_location"] != nil {
			t.Errorf("%s: searched %q, default %v", name, got, out["default_location"])
		}
	}
	f.account.Preferences.City = ""
	if got, out := search("owner-token", map[string]any{}); got != "/" || out["default_location"] != nil {
		t.Errorf("no saved city: %q %v", got, out["default_location"])
	}
	f.account.Preferences.City, f.accountErr = "Chicago", errors.New("down")
	if got, _ := search("owner-token", map[string]any{}); got != "/" {
		t.Errorf("account unavailable still searched %q", got)
	}
}

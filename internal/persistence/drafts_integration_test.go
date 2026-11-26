package persistence

import (
	"errors"
	"fmt"
	"net/url"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/sonastea/ticketopia/internal/accounts"
	"github.com/sonastea/ticketopia/internal/drafts"
	"github.com/sonastea/ticketopia/internal/oauth"
	"github.com/sonastea/ticketopia/internal/recommendations"
)

// Drafts are per account, capped under the account lock even across
// replicas, idempotent for identical text, and expire after seven days.
func TestMariaDBDraftsCapExpiryAndOwnership(t *testing.T) {
	f := newMaria(t)
	f.migrate(t)
	pools := []*Pool{f.open(t), f.open(t), f.open(t)}
	auth := accounts.New(pools[0].Accounts(), accountProviderFixture{})
	owner, _ := loginAccount(t, auth, "draft-owner")
	other, _ := loginAccount(t, auth, "draft-other")
	detail := savedDetail("DraftCase")
	now := time.Now().UTC().Truncate(time.Microsecond)
	draft := func(text string, at time.Time) drafts.Draft {
		return drafts.Draft{ID: fmt.Sprintf("%032x", time.Now().UnixNano()), EventID: detail.Item.ID, Kind: drafts.KindRecommendation, Text: text,
			ClientName: "Agent", CreatedAt: at, ExpiresAt: at.Add(drafts.Lifetime)}
	}

	// Concurrent creates on three replicas never exceed the cap.
	var wg sync.WaitGroup
	var mu sync.Mutex
	limited := 0
	for i := range drafts.MaxPending + 5 {
		wg.Go(func() {
			d := draft(fmt.Sprintf("Reason %d", i), now)
			d.ID = fmt.Sprintf("%032x", i+1)
			_, err := pools[i%3].Drafts().Create(t.Context(), &detail, d, owner.ID, now)
			if errors.Is(err, drafts.ErrLimit) {
				mu.Lock()
				limited++
				mu.Unlock()
			} else if err != nil {
				t.Error(err)
			}
		})
	}
	wg.Wait()
	if count, err := pools[0].Drafts().Count(t.Context(), owner.ID, now); err != nil || count != drafts.MaxPending || limited != 5 {
		t.Fatalf("count %d limited %d err %v", count, limited, err)
	}
	// Identical text is the same draft, even at the cap; any stored one will do,
	// since which creates won the race varies.
	pending, err := pools[0].Drafts().List(t.Context(), owner.ID, now)
	if err != nil || len(pending) == 0 {
		t.Fatal(err)
	}
	same := draft(pending[0].Text, now)
	same.ID = fmt.Sprintf("%032x", 999)
	if got, err := pools[1].Drafts().Create(t.Context(), &detail, same, owner.ID, now); err != nil || got.ID != pending[0].ID || got.Event.ID != detail.Item.ID {
		t.Fatalf("identical draft: %+v %v", got, err)
	}

	// Drafts are per account.
	list, err := pools[2].Drafts().List(t.Context(), owner.ID, now)
	if err != nil || len(list) != drafts.MaxPending || list[0].Event.Name == "" {
		t.Fatalf("owner list: %d %v", len(list), err)
	}
	if list, _ := pools[2].Drafts().List(t.Context(), other.ID, now); len(list) != 0 {
		t.Fatalf("other sees drafts: %+v", list)
	}
	if _, err := pools[0].Drafts().Get(t.Context(), other.ID, list[0].ID, now); !errors.Is(err, drafts.ErrNotFound) {
		t.Fatalf("other read a draft: %v", err)
	}
	if deleted, err := pools[0].Drafts().Delete(t.Context(), other.ID, list[0].ID); err != nil || deleted {
		t.Fatalf("other deleting a draft: %v %v", deleted, err)
	}
	if _, err := pools[0].Drafts().Get(t.Context(), owner.ID, list[0].ID, now); err != nil {
		t.Fatalf("other deleted a draft: %v", err)
	}

	// After seven days they are gone, and creating another cleans them up.
	later := now.Add(drafts.Lifetime + time.Minute)
	if count, _ := pools[0].Drafts().Count(t.Context(), owner.ID, later); count != 0 {
		t.Fatalf("expired drafts counted: %d", count)
	}
	if _, err := pools[0].Drafts().Get(t.Context(), owner.ID, list[0].ID, later); !errors.Is(err, drafts.ErrNotFound) {
		t.Fatalf("expired draft read: %v", err)
	}
	fresh := draft("After expiry", later)
	if _, err := pools[0].Drafts().Create(t.Context(), &detail, fresh, owner.ID, later); err != nil {
		t.Fatal(err)
	}
	var stored int
	if err := f.admin.QueryRowContext(t.Context(), "SELECT COUNT(*) FROM "+f.runtime.Database+".agent_drafts WHERE account_id=?", owner.ID).Scan(&stored); err != nil || stored != 1 {
		t.Fatalf("expired rows kept: %d %v", stored, err)
	}
}

// Attribution is stored with the content it describes: an agent's
// publication and a published draft carry the app, the person's own edits and
// withdrawals clear it, and KeepExisting never overwrites a reason.
func TestMariaDBRecommendationAttribution(t *testing.T) {
	f := newMaria(t)
	f.migrate(t)
	p := f.open(t)
	auth := accounts.New(p.Accounts(), accountProviderFixture{})
	owner, _ := loginAccount(t, auth, "via-owner")
	detail := savedDetail("ViaCase")
	repo := p.Recommendations()
	item, created, err := repo.Write(t.Context(), owner.ID, detail, recommendations.Write{Via: "Agent", ViaHost: "agent.example", KeepExisting: true})
	if err != nil || !created || item.Via != "Agent" || item.ViaHost != "agent.example" || item.Reason != "" {
		t.Fatalf("agent publication: %+v %v %v", item, created, err)
	}
	if item, _, _ = repo.Write(t.Context(), owner.ID, detail, recommendations.Write{Reason: "My words"}); item.Via != "" || item.ViaHost != "" || item.Reason != "My words" {
		t.Fatalf("own edit kept via: %+v", item)
	}
	if item, created, _ = repo.Write(t.Context(), owner.ID, detail, recommendations.Write{Via: "Agent", KeepExisting: true}); created || item.Reason != "My words" || item.Via != "" {
		t.Fatalf("KeepExisting changed the recommendation: %+v", item)
	}
	if item, _, _ = repo.Write(t.Context(), owner.ID, detail, recommendations.Write{Reason: "Drafted words", Via: "Agent", ViaHost: "agent.example"}); item.Via != "Agent" || item.ViaHost != "agent.example" {
		t.Fatalf("published draft lacks via: %+v", item)
	}
	// Every read projection carries it.
	service := recommendations.New(repo, nil)
	for name, read := range map[string]func() ([]recommendations.Item, error){
		"own": func() ([]recommendations.Item, error) {
			l, err := service.Own(t.Context(), owner.ID, url.Values{})
			return l.Items, err
		},
		"event": func() ([]recommendations.Item, error) {
			l, err := service.Event(t.Context(), detail.Item.ID, url.Values{})
			return l.Items, err
		},
		"community": func() ([]recommendations.Item, error) {
			feed, err := service.Community(t.Context(), url.Values{})
			if err != nil || len(feed.Items) == 0 {
				return nil, err
			}
			return feed.Items[0].Recommendations, nil
		},
	} {
		items, err := read()
		if err != nil || len(items) != 1 || items[0].Via != "Agent" || items[0].ViaHost != "agent.example" {
			t.Fatalf("%s projection: %+v %v", name, items, err)
		}
	}
	// An agent cannot withdraw a recommendation with a reason, even its own
	// draft's, nor one the person published.
	if withdrawn, err := repo.WithdrawAttributed(t.Context(), owner.ID, detail.Item.ID); err != nil || withdrawn {
		t.Fatalf("withdrew a reasoned recommendation: %v %v", withdrawn, err)
	}
	if item, err := repo.Get(t.Context(), owner.ID, detail.Item.ID); err != nil || item.Reason != "Drafted words" {
		t.Fatalf("reason lost: %+v %v", item, err)
	}
	if err := repo.Remove(t.Context(), owner.ID, detail.Item.ID); err != nil {
		t.Fatal(err)
	}
	if item, _, _ = repo.Write(t.Context(), owner.ID, detail, recommendations.Write{}); item.Via != "" {
		t.Fatalf("withdrawal kept via: %+v", item)
	}
	if withdrawn, _ := repo.WithdrawAttributed(t.Context(), owner.ID, detail.Item.ID); withdrawn {
		t.Fatal("withdrew the person's own recommendation")
	}
	repo.Remove(t.Context(), owner.ID, detail.Item.ID)
	repo.Write(t.Context(), owner.ID, detail, recommendations.Write{Via: "Agent"})
	if withdrawn, err := repo.WithdrawAttributed(t.Context(), owner.ID, detail.Item.ID); err != nil || !withdrawn {
		t.Fatalf("agent publication not withdrawn: %v %v", withdrawn, err)
	}
}

// A connection's drafts go with it however it is removed (disconnect, sign
// out everywhere, token revocation), leaving other connections' drafts and
// API-token drafts alone.
func TestMariaDBDraftsFollowTheirConnection(t *testing.T) {
	f := newMaria(t)
	f.migrate(t)
	p := f.open(t)
	auth := accounts.New(p.Accounts(), accountProviderFixture{})
	owner, _ := loginAccount(t, auth, "draft-grant-owner")
	detail := savedDetail("DraftGrantCase")
	now := time.Now().UTC().Truncate(time.Microsecond)
	grant := func(n int) oauth.Grant {
		g := oauth.Grant{ID: fmt.Sprintf("%032x", n), AccountID: owner.ID, ClientID: "https://agent.example/client.json", ClientName: "Agent",
			Scopes: []string{"read", "write"}, CreatedAt: now, ExpiresAt: now.Add(time.Hour), AccessHash: oauth.Hash(fmt.Sprint("access", n)), AccessUntil: now.Add(time.Hour)}
		if err := p.OAuth().CreateGrant(t.Context(), g, now); err != nil {
			t.Fatal(err)
		}
		return g
	}
	draft := func(n int, grant string) {
		t.Helper()
		d := drafts.Draft{ID: fmt.Sprintf("%032x", n), EventID: detail.Item.ID, Kind: drafts.KindRecommendation, Text: fmt.Sprint("Reason ", n),
			ClientName: "Agent", ClientHost: "agent.example", GrantID: grant, CreatedAt: now, ExpiresAt: now.Add(drafts.Lifetime)}
		if grant == "" {
			d.ClientName, d.ClientHost = drafts.APIClientName, ""
		}
		if _, err := p.Drafts().Create(t.Context(), &detail, d, owner.ID, now); err != nil {
			t.Fatal(err)
		}
	}
	remaining := func() []string {
		list, err := p.Drafts().List(t.Context(), owner.ID, now)
		if err != nil {
			t.Fatal(err)
		}
		texts := []string{}
		for _, d := range list {
			texts = append(texts, d.Text)
		}
		slices.Sort(texts)
		return texts
	}
	first, second, third := grant(1), grant(2), grant(3)
	draft(1, first.ID)
	draft(2, second.ID)
	draft(3, third.ID)
	draft(4, "")
	if err := p.OAuth().RevokeGrant(t.Context(), owner.ID, first.ID); err != nil {
		t.Fatal(err)
	}
	if got := remaining(); !slices.Equal(got, []string{"Reason 2", "Reason 3", "Reason 4"}) {
		t.Fatalf("after disconnect: %v", got)
	}
	if _, err := p.OAuth().RevokeToken(t.Context(), second.AccessHash, false, second.ClientID); err != nil {
		t.Fatal(err)
	}
	if got := remaining(); !slices.Equal(got, []string{"Reason 3", "Reason 4"}) {
		t.Fatalf("after token revocation: %v", got)
	}
	if err := p.OAuth().RevokeAll(t.Context(), owner.ID); err != nil {
		t.Fatal(err)
	}
	if got := remaining(); !slices.Equal(got, []string{"Reason 4"}) {
		t.Fatalf("after sign out everywhere: %v", got)
	}
	// A draft for a connection already removed is refused, not orphaned.
	d := drafts.Draft{ID: fmt.Sprintf("%032x", 9), EventID: detail.Item.ID, Kind: drafts.KindRecommendation, Text: "Late", ClientName: "Agent",
		ClientHost: "agent.example", GrantID: first.ID, CreatedAt: now, ExpiresAt: now.Add(drafts.Lifetime)}
	if _, err := p.Drafts().Create(t.Context(), &detail, d, owner.ID, now); err == nil {
		t.Fatal("draft stored for a removed connection")
	}
}

// Publishing a draft consumes it in the recommendation's transaction: across
// replicas exactly one publication wins, and a stale review publishes
// nothing and keeps the draft.
func TestMariaDBDraftPublicationIsTransactional(t *testing.T) {
	f := newMaria(t)
	f.migrate(t)
	pools := []*Pool{f.open(t), f.open(t), f.open(t)}
	auth := accounts.New(pools[0].Accounts(), accountProviderFixture{})
	owner, _ := loginAccount(t, auth, "draft-publish-owner")
	detail := savedDetail("DraftPublishCase")
	now := time.Now().UTC().Truncate(time.Microsecond)
	id := fmt.Sprintf("%032x", 7)
	if _, err := pools[0].Drafts().Create(t.Context(), &detail, drafts.Draft{ID: id, EventID: detail.Item.ID, Kind: drafts.KindRecommendation, Text: "Drafted",
		ClientName: "an API client", CreatedAt: now, ExpiresAt: now.Add(drafts.Lifetime)}, owner.ID, now); err != nil {
		t.Fatal(err)
	}
	if _, _, err := pools[0].Recommendations().Set(t.Context(), owner.ID, detail, "Mine"); err != nil {
		t.Fatal(err)
	}
	write := recommendations.Write{Reason: "Drafted", Via: "an API client", Draft: id}
	stale := write
	stale.Expect = &recommendations.Current{Active: true, ReasonHash: recommendations.ReasonHash("Not what is there")}
	if _, _, err := pools[1].Recommendations().Write(t.Context(), owner.ID, detail, stale); !errors.Is(err, recommendations.ErrChanged) {
		t.Fatalf("stale review: %v", err)
	}
	if _, err := pools[0].Drafts().Get(t.Context(), owner.ID, id, now); err != nil {
		t.Fatalf("stale review consumed the draft: %v", err)
	}
	write.Expect = &recommendations.Current{Active: true, ReasonHash: recommendations.ReasonHash("Mine")}
	// An agent publishing never replaces the person's own reason, even when
	// its review matches: checked under the row lock, keeping the draft.
	agent := write
	agent.KeepPersonal = true
	if _, _, err := pools[2].Recommendations().Write(t.Context(), owner.ID, detail, agent); !errors.Is(err, recommendations.ErrPersonal) {
		t.Fatalf("agent replaced the person's reason: %v", err)
	}
	if _, err := pools[0].Drafts().Get(t.Context(), owner.ID, id, now); err != nil {
		t.Fatalf("refused agent publication consumed the draft: %v", err)
	}
	var wg sync.WaitGroup
	var mu sync.Mutex
	published, gone := 0, 0
	for i := range 6 {
		wg.Go(func() {
			_, _, err := pools[i%3].Recommendations().Write(t.Context(), owner.ID, detail, write)
			mu.Lock()
			defer mu.Unlock()
			switch {
			case err == nil:
				published++
			case errors.Is(err, recommendations.ErrDraftGone), errors.Is(err, recommendations.ErrChanged):
				gone++
			default:
				t.Error(err)
			}
		})
	}
	wg.Wait()
	item, err := pools[0].Recommendations().Get(t.Context(), owner.ID, detail.Item.ID)
	if published != 1 || gone != 5 || err != nil || item.Reason != "Drafted" || item.Via != "an API client" || item.ViaHost != "" {
		t.Fatalf("published %d gone %d item %+v %v", published, gone, item, err)
	}
	if _, err := pools[0].Drafts().Get(t.Context(), owner.ID, id, now); !errors.Is(err, drafts.ErrNotFound) {
		t.Fatalf("draft kept: %v", err)
	}
}

// Publishing a connection's draft while the person signs out everywhere never
// deadlocks: both lock the account before the draft, and sign-out always
// succeeds, removing the draft unless the publication consumed it first.
func TestMariaDBDraftPublicationRacingSignOutEverywhere(t *testing.T) {
	f := newMaria(t)
	f.migrate(t)
	pools := []*Pool{f.open(t), f.open(t)}
	auth := accounts.New(pools[0].Accounts(), accountProviderFixture{})
	owner, _ := loginAccount(t, auth, "draft-race-owner")
	for i := range 12 {
		detail := savedDetail(fmt.Sprint("DraftRace", i))
		now := time.Now().UTC().Truncate(time.Microsecond)
		g := oauth.Grant{ID: fmt.Sprintf("%032x", i+1), AccountID: owner.ID, ClientID: "https://agent.example/client.json", ClientName: "Agent",
			Scopes: []string{"read", "write"}, CreatedAt: now, ExpiresAt: now.Add(time.Hour), AccessHash: oauth.Hash(fmt.Sprint("race", i)), AccessUntil: now.Add(time.Hour)}
		if err := pools[0].OAuth().CreateGrant(t.Context(), g, now); err != nil {
			t.Fatal(err)
		}
		id := fmt.Sprintf("%032x", 100+i)
		if _, err := pools[0].Drafts().Create(t.Context(), &detail, drafts.Draft{ID: id, EventID: detail.Item.ID, Kind: drafts.KindRecommendation, Text: "Race",
			ClientName: "Agent", ClientHost: "agent.example", GrantID: g.ID, CreatedAt: now, ExpiresAt: now.Add(drafts.Lifetime)}, owner.ID, now); err != nil {
			t.Fatal(err)
		}
		var wg sync.WaitGroup
		var publishErr, revokeErr error
		wg.Go(func() {
			_, _, publishErr = pools[0].Recommendations().Write(t.Context(), owner.ID, detail, recommendations.Write{Reason: "Race", Via: "Agent", ViaHost: "agent.example", Draft: id})
		})
		wg.Go(func() { revokeErr = pools[1].OAuth().RevokeAll(t.Context(), owner.ID) })
		wg.Wait()
		if revokeErr != nil || (publishErr != nil && !errors.Is(publishErr, recommendations.ErrDraftGone)) {
			t.Fatalf("round %d: publish %v revoke %v", i, publishErr, revokeErr)
		}
		if _, err := pools[0].Drafts().Get(t.Context(), owner.ID, id, now); !errors.Is(err, drafts.ErrNotFound) {
			t.Fatalf("round %d: draft survived: %v", i, err)
		}
	}
}

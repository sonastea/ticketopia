package persistence

import (
	"net/url"
	"reflect"
	"strconv"
	"testing"
	"time"

	"github.com/sonastea/ticketopia/internal/accounts"
	"github.com/sonastea/ticketopia/internal/discussions"
	"github.com/sonastea/ticketopia/internal/saved"
	"github.com/sonastea/ticketopia/internal/useractivity"
)

func TestMariaDBUserActivityMergesOnlyPublicContributionsInStableOrder(t *testing.T) {
	f := newMaria(t)
	f.migrate(t)
	p := f.open(t)
	auth := accounts.New(p.Accounts(), accountProviderFixture{})
	owner, _ := loginAccount(t, auth, "timeline-owner")
	other, _ := loginAccount(t, auth, "timeline-other")
	one, two, three := savedDetail("TimelineOne"), savedDetail("TimelineTwo"), savedDetail("TimelinePrivate")
	for _, d := range []string{"TimelineOne", "TimelineTwo", "TimelinePrivate"} {
		if _, _, err := p.Saved().Save(t.Context(), owner.ID, savedDetail(d)); err != nil {
			t.Fatal(err)
		}
	}
	s := discussions.New(p.Discussions(), saved.New(p.Saved(), nil))
	create := func(who, event, thread, body string) discussions.Post {
		t.Helper()
		post, _, err := s.Create(t.Context(), who, event, thread, "", body, accounts.ID())
		if err != nil {
			t.Fatal(err)
		}
		return post
	}
	root := create(owner.ID, one.Item.ID, "", "Visible owner question")
	otherRoot := create(other.ID, one.Item.ID, "", "Someone else's question")
	reply := create(owner.ID, one.Item.ID, otherRoot.ID, "Owner reply in a thread that is later hidden")
	removed := create(owner.ID, one.Item.ID, "", "Removed secret text")
	if err := s.Remove(t.Context(), owner.ID, removed.ID); err != nil {
		t.Fatal(err)
	}
	hidden := create(owner.ID, one.Item.ID, "", "Moderator-hidden secret text")
	db := f.runtime.Database
	execSQL(t, f.admin, "UPDATE "+db+".discussion_posts SET hidden_at=UTC_TIMESTAMP(6) WHERE post_id IN (?,?)", hidden.ID, otherRoot.ID)
	if _, _, err := p.Recommendations().Set(t.Context(), owner.ID, one, "Active owner reason"); err != nil {
		t.Fatal(err)
	}
	if _, _, err := p.Recommendations().Set(t.Context(), owner.ID, two, "Withdrawn secret reason"); err != nil {
		t.Fatal(err)
	}
	if err := p.Recommendations().Remove(t.Context(), owner.ID, two.Item.ID); err != nil {
		t.Fatal(err)
	}
	for _, choice := range []struct {
		event      string
		visibility string
	}{{one.Item.ID, "public"}, {two.Item.ID, "public"}, {three.Item.ID, "private"}} {
		detail := savedDetail(choice.event[len("ticketmaster:"):])
		if _, _, err := p.Interests().Set(t.Context(), owner.ID, detail, choice.visibility); err != nil {
			t.Fatal(err)
		}
	}
	if _, _, err := p.Recommendations().Set(t.Context(), other.ID, one, "Other person's reason"); err != nil {
		t.Fatal(err)
	}
	if _, _, err := p.Interests().Set(t.Context(), other.ID, one, "public"); err != nil {
		t.Fatal(err)
	}

	// Force a tie across every type so ordering relies on (rank, key).
	tie := time.Date(2026, 11, 1, 12, 0, 0, 0, time.UTC)
	execSQL(t, f.admin, "UPDATE "+db+".discussion_posts SET created_at=? WHERE post_id=?", tie, root.ID)
	execSQL(t, f.admin, "UPDATE "+db+".discussion_posts SET created_at=? WHERE post_id=?", tie.Add(-time.Second), reply.ID)
	execSQL(t, f.admin, "UPDATE "+db+".event_recommendations SET recommended_at=? WHERE account_id=?", tie, owner.ID)
	execSQL(t, f.admin, "UPDATE "+db+".event_interests SET interested_at=? WHERE account_id=?", tie, owner.ID)

	service := useractivity.New(p.UserActivity())
	all, err := service.List(t.Context(), owner.ID, url.Values{"limit": {"100"}})
	if err != nil {
		t.Fatal(err)
	}
	type key struct{ kind, id string }
	var got []key
	for _, item := range all.Items {
		got = append(got, key{item.Type, item.Key()})
		if item.Event.Name == "" || item.Meta.DataAsOf.IsZero() {
			t.Fatal("item lost its event snapshot", item)
		}
	}
	want := []key{
		{useractivity.TypeRecommendation, one.Item.ID},
		{useractivity.TypeDiscussionPost, root.ID},
		{useractivity.TypeInterest, two.Item.ID},
		{useractivity.TypeInterest, one.Item.ID},
		{useractivity.TypeDiscussionPost, reply.ID},
	}
	if !reflect.DeepEqual(got, want) || all.NextCursor != nil {
		t.Fatalf("timeline leaked private, removed, hidden, withdrawn or other people's activity, or lost order:\n got %v\nwant %v", got, want)
	}
	if r := all.Items[0].Recommendation; r == nil || r.Reason != "Active owner reason" {
		t.Fatal("recommendation payload wrong", r)
	}
	if p := all.Items[4].Post; p == nil || p.ThreadID != otherRoot.ID || p.Body != reply.Body {
		t.Fatal("reply lost its thread or text", p)
	}

	// One-item pages walk the same order without gaps or repeats.
	for limit := 1; limit <= 2; limit++ {
		var walked []key
		values := url.Values{"limit": {strconv.Itoa(limit)}}
		for range 10 {
			page, err := service.List(t.Context(), owner.ID, values)
			if err != nil {
				t.Fatal(err)
			}
			for _, item := range page.Items {
				walked = append(walked, key{item.Type, item.Key()})
			}
			if page.NextCursor == nil {
				break
			}
			values.Set("cursor", *page.NextCursor)
		}
		if !reflect.DeepEqual(walked, want) {
			t.Fatalf("limit %d pagination diverged:\n got %v\nwant %v", limit, walked, want)
		}
	}

	if _, _, err := p.Interests().Set(t.Context(), owner.ID, two, "private"); err != nil {
		t.Fatal(err)
	}
	if page, err := service.List(t.Context(), owner.ID, nil); err != nil || len(page.Items) != 4 {
		t.Fatal("making interest private left it public", page, err)
	}
	if page, err := service.List(t.Context(), accounts.ID(), nil); err != nil || len(page.Items) != 0 {
		t.Fatal("unknown account returned activity", page, err)
	}
}

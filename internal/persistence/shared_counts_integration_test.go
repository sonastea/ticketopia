package persistence

import (
	"testing"

	"github.com/sonastea/ticketopia/internal/accounts"
	"github.com/sonastea/ticketopia/internal/activity"
)

// With shared totals on, as the server runs, a person's own recommendation and
// interest changes show in the next count, not after the sharing window.
func TestMariaDBSharedActivityCountsFollowOwnWrites(t *testing.T) {
	f := newMaria(t)
	f.migrate(t)
	p := f.open(t)
	auth := accounts.New(p.Accounts(), accountProviderFixture{})
	owner, _ := loginAccount(t, auth, "shared-counts")
	detail := savedDetail("SharedCounts")
	s := activity.New(t.Context(), p.Activity(), 4)
	defer s.Close()
	s.ShareCounts(activity.DefaultSharedCounts)
	count := func() activity.Count {
		t.Helper()
		c, err := s.Counts(t.Context(), []string{detail.Item.ID})
		if err != nil || len(c.Items) != 1 {
			t.Fatal(c, err)
		}
		return c.Items[0]
	}
	if c := count(); c.RecommendationCount != 0 || c.InterestedCount != 0 {
		t.Fatalf("before writes: %+v", c)
	}
	if _, _, err := p.Recommendations().Set(t.Context(), owner.ID, detail, "Worth going"); err != nil {
		t.Fatal(err)
	}
	if c := count(); c.RecommendationCount != 1 {
		t.Fatalf("own recommendation hidden by a shared total: %+v", c)
	}
	if _, _, err := p.Interests().Add(t.Context(), owner.ID, detail, "public"); err != nil {
		t.Fatal(err)
	}
	if c := count(); c.InterestedCount != 1 {
		t.Fatalf("own interest hidden by a shared total: %+v", c)
	}
	if err := p.Recommendations().Remove(t.Context(), owner.ID, detail.Item.ID); err != nil {
		t.Fatal(err)
	}
	if err := p.Interests().Remove(t.Context(), owner.ID, detail.Item.ID); err != nil {
		t.Fatal(err)
	}
	if c := count(); c.RecommendationCount != 0 || c.InterestedCount != 0 {
		t.Fatalf("withdrawals hidden by a shared total: %+v", c)
	}
}

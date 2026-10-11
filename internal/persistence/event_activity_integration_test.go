package persistence

import (
	"testing"

	"github.com/sonastea/ticketopia/internal/accounts"
	"github.com/sonastea/ticketopia/internal/discussions"
	"github.com/sonastea/ticketopia/internal/eventactivity"
	"github.com/sonastea/ticketopia/internal/saved"
)

func TestMariaDBEventActivityBatchesVisibleContributions(t *testing.T) {
	f := newMaria(t)
	f.migrate(t)
	p := f.open(t)
	auth := accounts.New(p.Accounts(), accountProviderFixture{})
	owner, _ := loginAccount(t, auth, "activity-author")
	other, _ := loginAccount(t, auth, "activity-other")
	detail, second := savedDetail("ActivityOne"), savedDetail("ActivityTwo")
	private := savedDetail("ActivityPrivate")
	for _, suffix := range []string{"One", "Two", "Private"} {
		if _, _, err := p.Saved().Save(t.Context(), owner.ID, savedDetail("Activity"+suffix)); err != nil {
			t.Fatal(err)
		}
	}
	s := discussions.New(p.Discussions(), saved.New(p.Saved(), nil))
	create := func(body string) discussions.Post {
		post, _, err := s.Create(t.Context(), owner.ID, detail.Item.ID, "", "", body, accounts.ID())
		if err != nil {
			t.Fatal(err)
		}
		return post
	}
	first := create("Visible first question")
	latest := create("Visible latest question")
	removed := create("Removed secret text")
	if err := s.Remove(t.Context(), owner.ID, removed.ID); err != nil {
		t.Fatal(err)
	}
	hidden := create("Moderator-hidden secret text")
	execSQL(t, f.admin, "UPDATE "+f.runtime.Database+".discussion_posts SET hidden_at=UTC_TIMESTAMP(6) WHERE post_id=?", hidden.ID)
	if _, _, err := s.Create(t.Context(), other.ID, detail.Item.ID, first.ID, "", "Reply is not a root preview", accounts.ID()); err != nil {
		t.Fatal(err)
	}
	if _, _, err := p.Recommendations().Set(t.Context(), owner.ID, detail, "An active endorsement"); err != nil {
		t.Fatal(err)
	}
	if _, _, err := p.Recommendations().Set(t.Context(), other.ID, detail, "A withdrawn reason"); err != nil {
		t.Fatal(err)
	}
	if err := p.Recommendations().Remove(t.Context(), other.ID, detail.Item.ID); err != nil {
		t.Fatal(err)
	}
	if _, _, err := p.Interests().Set(t.Context(), other.ID, private, "private"); err != nil {
		t.Fatal(err)
	}
	secondPost, _, err := s.Create(t.Context(), other.ID, second.Item.ID, "", "", "Another event's question", accounts.ID())
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := p.Recommendations().Set(t.Context(), other.ID, second, ""); err != nil {
		t.Fatal(err)
	}
	previews, err := eventactivity.New(p.EventActivity()).Previews(t.Context(), []string{detail.Item.ID, second.Item.ID, private.Item.ID, "ticketmaster:Absent"})
	if err != nil {
		t.Fatal(err)
	}
	preview := previews[detail.Item.ID]
	if len(previews) != 2 || preview.Questions != 2 || preview.Question == nil || preview.Question.ID != latest.ID || preview.Question.Body != latest.Body || preview.Recommendations != 1 || preview.Recommendation == nil || preview.Recommendation.Body != "An active endorsement" {
		t.Fatal("activity leaked hidden/removed/withdrawn/private data, lost ordering, or crossed event scope", previews)
	}
	preview = previews[second.Item.ID]
	if preview.Questions != 1 || preview.Question == nil || preview.Question.ID != secondPost.ID || preview.Recommendations != 1 || preview.Recommendation == nil || preview.Recommendation.Body != "" || preview.Recommendation.AuthorID != other.ID {
		t.Fatal("batch mixed event partitions or lost reason-free endorsements", preview)
	}
	if err := s.Remove(t.Context(), owner.ID, latest.ID); err != nil {
		t.Fatal(err)
	}
	previews, err = p.EventActivity().Previews(t.Context(), []string{detail.Item.ID})
	if err != nil || previews[detail.Item.ID].Question == nil || previews[detail.Item.ID].Question.ID != first.ID || previews[detail.Item.ID].Questions != 1 {
		t.Fatal("removed preview retained text instead of selecting the next visible root", previews, err)
	}
}

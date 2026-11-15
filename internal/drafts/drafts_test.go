package drafts

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/sonastea/ticketopia/internal/accounts"
	"github.com/sonastea/ticketopia/internal/discussions"
	"github.com/sonastea/ticketopia/internal/models"
)

type repo struct {
	created []Draft
	// snapshots records whether each Create stored an event snapshot.
	snapshots []bool
}

func (r *repo) Create(_ context.Context, detail *models.EventDetail, d Draft, _ string, _ time.Time) (Draft, error) {
	r.created = append(r.created, d)
	r.snapshots = append(r.snapshots, detail != nil)
	return d, nil
}

// threads has one thread, "1…1" on ticketmaster:Talk, with reply "2…2".
type threads struct{}

func (threads) Thread(_ context.Context, _, event, id string) (discussions.Post, error) {
	if event != "ticketmaster:Talk" || id != strings.Repeat("1", 32) {
		return discussions.Post{}, discussions.ErrNotFound
	}
	return discussions.Post{ID: id, ThreadID: id, EventID: event, Event: models.Event{ID: event, Name: "Talk"}}, nil
}

func (threads) Parent(_ context.Context, _, event, thread, id string) (discussions.Post, error) {
	if id != strings.Repeat("2", 32) && id != thread {
		return discussions.Post{}, discussions.ErrNotFound
	}
	return discussions.Post{ID: id, ThreadID: thread, EventID: event}, nil
}

// countingDetails fails the test if a reply draft reads the provider.
type countingDetails struct{ calls *int }

func (d countingDetails) Detail(ctx context.Context, id string) (models.EventDetail, error) {
	*d.calls++
	return details{}.Detail(ctx, id)
}

// Reply drafts answer a real thread on their event and a post in it, use
// the thread's stored event rather than the provider, and treat answering
// the root as answering the thread. Questions read the event.
func TestDiscussionDrafts(t *testing.T) {
	r, calls := &repo{}, 0
	s := New(r, countingDetails{&calls}, threads{})
	owner, event, thread, reply := accounts.ID(), "ticketmaster:Talk", strings.Repeat("1", 32), strings.Repeat("2", 32)

	d, err := s.CreateReply(t.Context(), owner, event, thread, reply, "  Row A.  ", Client{})
	if err != nil || d.Kind != KindReply || d.ThreadID != thread || d.ParentID != reply || d.Text != "Row A." || d.Event.Name != "Talk" || !d.Discussion() {
		t.Fatalf("reply: %+v %v", d, err)
	}
	if d, err = s.CreateReply(t.Context(), owner, event, thread, thread, "Root", Client{}); err != nil || d.ParentID != "" {
		t.Fatalf("answering the root: %+v %v", d, err)
	}
	if calls != 0 || r.snapshots[0] || r.snapshots[1] {
		t.Fatalf("reply drafts read the provider (%d) or stored snapshots %v", calls, r.snapshots)
	}
	for name, c := range map[string]struct {
		event, thread, parent, body string
		want                        error
	}{
		"other event":    {"ticketmaster:Other", thread, "", "x", discussions.ErrNotFound},
		"unknown thread": {event, strings.Repeat("9", 32), "", "x", discussions.ErrNotFound},
		"unknown parent": {event, thread, strings.Repeat("9", 32), "x", ErrParentNotFound},
	} {
		if _, err := s.CreateReply(t.Context(), owner, c.event, c.thread, c.parent, c.body, Client{}); !errors.Is(err, c.want) {
			t.Errorf("%s: %v", name, err)
		}
	}
	var invalid *accounts.ValidationError
	for field, args := range map[string][3]string{"thread_id": {"bad", "", "x"}, "parent_id": {thread, "bad", "x"}, "body": {thread, "", strings.Repeat("a", discussions.MaxBody+1)}} {
		if _, err := s.CreateReply(t.Context(), owner, event, args[0], args[1], args[2], Client{}); !errors.As(err, &invalid) || invalid.Field != field {
			t.Errorf("%s: %v", field, err)
		}
	}
	if _, err := New(r, details{}, nil).CreateReply(t.Context(), owner, event, thread, "", "x", Client{}); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("without discussions: %v", err)
	}

	q, err := s.CreateQuestion(t.Context(), owner, event, "Cloakroom?", Client{Name: "Agent", Host: "agent.example", GrantID: "g"})
	if err != nil || q.Kind != KindQuestion || q.ThreadID != "" || q.ClientHost != "agent.example" || calls != 1 || !r.snapshots[len(r.snapshots)-1] {
		t.Fatalf("question: %+v %v calls=%d", q, err, calls)
	}
	if _, err := s.CreateQuestion(t.Context(), owner, event, " ", Client{}); !errors.As(err, &invalid) || invalid.Field != "body" {
		t.Fatalf("empty question: %v", err)
	}
	if err := discussions.Key(q.PublishKey()); err != nil {
		t.Fatalf("publish key %q: %v", q.PublishKey(), err)
	}
}
func (r *repo) List(context.Context, string, time.Time) ([]Draft, error) { return nil, nil }
func (r *repo) Get(context.Context, string, string, time.Time) (Draft, error) {
	return Draft{}, ErrNotFound
}
func (r *repo) Delete(context.Context, string, string) (bool, error)  { return false, nil }
func (r *repo) Count(context.Context, string, time.Time) (int, error) { return 0, nil }

type details struct{}

func (details) Detail(_ context.Context, id string) (models.EventDetail, error) {
	return models.EventDetail{Item: models.Event{ID: id}}, nil
}

// API tokens identify no app; an OAuth connection is always identified as
// itself and keeps its connection, or the draft is refused, never downgraded.
func TestDraftIdentityIsNeverDowngraded(t *testing.T) {
	r := &repo{}
	s := New(r, details{}, nil)
	owner, event := accounts.ID(), "ticketmaster:Draft"
	d, err := s.CreateRecommendation(t.Context(), owner, event, "Words", Client{})
	if err != nil || d.ClientName != APIClientName || d.ClientHost != "" || d.GrantID != "" || !d.FromAPIToken() {
		t.Fatalf("API token draft: %+v %v", d, err)
	}
	d, err = s.CreateRecommendation(t.Context(), owner, event, "Words", Client{Name: "Agent", Host: "agent.example", GrantID: "g"})
	if err != nil || d.ClientName != "Agent" || d.ClientHost != "agent.example" || d.GrantID != "g" || d.FromAPIToken() {
		t.Fatalf("OAuth draft: %+v %v", d, err)
	}
	for _, client := range []Client{{Name: "Agent", Host: strings.Repeat("a", MaxClientHost+1), GrantID: "g"}, {Name: "Agent", GrantID: "g"}, {Name: "Agent", Host: "agent.example"}} {
		if _, err := s.CreateRecommendation(t.Context(), owner, event, "Words", client); !errors.Is(err, ErrUnavailable) {
			t.Fatalf("%+v: %v", client, err)
		}
	}
	if len(r.created) != 2 {
		t.Fatalf("created %d", len(r.created))
	}
}

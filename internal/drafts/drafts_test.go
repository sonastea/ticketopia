package drafts

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/sonastea/ticketopia/internal/accounts"
	"github.com/sonastea/ticketopia/internal/models"
)

type repo struct{ created []Draft }

func (r *repo) Create(_ context.Context, _ models.EventDetail, d Draft, _ string, _ time.Time) (Draft, error) {
	r.created = append(r.created, d)
	return d, nil
}
func (r *repo) List(context.Context, string, time.Time) ([]Draft, error) { return nil, nil }
func (r *repo) Get(context.Context, string, string, time.Time) (Draft, error) {
	return Draft{}, ErrNotFound
}
func (r *repo) Delete(context.Context, string, string) error          { return nil }
func (r *repo) Count(context.Context, string, time.Time) (int, error) { return 0, nil }

type details struct{}

func (details) Detail(_ context.Context, id string) (models.EventDetail, error) {
	return models.EventDetail{Item: models.Event{ID: id}}, nil
}

// API tokens identify no app; an OAuth connection is always identified as
// itself and keeps its connection, or the draft is refused, never downgraded.
func TestDraftIdentityIsNeverDowngraded(t *testing.T) {
	r := &repo{}
	s := New(r, details{})
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

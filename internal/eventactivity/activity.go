// Package eventactivity composes lightweight public discovery previews. It does
// not expose private saves, interest identities, reporting, or viewer state.
package eventactivity

import (
	"context"
	"errors"

	"github.com/sonastea/ticketopia/internal/accounts"
	"github.com/sonastea/ticketopia/internal/saved"
)

var ErrUnavailable = errors.New("event activity unavailable")

type Contribution struct {
	ID, Body, AuthorID, AuthorName string
	// Via names the agent's app (or "an API client") that wrote or published
	// it; empty for the person's own words. Previews show an Agent label.
	Via string
}

type Preview struct {
	Questions, Recommendations int
	Question, Recommendation   *Contribution
}

// Community is one event's public totals and, separately, the signed-in
// viewer's own state, as GET /api/v1/events/{id}/community and the agents'
// event_community tool return it.
type Community struct {
	EventID string          `json:"event_id"`
	Counts  CommunityCounts `json:"counts"`
	// Viewer is nil (null) without a signed-in caller.
	Viewer *CommunityViewer `json:"viewer"`
}

type CommunityCounts struct {
	Interested      int `json:"interested" jsonschema:"all interest, including private choices, without identities"`
	Recommendations int `json:"recommendations" jsonschema:"active public recommendations"`
	Discussions     int `json:"discussions" jsonschema:"visible root questions and tips; replies are not counted"`
}

// CommunityViewer is a separate object so a client never mistakes a public
// count for a personal choice.
type CommunityViewer struct {
	Saved              bool   `json:"saved"`
	Interested         bool   `json:"interested"`
	InterestVisibility string `json:"interest_visibility,omitempty" jsonschema:"private or public; present only when interested"`
	Recommended        bool   `json:"recommended"`
}

type Repository interface {
	Previews(context.Context, []string) (map[string]Preview, error)
}

type Service struct{ repo Repository }

func New(repo Repository) *Service { return &Service{repo: repo} }

// Previews reads at most one discovery page, independent of the viewer. Empty
// results mean no public activity; a failed read is never projected as zero.
func (s *Service) Previews(ctx context.Context, ids []string) (map[string]Preview, error) {
	if len(ids) > 100 {
		return nil, &accounts.ValidationError{Field: "event_id", Message: "use at most 100 events"}
	}
	unique := make([]string, 0, len(ids))
	seen := make(map[string]bool, len(ids))
	for _, id := range ids {
		if err := saved.ValidateID(id); err != nil {
			return nil, err
		}
		if !seen[id] {
			seen[id] = true
			unique = append(unique, id)
		}
	}
	if len(unique) == 0 {
		return map[string]Preview{}, nil
	}
	if s.repo == nil {
		return nil, ErrUnavailable
	}
	return s.repo.Previews(ctx, unique)
}

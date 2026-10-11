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
}

type Preview struct {
	Questions, Recommendations int
	Question, Recommendation   *Contribution
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

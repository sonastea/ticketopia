// Package events is the shared boundary for durable event identity. Discovery
// remains read-only; local actions explicitly ensure an identity.
package events

import (
	"context"
	"errors"

	"github.com/sonastea/ticketopia/internal/models"
)

var ErrDisabled = errors.New("durable event storage is disabled")
var ErrNotFound = errors.New("durable event not found")

type Repository interface {
	Upsert(context.Context, models.Event) (models.Event, error)
	Get(context.Context, string) (models.Event, error)
}

type Service struct{ repository Repository }

func New(repository Repository) *Service { return &Service{repository: repository} }

// EnsureDurable is explicit, not called by discovery reads. Bookmark writes use
// the same repository identity operations inside their activity transaction.
func (s *Service) EnsureDurable(ctx context.Context, event models.Event) (models.Event, error) {
	if s.repository == nil {
		return models.Event{}, ErrDisabled
	}
	return s.repository.Upsert(ctx, event)
}

func (s *Service) Get(ctx context.Context, id string) (models.Event, error) {
	if s.repository == nil {
		return models.Event{}, ErrDisabled
	}
	return s.repository.Get(ctx, id)
}

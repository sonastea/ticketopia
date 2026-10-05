package events

import (
	"errors"
	"testing"

	"github.com/sonastea/ticketopia/internal/models"
)

func TestDisabledDoesNotPretendToPersist(t *testing.T) {
	s := New(nil)
	if _, err := s.EnsureDurable(t.Context(), models.Event{}); !errors.Is(err, ErrDisabled) {
		t.Fatal(err)
	}
	if _, err := s.Get(t.Context(), "ticketmaster:ID"); !errors.Is(err, ErrDisabled) {
		t.Fatal(err)
	}
}

package history

import (
	"context"
	"errors"
	"time"

	"github.com/sonastea/ticketopia/internal/discovery"
	"github.com/sonastea/ticketopia/internal/models"
)

type DetailRepository interface {
	ClaimDetail(context.Context) (string, string, error)
	FinishDetail(context.Context, string, string, *models.EventDetail, time.Duration) error
}
type DetailProvider interface {
	RefreshEvent(context.Context, string) (models.EventDetail, error)
}

// At most one targeted detail per minute per worker, sharing the provider gate.
// A failed/404 read is a retry receipt, never a cancellation observation.
func RefreshMissing(ctx context.Context, r DetailRepository, p DetailProvider) (bool, error) {
	id, token, err := r.ClaimDetail(ctx)
	if err != nil || id == "" {
		return false, err
	}
	fetchCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
	detail, fetchErr := p.RefreshEvent(fetchCtx, id)
	cancel()
	delay := time.Hour
	var value *models.EventDetail
	if fetchErr == nil && !detail.Meta.Stale && !detail.Meta.DataAsOf.IsZero() {
		value = &detail
	}
	if errors.Is(fetchErr, discovery.ErrNotFound) {
		delay = 24 * time.Hour
	}
	var unavailable *discovery.UnavailableError
	if errors.As(fetchErr, &unavailable) {
		delay = max(delay, unavailable.RetryAfter)
	}
	finishCtx, finishCancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer finishCancel()
	return true, r.FinishDetail(finishCtx, id, token, value, delay)
}

package discovery

import (
	"context"
	"errors"
	"time"

	"github.com/sonastea/ticketopia/internal/models"
)

const (
	storedColdWait    = 12 * time.Second
	storedPageTimeout = 20 * time.Second
)

type CatalogRead struct {
	List    models.EventList
	Refresh bool
}

type Catalog interface {
	Search(context.Context, Query, time.Duration) (CatalogRead, error)
	ClaimSearch(context.Context, Query) (string, error)
	SearchPage(context.Context, Query, string, int, models.EventList) error
	FinishSearch(context.Context, Query, string, string, time.Duration) (string, error)
	Snapshot(context.Context, string) (models.EventDetail, error)
	Observe(context.Context, models.EventDetail) error
	History(context.Context, string, time.Time, int) (*models.EventHistory, error)
}

func (s *Service) storedEvents(ctx context.Context, q Query) (models.EventList, error) {
	if err := ctx.Err(); err != nil {
		return models.EventList{}, err
	}
	read, err := s.catalog.Search(ctx, q, s.freshFor)
	if err != nil {
		return models.EventList{}, err
	}
	if read.Refresh {
		// One browser must not cancel a refresh shared by other visitors. Track
		// the work so shutdown drains it before closing persistence/cache.
		done := make(chan struct{})
		s.refreshes.Go(func() {
			defer close(done)
			s.flights.Do("stored:"+q.ScopeID(), func() (any, error) {
				err := s.refreshSearch(q.CollectionScope())
				if err != nil && s.ctx.Err() == nil {
					s.logger.Warn().Err(err).Msg("Discover refresh incomplete; retaining catalog")
				}
				return nil, err
			})
		})
		// Serve last-good results (including previously collected empty searches)
		// immediately, preserving their freshness/coverage labels.
		if len(read.List.Items) > 0 || !read.List.Meta.DataAsOf.IsZero() {
			return read.List, nil
		}
		// A cold visitor waits briefly for useful results, not for every page.
		// Expiring this wait must not cancel shared collection or its SQL writes.
		timer := time.NewTimer(storedColdWait)
		defer timer.Stop()
		select {
		case <-ctx.Done():
			return models.EventList{}, ctx.Err()
		case <-done:
		case <-timer.C:
		}
		updated, readErr := s.catalog.Search(ctx, q, s.freshFor)
		if readErr == nil {
			read = updated
		} else if len(read.List.Items) == 0 {
			return models.EventList{}, readErr
		}
	}
	return read.List, nil
}

func (s *Service) refreshSearch(scope Query) error {
	// Ownership/cooldown are durable across replicas/restarts. The service
	// lifecycle, ten-page cap and per-page deadlines bound collection, not the
	// initiating request. Keep each page below the catalog's 30-second lease.
	claimCtx, claimCancel := context.WithTimeout(s.ctx, storedPageTimeout)
	token, err := s.catalog.ClaimSearch(claimCtx, scope)
	claimCancel()
	if err != nil || token == "" {
		return err
	}
	status, delay := "complete", s.freshFor
	pages := 0
	var ingestionErr error
	for scope.Page = 0; scope.Page < 10; scope.Page++ {
		// Earlier fetches/commits must not consume later pages' ingestion budget.
		pageCtx, pageCancel := context.WithTimeout(s.ctx, storedPageTimeout)
		list, fetchErr := s.Collect(pageCtx, scope)
		if fetchErr == nil {
			ingestionErr = s.catalog.SearchPage(pageCtx, scope, token, scope.Page, list)
		}
		pageCancel()
		if fetchErr != nil || ingestionErr != nil {
			status, delay = "failed", time.Hour
			if pages > 0 {
				status = "partial"
			}
			var unavailable *UnavailableError
			if errors.As(fetchErr, &unavailable) {
				delay = max(delay, unavailable.RetryAfter)
			}
			break
		}
		pages++
		if list.Limited || (scope.Page == 9 && list.NextCursor != nil) {
			status = "limited"
			break
		}
		if list.NextCursor == nil {
			break
		}
	}
	// Finalize even if ingestion fails or collection/shutdown cancels its context:
	// release the claim and record failed/partial coverage instead of leaving it
	// running until its lease expires. Never retry an uncertain page commit here.
	finishCtx, finishCancel := context.WithTimeout(context.WithoutCancel(s.ctx), 5*time.Second)
	defer finishCancel()
	status, err = s.catalog.FinishSearch(finishCtx, scope, token, status, delay)
	if err == nil {
		s.logger.Info().Str("scope", scope.ScopeID()).Str("coverage", status).Int("pages", pages).Msg("Discover collection refreshed")
	}
	return errors.Join(ingestionErr, err)
}

func (s *Service) storedEvent(ctx context.Context, id string) (models.EventDetail, error) {
	if _, err := sourceID(id); err != nil {
		return models.EventDetail{}, err
	}
	stored, storedErr := s.catalog.Snapshot(ctx, id)
	detail := stored
	if storedErr != nil || s.now().Sub(stored.Meta.DataAsOf) >= s.freshFor {
		fresh, err := s.providerEvent(ctx, id)
		if err != nil {
			if storedErr != nil {
				return models.EventDetail{}, err
			}
			detail.Meta.Stale = true
		} else {
			detail = fresh
			if !fresh.Meta.Stale {
				if err := s.catalog.Observe(ctx, fresh); err != nil {
					return models.EventDetail{}, err
				}
			}
		}
	} else {
		detail.Meta.Stale = stored.Meta.Stale
	}
	h, err := s.catalog.History(ctx, id, time.Time{}, 20)
	if err != nil {
		return models.EventDetail{}, err
	}
	detail.History = h
	return detail, nil
}

func (s *Service) History(ctx context.Context, id string, before time.Time, limit int) (*models.EventHistory, error) {
	if _, err := sourceID(id); err != nil {
		return nil, err
	}
	if limit < 1 || limit > 100 {
		return nil, &ValidationError{"limit", "use 1 to 100"}
	}
	if s.catalog == nil {
		return nil, &UnavailableError{RetryAfter: time.Minute}
	}
	return s.catalog.History(ctx, id, before, limit)
}

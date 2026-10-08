package discovery

import (
	"context"
	"errors"
	"time"

	"github.com/sonastea/ticketopia/internal/models"
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
	read, err := s.catalog.Search(ctx, q, s.freshFor)
	if err != nil {
		return models.EventList{}, err
	}
	if read.Refresh {
		// Refresh ownership and cooldown are durable across replicas/restarts.
		// Bound foreground collection; committed pages remain useful if interrupted.
		_, err, _ = s.flights.Do("stored:"+q.ScopeID(), func() (any, error) {
			refreshCtx, cancel := context.WithTimeout(ctx, 12*time.Second)
			defer cancel()
			scope := q.CollectionScope()
			token, err := s.catalog.ClaimSearch(refreshCtx, scope)
			if err != nil || token == "" {
				return nil, err
			}
			status, delay := "complete", s.freshFor
			pages := 0
			for scope.Page = 0; scope.Page < 10; scope.Page++ {
				list, fetchErr := s.Collect(refreshCtx, scope)
				if fetchErr != nil {
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
				if err := s.catalog.SearchPage(refreshCtx, scope, token, scope.Page, list); err != nil {
					return nil, err
				}
				pages++
				if list.Limited {
					status = "limited"
					break
				}
				if list.NextCursor == nil {
					break
				}
				if scope.Page == 9 {
					status = "limited"
				}
			}
			finishCtx, finishCancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
			defer finishCancel()
			status, err = s.catalog.FinishSearch(finishCtx, scope, token, status, delay)
			if err == nil {
				s.logger.Info().Str("scope", scope.ScopeID()).Str("coverage", status).Int("pages", pages).Msg("Discover collection refreshed")
			}
			return nil, err
		})
		if err != nil {
			s.logger.Warn().Err(err).Msg("Discover refresh incomplete; retaining catalog")
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

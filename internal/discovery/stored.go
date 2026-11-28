package discovery

import (
	"context"
	"errors"
	"slices"
	"time"

	"github.com/sonastea/ticketopia/internal/models"
)

const (
	storedColdWait    = 12 * time.Second
	storedColdTick    = 250 * time.Millisecond
	storedPageTimeout = 20 * time.Second
)

type CatalogRead struct {
	List    models.EventList
	Refresh bool
	// RefreshScopes are a metro's members due for refresh. Empty means the
	// search itself is the one scope to refresh.
	RefreshScopes []Query
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
	read, err := s.searchCatalog(ctx, q)
	if err != nil {
		return models.EventList{}, err
	}
	if read.Refresh {
		// A metro refreshes only its members whose coverage is due; durable
		// claims still skip any being collected elsewhere.
		scopes := read.RefreshScopes
		if len(scopes) == 0 {
			scopes = q.MemberQueries()
		}
		dones := make([]<-chan struct{}, 0, len(scopes))
		for _, scope := range scopes {
			dones = append(dones, s.startRefresh(scope))
		}
		// Serve last-good results (including previously collected empty searches)
		// immediately, preserving their freshness/coverage labels.
		if len(read.List.Items) > 0 || !read.List.Meta.DataAsOf.IsZero() {
			return withArea(q, read.List), nil
		}
		// A cold visitor waits briefly for useful results, not for every page or
		// member: a finished member's results end the wait. Completion is checked
		// on a short tick, so visitors sharing a refresh add no goroutines.
		// Expiring the wait must not cancel shared collection or its SQL writes.
		timer := time.NewTimer(storedColdWait)
		defer timer.Stop()
		tick := time.NewTicker(storedColdTick)
		defer tick.Stop()
	wait:
		for len(dones) > 0 {
			select {
			case <-ctx.Done():
				return models.EventList{}, ctx.Err()
			case <-timer.C:
				break wait
			case <-dones[0]:
			case <-tick.C:
			}
			pending := dones[:0]
			for _, done := range dones {
				select {
				case <-done:
				default:
					pending = append(pending, done)
				}
			}
			finished := len(pending) < len(dones)
			dones = pending
			// Read past the shared window: collection may have just stored results.
			if finished && len(dones) > 0 {
				if updated, err := s.readCatalog(ctx, storedReadKey(q), q); err == nil && len(updated.List.Items) > 0 {
					return withArea(q, shareRead(updated).List), nil
				}
			}
		}
		updated, readErr := s.readCatalog(ctx, storedReadKey(q), q)
		if readErr == nil {
			read = shareRead(updated)
		} else if len(read.List.Items) == 0 {
			return models.EventList{}, readErr
		}
	}
	return withArea(q, read.List), nil
}

// KeepFresh reads q as a visitor would, so stored coverage that has expired
// is collected again: for a metro, only its due member cities. It needs the
// stored catalog; scheduled collection calls it for configured metro areas.
func (s *Service) KeepFresh(ctx context.Context, q Query) error {
	if s.catalog == nil {
		return nil
	}
	_, err := s.Events(ctx, q)
	return err
}

// Stored reports whether searches read the durable catalog, where metro areas
// and folded runs apply. Database-free discovery ignores both.
func (s *Service) Stored() bool { return s != nil && s.catalog != nil }

// withArea names a metro search's area on its list. Lists can be shared
// reads, so the area is set on this copy of the list value only.
func withArea(q Query, list models.EventList) models.EventList {
	if m, ok := q.Metro(); ok {
		list.Area = &models.Area{Name: m.Name, Country: m.Country, Cities: m.Cities}
	}
	return list
}

// startRefresh returns a channel closed when the scope's in-process refresh
// ends, starting one only if none is running. Concurrent visitors share it
// rather than each parking a goroutine until it finishes.
func (s *Service) startRefresh(q Query) <-chan struct{} {
	key := q.ScopeID()
	s.refreshMu.Lock()
	defer s.refreshMu.Unlock()
	if done, ok := s.refreshing[key]; ok {
		return done
	}
	if s.refreshing == nil {
		s.refreshing = make(map[string]chan struct{})
	}
	done := make(chan struct{})
	s.refreshing[key] = done
	// One browser must not cancel a refresh shared by other visitors. Track
	// the work so shutdown drains it before closing persistence/cache.
	s.refreshes.Go(func() {
		defer func() {
			// Shared reads may predate this collection; serve its results next.
			s.stored.clear()
			s.refreshMu.Lock()
			delete(s.refreshing, key)
			s.refreshMu.Unlock()
			close(done)
		}()
		if err := s.refreshSearch(q.CollectionScope()); err != nil && s.ctx.Err() == nil {
			s.logger.Warn().Err(err).Msg("Discover refresh incomplete; retaining catalog")
		}
	})
	return done
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
		refresh, err := s.refreshStored(ctx, id)
		fresh := refresh.detail
		if err != nil {
			if storedErr != nil {
				return models.EventDetail{}, err
			}
			detail.Meta.Stale = true
		} else if fresh.Meta.Stale && storedErr == nil && !fresh.Meta.DataAsOf.After(stored.Meta.DataAsOf) {
			// A retained cache fallback must not rewind newer durable facts.
			detail.Meta.Stale = true
		} else {
			if refresh.observeErr != nil {
				return models.EventDetail{}, refresh.observeErr
			}
			detail = fresh
		}
	} else {
		detail.Meta.Stale = stored.Meta.Stale
	}
	h, err := s.catalog.History(ctx, id, time.Time{}, 20)
	if err != nil {
		return models.EventDetail{}, err
	}
	detail.History = h
	detail.Dates, detail.DatesMore = s.eventDates(ctx, detail.Item)
	return detail, nil
}

// DateLister is an optional Catalog capability: the stored listings of one
// event's name at its venue, as Discover folds them into a run.
type DateLister interface {
	Dates(ctx context.Context, e models.Event, from, to string, notBefore time.Time, limit int) ([]models.EventDate, error)
}

// eventDates lists an event's upcoming run, including the event itself unless
// it has started, or nil when there is nothing else to list. It reports whether
// more listings exist beyond MaxEventDates. The run is optional enrichment: a
// failed read leaves the event page usable.
func (s *Service) eventDates(ctx context.Context, e models.Event) ([]models.EventDate, bool) {
	lister, ok := s.catalog.(DateLister)
	if !ok || e.Upsell() {
		return nil, false
	}
	now := s.now().UTC()
	// Yesterday in UTC keeps today's listings in every venue time zone; the
	// instant filter then drops listings that have already started.
	from := now.AddDate(0, 0, -1)
	dates, err := lister.Dates(ctx, e, from.Format(time.DateOnly), from.AddDate(0, 0, 367).Format(time.DateOnly), now, models.MaxEventDates+1)
	if err != nil {
		s.logger.Warn().Err(err).Msg("Could not read an event's other dates")
		return nil, false
	}
	more := len(dates) > models.MaxEventDates
	dates = dates[:min(len(dates), models.MaxEventDates)]
	// A long run's later listing starts its own list, so it is always marked
	// and opening the last listed date moves on to the dates after it.
	at := slices.IndexFunc(dates, func(d models.EventDate) bool { return d.ID == e.ID })
	if more && e.Start.LocalDate != nil && (at < 0 || at == len(dates)-1) {
		later, err := lister.Dates(ctx, e, *e.Start.LocalDate, from.AddDate(0, 0, 367).Format(time.DateOnly), now, models.MaxEventDates+1)
		if err == nil && len(later) > 0 {
			more = len(later) > models.MaxEventDates
			dates = later[:min(len(later), models.MaxEventDates)]
		}
	}
	if len(dates) < 2 && !(len(dates) == 1 && dates[0].ID != e.ID) {
		return nil, false
	}
	return dates, more
}

type storedRefresh struct {
	detail     models.EventDetail
	observeErr error
}

// refreshStored shares one provider read and its durable observation among
// concurrent readers of an event. The write runs on the service lifecycle, so a
// cancelled reader cannot abandon an observation others wait for; its error
// reaches every reader and an uncertain commit is never replayed. A stale cache
// fallback is not observed.
func (s *Service) refreshStored(ctx context.Context, id string) (storedRefresh, error) {
	result := s.flights.DoChan("observe:"+id, func() (any, error) {
		s.refreshes.Add(1)
		defer s.refreshes.Done()
		fresh, err := s.providerEvent(s.ctx, id)
		if err != nil || fresh.Meta.Stale {
			return storedRefresh{detail: fresh}, err
		}
		observeCtx, cancel := context.WithTimeout(s.ctx, 10*time.Second)
		defer cancel()
		return storedRefresh{fresh, s.catalog.Observe(observeCtx, fresh)}, nil
	})
	select {
	case <-ctx.Done():
		return storedRefresh{}, ctx.Err()
	case r := <-result:
		refresh, _ := r.Val.(storedRefresh)
		return refresh, r.Err
	}
}

// MaxSnapshotBatch bounds StoredEvents reads.
const MaxSnapshotBatch = 50

// SnapshotReader is an optional Catalog capability for bounded batch reads.
type SnapshotReader interface {
	Snapshots(context.Context, []string) (map[string]models.Event, error)
}

// StoredEvents returns last-known snapshots for display enrichment only. It
// never calls the provider; missing events are simply absent, and a failed
// read degrades to fallback presentation rather than failing the page.
func (s *Service) StoredEvents(ctx context.Context, ids []string) map[string]models.Event {
	reader, ok := s.catalog.(SnapshotReader)
	if !ok || len(ids) == 0 {
		return nil
	}
	events, err := reader.Snapshots(ctx, ids[:min(len(ids), MaxSnapshotBatch)])
	if err != nil {
		s.logger.Warn().Err(err).Msg("Could not read event snapshots for display")
		return nil
	}
	return events
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

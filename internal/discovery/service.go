package discovery

import (
	"context"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/rs/zerolog"
	"github.com/sonastea/ticketopia/internal/kv"
	"github.com/sonastea/ticketopia/internal/models"
	"golang.org/x/sync/singleflight"
)

type Config struct {
	APIKey      string
	DailyBudget int
	BaseURL     string
	HTTPClient  *http.Client
	Coordinator Coordinator
}

type Service struct {
	ctx         context.Context
	cache       kv.Store
	logger      zerolog.Logger
	key         string
	baseURL     string
	client      *http.Client
	gate        requestGate
	flights     singleflight.Group
	now         func() time.Time
	coordinator Coordinator
}

func New(ctx context.Context, cache kv.Store, logger zerolog.Logger, config Config) *Service {
	if config.BaseURL == "" {
		config.BaseURL = "https://app.ticketmaster.com/discovery/v2"
	}
	if config.HTTPClient == nil {
		config.HTTPClient = &http.Client{
			Timeout:       8 * time.Second,
			CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
		}
	}
	if config.DailyBudget <= 0 {
		config.DailyBudget = 4500
	}
	return &Service{
		ctx: ctx, cache: cache, logger: logger, key: strings.TrimSpace(config.APIKey),
		baseURL: strings.TrimRight(config.BaseURL, "/"), client: config.HTTPClient,
		gate: requestGate{budget: config.DailyBudget, interval: 250 * time.Millisecond}, now: time.Now, coordinator: config.Coordinator,
	}
}

type eventPage struct {
	Items      []models.Event `json:"items"`
	Total      int            `json:"total"`
	TotalPages int            `json:"total_pages"`
}

func (s *Service) Events(ctx context.Context, q Query) (models.EventList, error) {
	// Validate again at the service boundary for non-HTTP clients.
	validated, err := ParseQuery(q.Values(), s.now())
	if err != nil {
		return models.EventList{}, err
	}
	if q.Page < 0 || q.Page > 999/validated.Limit {
		return models.EventList{}, &ValidationError{"cursor", "page exceeds the provider result limit"}
	}
	validated.Page = q.Page
	q = validated
	page, meta, err := cached(ctx, s, q.cacheKey(), eventPolicy, func(ctx context.Context) (eventPage, error) {
		return s.fetchPage(ctx, q)
	})
	if err != nil {
		return models.EventList{}, err
	}
	return eventList(q, page, meta), nil
}

func eventList(q Query, page eventPage, meta models.Freshness) models.EventList {
	list := models.EventList{Items: page.Items, Total: page.Total, Meta: meta}
	if len(page.Items) > 0 && q.Page+1 < page.TotalPages {
		if (q.Page+1)*q.Limit < 1000 {
			cursor := q.nextCursor()
			list.NextCursor = &cursor
		} else {
			list.Limited = true
		}
	}
	return list
}

func (s *Service) fetchPage(ctx context.Context, q Query) (eventPage, error) {
	var raw providerEvents
	if err := s.get(ctx, "/events.json", q.upstream(), &raw); err != nil {
		return eventPage{}, err
	}
	if raw.Page == nil || raw.Page.Number != q.Page || raw.Page.TotalPages < 0 || raw.Page.TotalElements < 0 || len(raw.Embedded.Events) > q.Limit || (len(raw.Embedded.Events) == 0 && q.Page < raw.Page.TotalPages && raw.Page.TotalElements > 0) {
		return eventPage{}, s.unavailable(30 * time.Second)
	}
	page := eventPage{Items: []models.Event{}, Total: raw.Page.TotalElements, TotalPages: raw.Page.TotalPages}
	for _, event := range raw.Embedded.Events {
		if !sourceIDPattern.MatchString(event.ID) || event.Name == "" {
			return eventPage{}, s.unavailable(30 * time.Second)
		}
		page.Items = append(page.Items, normalizeEvent(event))
	}
	now := s.now()
	for _, event := range page.Items {
		writeRecord(ctx, s, detailKey(event.ID), cacheRecord[models.Event]{
			Data: event, FetchedAt: now, FreshUntil: now.Add(eventPolicy.fresh), StaleUntil: now.Add(eventPolicy.retain),
		})
	}
	return page, nil
}

// Collect deliberately bypasses retained reads: cache fallback cannot establish
// collection coverage or advance durable last-seen times. It shares the same
// client, budget, cooldowns, validation and normalization as interactive reads.
func (s *Service) Collect(ctx context.Context, q Query) (models.EventList, error) {
	validated, err := ParseQuery(q.Values(), s.now())
	if err != nil {
		return models.EventList{}, err
	}
	if q.Page < 0 || q.Page > 999/validated.Limit {
		return models.EventList{}, &ValidationError{"cursor", "page exceeds the provider result limit"}
	}
	validated.Page = q.Page
	page, err := s.fetchPage(ctx, validated)
	if err != nil {
		return models.EventList{}, err
	}
	now := s.now()
	writeRecord(ctx, s, validated.cacheKey(), cacheRecord[eventPage]{Data: page, FetchedAt: now, FreshUntil: now.Add(eventPolicy.fresh), StaleUntil: now.Add(eventPolicy.retain)})
	return eventList(validated, page, models.Freshness{DataAsOf: now}), nil
}

func detailKey(id string) string { return "ticketmaster:v1:event:" + id }

func (s *Service) Event(ctx context.Context, id string) (models.EventDetail, error) {
	rawID, err := sourceID(id)
	if err != nil {
		return models.EventDetail{}, err
	}
	event, meta, err := cached(ctx, s, detailKey(id), eventPolicy, func(ctx context.Context) (models.Event, error) {
		var raw providerEvent
		if err := s.get(ctx, "/events/"+rawID+".json", url.Values{}, &raw); err != nil {
			return models.Event{}, err
		}
		if raw.ID != rawID || raw.Name == "" {
			return models.Event{}, s.unavailable(30 * time.Second)
		}
		return normalizeEvent(raw), nil
	})
	return models.EventDetail{Item: event, Meta: meta}, err
}

func (s *Service) Genres(ctx context.Context) (models.GenreList, error) {
	items, meta, err := cached(ctx, s, "ticketmaster:v1:genres:music:en", genrePolicy, func(ctx context.Context) ([]models.Genre, error) {
		var raw providerSegment
		if err := s.get(ctx, "/classifications/segments/"+musicSegment+".json", url.Values{}, &raw); err != nil {
			return nil, err
		}
		if raw.ID != musicSegment {
			return nil, s.unavailable(30 * time.Second)
		}
		return normalizedGenres(raw), nil
	})
	return models.GenreList{Items: items, Meta: meta}, err
}

// Categories exposes Ticketmaster segments with their compatible genres. The
// classifications collection also contains types, which are not event categories.
func (s *Service) Categories(ctx context.Context) (models.CategoryList, error) {
	items, meta, err := cached(ctx, s, "ticketmaster:v1:categories:en", genrePolicy, func(ctx context.Context) ([]models.Category, error) {
		items := []models.Category{}
		seen := map[string]bool{}
		for page := 0; ; page++ {
			var raw providerClassifications
			if err := s.get(ctx, "/classifications.json", url.Values{"size": {"100"}, "page": {strconv.Itoa(page)}}, &raw); err != nil {
				return nil, err
			}
			// Bound the full-catalog fetch and never cache a truncated catalog.
			if raw.Page == nil || raw.Page.Number != page || raw.Page.TotalPages < 1 || raw.Page.TotalPages > 10 {
				return nil, s.unavailable(30 * time.Second)
			}
			for _, classification := range raw.Embedded.Classifications {
				segment := classification.Segment
				if segment == nil {
					continue
				}
				if !sourceIDPattern.MatchString(segment.ID) || segment.Name == "" {
					return nil, s.unavailable(30 * time.Second)
				}
				if !seen[segment.ID] {
					seen[segment.ID] = true
					items = append(items, models.Category{NamedID: models.NamedID{ID: segment.ID, Name: segment.Name}, Genres: normalizedGenres(*segment)})
				}
			}
			if page+1 >= raw.Page.TotalPages {
				break
			}
		}
		if len(items) == 0 {
			return nil, s.unavailable(30 * time.Second)
		}
		sort.Slice(items, func(i, j int) bool {
			if items[i].Name == items[j].Name {
				return items[i].ID < items[j].ID
			}
			return items[i].Name < items[j].Name
		})
		return items, nil
	})
	return models.CategoryList{Items: items, Meta: meta}, err
}

func normalizedGenres(raw providerSegment) []models.Genre {
	items := make([]models.Genre, 0, len(raw.Embedded.Genres))
	for _, genre := range raw.Embedded.Genres {
		items = append(items, models.Genre{NamedID: genre.NamedID, Subgenres: nonNil(genre.Embedded.Subgenres)})
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].Name == items[j].Name {
			return items[i].ID < items[j].ID
		}
		return items[i].Name < items[j].Name
	})
	return items
}

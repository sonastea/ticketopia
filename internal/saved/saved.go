// Package saved owns private bookmarks. Callers supply a verified account ID;
// event metadata is shared, but bookmark state never enters public discovery DTOs.
package saved

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/url"
	"regexp"
	"strconv"
	"time"

	"github.com/sonastea/ticketopia/internal/accounts"
	"github.com/sonastea/ticketopia/internal/discovery"
	"github.com/sonastea/ticketopia/internal/models"
)

var ErrNotFound = errors.New("saved event not found")
var ErrUnavailable = errors.New("saved events unavailable")
var eventID = regexp.MustCompile(`^ticketmaster:[A-Za-z0-9_-]{1,128}$`)

type Item struct {
	Event   models.Event     `json:"event"`
	SavedAt time.Time        `json:"saved_at"`
	Meta    models.Freshness `json:"meta"`
}
type List struct {
	Items      []Item  `json:"items"`
	NextCursor *string `json:"next_cursor"`
}
type Cursor struct {
	Version int       `json:"v"`
	Owner   string    `json:"owner"`
	Before  time.Time `json:"before"`
	ID      string    `json:"id"`
}
type Query struct {
	Limit  int
	Cursor *Cursor
}
type Repository interface {
	Get(context.Context, string, string) (Item, error)
	Save(context.Context, string, models.EventDetail) (Item, bool, error)
	Remove(context.Context, string, string) error
	List(context.Context, string, Query) ([]Item, error)
	States(context.Context, string, []string) (map[string]bool, error)
	Snapshot(context.Context, string) (models.EventDetail, error)
}
type Discovery interface {
	Event(context.Context, string) (models.EventDetail, error)
}
type Service struct {
	repo      Repository
	discovery Discovery
}

func New(repo Repository, discovery Discovery) *Service { return &Service{repo, discovery} }

func ValidateID(id string) error {
	if !eventID.MatchString(id) {
		return &accounts.ValidationError{Field: "event_id", Message: "use a Ticketmaster event ID"}
	}
	return nil
}
func ownerID(owner string) error {
	if !accounts.ValidID(owner) {
		return accounts.ErrUnauthenticated
	}
	return nil
}

// Detail prefers live/cache reads; a durable last-known snapshot survives source
// disappearance. It contains no viewer state and is safe for public event reads.
func (s *Service) Detail(ctx context.Context, id string) (models.EventDetail, error) {
	if err := ValidateID(id); err != nil {
		return models.EventDetail{}, err
	}
	var detail models.EventDetail
	var err error = discovery.ErrNotFound
	if s.discovery != nil {
		detail, err = s.discovery.Event(ctx, id)
	}
	if err == nil {
		return detail, nil
	}
	stored, storedErr := s.repo.Snapshot(ctx, id)
	if storedErr == nil {
		stored.Meta.Stale = true
		return stored, nil
	}
	if !errors.Is(storedErr, ErrNotFound) {
		return models.EventDetail{}, ErrUnavailable
	}
	return models.EventDetail{}, err
}
func (s *Service) Save(ctx context.Context, owner, id string) (Item, bool, error) {
	if err := ownerID(owner); err != nil {
		return Item{}, false, err
	}
	if err := ValidateID(id); err != nil {
		return Item{}, false, err
	}
	item, err := s.repo.Get(ctx, owner, id)
	if err == nil {
		return item, false, nil
	}
	if !errors.Is(err, ErrNotFound) {
		return Item{}, false, err
	}
	detail, err := s.Detail(ctx, id)
	if err != nil {
		return Item{}, false, err
	}
	if detail.Item.ID != id {
		return Item{}, false, ErrUnavailable
	}
	return s.repo.Save(ctx, owner, detail)
}
func (s *Service) Remove(ctx context.Context, owner, id string) error {
	if err := ownerID(owner); err != nil {
		return err
	}
	if err := ValidateID(id); err != nil {
		return err
	}
	return s.repo.Remove(ctx, owner, id)
}
func (s *Service) Get(ctx context.Context, owner, id string) (Item, error) {
	if err := ownerID(owner); err != nil {
		return Item{}, err
	}
	if err := ValidateID(id); err != nil {
		return Item{}, err
	}
	return s.repo.Get(ctx, owner, id)
}
func (s *Service) States(ctx context.Context, owner string, ids []string) (map[string]bool, error) {
	if err := ownerID(owner); err != nil {
		return nil, err
	}
	if len(ids) > 101 {
		return nil, &accounts.ValidationError{Field: "events", Message: "use at most 101 events"}
	}
	for _, id := range ids {
		if err := ValidateID(id); err != nil {
			return nil, err
		}
	}
	return s.repo.States(ctx, owner, ids)
}
func ParseQuery(values url.Values, owner string) (Query, error) {
	q := Query{Limit: 20}
	for key, entries := range values {
		if (key != "limit" && key != "cursor") || len(entries) != 1 {
			return q, &accounts.ValidationError{Field: key, Message: "use one value for limit or cursor"}
		}
	}
	if raw, ok := values["limit"]; ok {
		n, err := strconv.Atoi(raw[0])
		if err != nil || n < 1 || n > 100 {
			return q, &accounts.ValidationError{Field: "limit", Message: "use an integer from 1 to 100"}
		}
		q.Limit = n
	}
	if raw, ok := values["cursor"]; ok {
		if len(raw[0]) > 512 {
			return q, &accounts.ValidationError{Field: "cursor", Message: "use the next cursor from your saved collection"}
		}
		var c Cursor
		data, err := base64.RawURLEncoding.Strict().DecodeString(raw[0])
		if err != nil || json.Unmarshal(data, &c) != nil || c.Version != 1 || c.Owner != owner || c.Before.IsZero() || c.Before.Year() < 1000 || c.Before.Year() > 9999 || ValidateID(c.ID) != nil {
			return q, &accounts.ValidationError{Field: "cursor", Message: "use the next cursor from your saved collection"}
		}
		q.Cursor = &c
	}
	return q, nil
}
func (s *Service) List(ctx context.Context, owner string, values url.Values) (List, error) {
	if err := ownerID(owner); err != nil {
		return List{}, err
	}
	q, err := ParseQuery(values, owner)
	if err != nil {
		return List{}, err
	}
	items, err := s.repo.List(ctx, owner, q)
	if err != nil {
		return List{}, err
	}
	result := List{Items: items}
	if result.Items == nil {
		result.Items = []Item{}
	}
	if len(items) > q.Limit {
		result.Items = items[:q.Limit]
		last := result.Items[q.Limit-1]
		data, _ := json.Marshal(Cursor{1, owner, last.SavedAt.UTC(), last.Event.ID})
		next := base64.RawURLEncoding.EncodeToString(data)
		result.NextCursor = &next
	}
	return result, nil
}

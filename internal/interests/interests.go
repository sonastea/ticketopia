// Package interests owns reversible event interest, independently of bookmarks.
// Only explicitly public choices may disclose an account's identity.
package interests

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/url"
	"strconv"
	"time"

	"github.com/sonastea/ticketopia/internal/accounts"
	"github.com/sonastea/ticketopia/internal/models"
	"github.com/sonastea/ticketopia/internal/saved"
)

var ErrNotFound = errors.New("event interest not found")
var ErrUnavailable = errors.New("event interest unavailable")

type Item struct {
	Event        models.Event     `json:"event"`
	InterestedAt time.Time        `json:"interested_at"`
	Visibility   string           `json:"visibility"`
	Meta         models.Freshness `json:"meta"`
}
type List struct {
	Items      []Item  `json:"items"`
	NextCursor *string `json:"next_cursor"`
}
type State struct {
	Count      int    `json:"count"`
	Interested bool   `json:"interested"`
	Visibility string `json:"visibility,omitempty"`
}
type Participant struct {
	Profile      accounts.PublicProfile `json:"profile"`
	InterestedAt time.Time              `json:"interested_at"`
}
type Participants struct {
	Items      []Participant `json:"items"`
	NextCursor *string       `json:"next_cursor"`
}
type Cursor struct {
	Scope  string    `json:"scope"`
	Before time.Time `json:"before"`
	ID     string    `json:"id"`
}
type Query struct {
	Limit  int
	Cursor *Cursor
}
type Repository interface {
	Get(context.Context, string, string) (Item, error)
	Set(context.Context, string, models.EventDetail, string) (Item, bool, error)
	Remove(context.Context, string, string) error
	States(context.Context, string, []string) (map[string]State, error)
	List(context.Context, string, bool, Query) ([]Item, error)
	Participants(context.Context, string, Query) ([]Participant, error)
}
type Details interface {
	Detail(context.Context, string) (models.EventDetail, error)
}
type Service struct {
	repo    Repository
	details Details
}

func New(repo Repository, details Details) *Service { return &Service{repo, details} }
func Visibility(value string) error {
	if value != "private" && value != "public" {
		return &accounts.ValidationError{Field: "visibility", Message: "choose private or public"}
	}
	return nil
}
func ownerID(id string) error {
	if !accounts.ValidID(id) {
		return accounts.ErrUnauthenticated
	}
	return nil
}
func (s *Service) Get(ctx context.Context, owner, id string) (Item, error) {
	if err := ownerID(owner); err != nil {
		return Item{}, err
	}
	if err := saved.ValidateID(id); err != nil {
		return Item{}, err
	}
	return s.repo.Get(ctx, owner, id)
}

// Omitted visibility preserves an existing choice and uses the profile default
// only for a new choice. Retries do not need a provider or reset the timestamp.
func (s *Service) Set(ctx context.Context, owner, id string, visibility *string, defaultVisibility string) (Item, bool, error) {
	if err := ownerID(owner); err != nil {
		return Item{}, false, err
	}
	if err := saved.ValidateID(id); err != nil {
		return Item{}, false, err
	}
	if visibility != nil {
		if err := Visibility(*visibility); err != nil {
			return Item{}, false, err
		}
	}
	item, err := s.repo.Get(ctx, owner, id)
	if err != nil && !errors.Is(err, ErrNotFound) {
		return Item{}, false, err
	}
	value := defaultVisibility
	if err == nil {
		value = item.Visibility
	}
	if visibility != nil {
		value = *visibility
	}
	if err := Visibility(value); err != nil {
		return Item{}, false, err
	}
	if err == nil && value == item.Visibility {
		return item, false, nil
	}
	detail := models.EventDetail{Item: item.Event, Meta: item.Meta}
	if errors.Is(err, ErrNotFound) {
		if s.details == nil {
			return Item{}, false, ErrUnavailable
		}
		detail, err = s.details.Detail(ctx, id)
		if err != nil {
			return Item{}, false, err
		}
	}
	if detail.Item.ID != id {
		return Item{}, false, ErrUnavailable
	}
	return s.repo.Set(ctx, owner, detail, value)
}
func (s *Service) Remove(ctx context.Context, owner, id string) error {
	if err := ownerID(owner); err != nil {
		return err
	}
	if err := saved.ValidateID(id); err != nil {
		return err
	}
	return s.repo.Remove(ctx, owner, id)
}
func (s *Service) States(ctx context.Context, owner string, ids []string) (map[string]State, error) {
	if owner != "" {
		if err := ownerID(owner); err != nil {
			return nil, err
		}
	}
	if len(ids) > 101 {
		return nil, &accounts.ValidationError{Field: "event_id", Message: "use at most 101 events"}
	}
	for _, id := range ids {
		if err := saved.ValidateID(id); err != nil {
			return nil, err
		}
	}
	return s.repo.States(ctx, owner, ids)
}
func parseQuery(values url.Values, scope string, participants bool) (Query, error) {
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
			return q, &accounts.ValidationError{Field: "cursor", Message: "use the next cursor from this interest collection"}
		}
		var cursor Cursor
		data, err := base64.RawURLEncoding.Strict().DecodeString(raw[0])
		valid := err == nil && json.Unmarshal(data, &cursor) == nil && cursor.Scope == scope && !cursor.Before.IsZero() && cursor.Before.Year() >= 1000 && cursor.Before.Year() <= 9999
		if participants {
			valid = valid && accounts.ValidID(cursor.ID)
		} else {
			valid = valid && saved.ValidateID(cursor.ID) == nil
		}
		if !valid {
			return q, &accounts.ValidationError{Field: "cursor", Message: "use the next cursor from this interest collection"}
		}
		q.Cursor = &cursor
	}
	return q, nil
}
func nextCursor(scope string, before time.Time, id string) *string {
	data, _ := json.Marshal(Cursor{scope, before.UTC(), id})
	next := base64.RawURLEncoding.EncodeToString(data)
	return &next
}
func (s *Service) List(ctx context.Context, owner string, public bool, values url.Values) (List, error) {
	if err := ownerID(owner); err != nil {
		return List{}, err
	}
	scope := "own:" + owner
	if public {
		scope = "public:" + owner
	}
	q, err := parseQuery(values, scope, false)
	if err != nil {
		return List{}, err
	}
	items, err := s.repo.List(ctx, owner, public, q)
	if err != nil {
		return List{}, err
	}
	result := List{Items: items}
	if items == nil {
		result.Items = []Item{}
	}
	if len(items) > q.Limit {
		result.Items = items[:q.Limit]
		last := result.Items[q.Limit-1]
		result.NextCursor = nextCursor(scope, last.InterestedAt, last.Event.ID)
	}
	return result, nil
}
func (s *Service) Participants(ctx context.Context, id string, values url.Values) (Participants, error) {
	if err := saved.ValidateID(id); err != nil {
		return Participants{}, err
	}
	scope := "event:" + id
	q, err := parseQuery(values, scope, true)
	if err != nil {
		return Participants{}, err
	}
	items, err := s.repo.Participants(ctx, id, q)
	if err != nil {
		return Participants{}, err
	}
	result := Participants{Items: items}
	if items == nil {
		result.Items = []Participant{}
	}
	if len(items) > q.Limit {
		result.Items = items[:q.Limit]
		last := result.Items[q.Limit-1]
		result.NextCursor = nextCursor(scope, last.InterestedAt, last.Profile.ID)
	}
	return result, nil
}

// Package follows owns private artist/venue subscriptions, independently of
// event interest, saves, public profiles and notification delivery.
package follows

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

var ErrNotFound = errors.New("follow target not found")
var ErrUnavailable = errors.New("follows unavailable")
var targetID = regexp.MustCompile(`^ticketmaster:[A-Za-z0-9_-]{1,128}$`)

type Item struct {
	Target     models.FollowTarget `json:"target"`
	FollowedAt time.Time           `json:"followed_at"`
	Meta       models.Freshness    `json:"meta"`
}
type List struct {
	Items      []Item  `json:"items"`
	NextCursor *string `json:"next_cursor"`
}
type Cursor struct {
	Version int       `json:"v"`
	Owner   string    `json:"owner"`
	Filter  string    `json:"filter"`
	Before  time.Time `json:"before"`
	Kind    string    `json:"kind"`
	ID      string    `json:"id"`
}
type Query struct {
	Kind   string
	Limit  int
	Cursor *Cursor
}
type Repository interface {
	Get(context.Context, string, string, string) (Item, error)
	Follow(context.Context, string, models.CatalogDetail) (Item, bool, error)
	Remove(context.Context, string, string, string) error
	List(context.Context, string, Query) ([]Item, error)
	States(context.Context, string, string, []string) (map[string]bool, error)
	Snapshot(context.Context, string, string) (models.CatalogDetail, error)
}
type Discovery interface {
	CatalogDetail(context.Context, string, string) (models.CatalogDetail, error)
}
type Service struct {
	repo      Repository
	discovery Discovery
}

func New(repo Repository, discovery Discovery) *Service { return &Service{repo, discovery} }
func Validate(kind, id string) error {
	if kind != "artist" && kind != "venue" {
		return &accounts.ValidationError{Field: "kind", Message: "choose artist or venue"}
	}
	if !targetID.MatchString(id) {
		return &accounts.ValidationError{Field: "target_id", Message: "use a Ticketmaster artist or venue ID"}
	}
	return nil
}
func ownerID(owner string) error {
	if !accounts.ValidID(owner) {
		return accounts.ErrUnauthenticated
	}
	return nil
}
func (s *Service) Follow(ctx context.Context, owner, kind, id string) (Item, bool, error) {
	if err := ownerID(owner); err != nil {
		return Item{}, false, err
	}
	if err := Validate(kind, id); err != nil {
		return Item{}, false, err
	}
	item, err := s.repo.Get(ctx, owner, kind, id)
	if err == nil {
		return item, false, nil
	}
	if !errors.Is(err, ErrNotFound) {
		return Item{}, false, err
	}
	var detail models.CatalogDetail
	err = discovery.ErrNotFound
	if s.discovery != nil {
		detail, err = s.discovery.CatalogDetail(ctx, kind, id)
	}
	if err != nil {
		stored, storedErr := s.repo.Snapshot(ctx, kind, id)
		if storedErr == nil {
			detail, err = stored, nil
		} else if !errors.Is(storedErr, ErrNotFound) {
			return Item{}, false, ErrUnavailable
		}
	}
	if errors.Is(err, discovery.ErrNotFound) {
		return Item{}, false, ErrNotFound
	}
	if err != nil {
		return Item{}, false, ErrUnavailable
	}
	if detail.Item.ID != id || detail.Item.Kind != kind {
		return Item{}, false, ErrUnavailable
	}
	return s.repo.Follow(ctx, owner, detail)
}
func (s *Service) Get(ctx context.Context, owner, kind, id string) (Item, error) {
	if err := ownerID(owner); err != nil {
		return Item{}, err
	}
	if err := Validate(kind, id); err != nil {
		return Item{}, err
	}
	return s.repo.Get(ctx, owner, kind, id)
}
func (s *Service) Remove(ctx context.Context, owner, kind, id string) error {
	if err := ownerID(owner); err != nil {
		return err
	}
	if err := Validate(kind, id); err != nil {
		return err
	}
	return s.repo.Remove(ctx, owner, kind, id)
}
func (s *Service) States(ctx context.Context, owner, kind string, ids []string) (map[string]bool, error) {
	if err := ownerID(owner); err != nil {
		return nil, err
	}
	if len(ids) > 100 {
		return nil, &accounts.ValidationError{Field: "targets", Message: "use at most 100 targets"}
	}
	if kind != "artist" && kind != "venue" {
		return nil, &accounts.ValidationError{Field: "kind", Message: "choose artist or venue"}
	}
	for _, id := range ids {
		if err := Validate(kind, id); err != nil {
			return nil, err
		}
	}
	return s.repo.States(ctx, owner, kind, ids)
}
func ParseQuery(values url.Values, owner string) (Query, error) {
	q := Query{Limit: 20, Kind: values.Get("kind")}
	for key, entries := range values {
		if len(entries) != 1 || (key != "kind" && key != "limit" && key != "cursor") {
			return q, &accounts.ValidationError{Field: key, Message: "use one value for kind, limit or cursor"}
		}
	}
	if q.Kind != "" && q.Kind != "artist" && q.Kind != "venue" {
		return q, &accounts.ValidationError{Field: "kind", Message: "choose artist or venue, or omit kind for all follows"}
	}
	if raw, ok := values["limit"]; ok {
		n, err := strconv.Atoi(raw[0])
		if err != nil || n < 1 || n > 100 {
			return q, &accounts.ValidationError{Field: "limit", Message: "use an integer from 1 to 100"}
		}
		q.Limit = n
	}
	if raw, ok := values["cursor"]; ok {
		var c Cursor
		data, err := base64.RawURLEncoding.Strict().DecodeString(raw[0])
		if len(raw[0]) > 768 || err != nil || json.Unmarshal(data, &c) != nil || c.Version != 1 || c.Owner != owner || c.Filter != q.Kind || c.Before.IsZero() || c.Before.Year() < 1000 || c.Before.Year() > 9999 || Validate(c.Kind, c.ID) != nil || (q.Kind != "" && c.Kind != q.Kind) {
			return q, &accounts.ValidationError{Field: "cursor", Message: "use the next cursor from your follows with the same kind filter"}
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
	list := List{Items: items}
	if list.Items == nil {
		list.Items = []Item{}
	}
	if len(items) > q.Limit {
		list.Items = items[:q.Limit]
		last := list.Items[q.Limit-1]
		data, _ := json.Marshal(Cursor{1, owner, q.Kind, last.FollowedAt.UTC(), last.Target.Kind, last.Target.ID})
		next := base64.RawURLEncoding.EncodeToString(data)
		list.NextCursor = &next
	}
	return list, nil
}

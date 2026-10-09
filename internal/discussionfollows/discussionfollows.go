// Package discussionfollows owns private conversation subscriptions and in-app
// reply notifications. Notification payloads deliberately contain no post text.
package discussionfollows

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/url"
	"strconv"
	"time"

	"github.com/sonastea/ticketopia/internal/accounts"
	"github.com/sonastea/ticketopia/internal/discussions"
	"github.com/sonastea/ticketopia/internal/saved"
)

var ErrUnavailable = errors.New("followed discussions unavailable")

type Follow struct {
	EventID   string    `json:"event_id"`
	ThreadID  string    `json:"thread_id"`
	Frequency string    `json:"frequency"`
	CreatedAt time.Time `json:"created_at"`
	// Root uses the same moderation-filtered projection as public discussions.
	Root discussions.Post `json:"root"`
}
type Notification struct {
	ID          string    `json:"id"`
	EventID     string    `json:"event_id"`
	ThreadID    string    `json:"thread_id"`
	PostID      string    `json:"post_id"`
	EventName   string    `json:"event_name"`
	ReplyCount  int       `json:"reply_count"`
	AvailableAt time.Time `json:"available_at"`
	Read        bool      `json:"read"`
	URL         string    `json:"url"`
}
type Page[T any] struct {
	Items      []T     `json:"items"`
	NextCursor *string `json:"next_cursor"`
}
type Cursor struct {
	Scope string    `json:"scope"`
	At    time.Time `json:"at"`
	ID    string    `json:"id"`
}
type Query struct {
	Limit  int
	Cursor *Cursor
}
type Repository interface {
	Get(context.Context, string, string, string) (Follow, error)
	Set(context.Context, string, string, string, string) (Follow, error)
	Remove(context.Context, string, string, string) error
	List(context.Context, string, Query) ([]Follow, error)
	Notifications(context.Context, string, Query) ([]Notification, error)
	Read(context.Context, string, string) error
}
type Service struct{ repo Repository }

func New(repo Repository) *Service { return &Service{repo} }
func valid(who, event, thread string) error {
	if !accounts.ValidID(who) {
		return accounts.ErrUnauthenticated
	}
	if err := saved.ValidateID(event); err != nil {
		return err
	}
	return discussions.PostID(thread)
}
func Frequency(value string) error {
	switch value {
	case "immediate", "daily", "weekly", "muted":
		return nil
	}
	return &accounts.ValidationError{Field: "frequency", Message: "choose immediate, daily, weekly, or muted"}
}

// DeliveryAt uses fixed UTC digest boundaries, independent of application clocks.
func DeliveryAt(frequency string, at time.Time) time.Time {
	at = at.UTC()
	day := time.Date(at.Year(), at.Month(), at.Day(), 0, 0, 0, 0, time.UTC)
	switch frequency {
	case "daily":
		return day.AddDate(0, 0, 1)
	case "weekly":
		return day.AddDate(0, 0, 7-(int(day.Weekday())+6)%7)
	default:
		return at
	}
}
func (s *Service) Get(ctx context.Context, who, event, thread string) (Follow, error) {
	if err := valid(who, event, thread); err != nil {
		return Follow{}, err
	}
	return s.repo.Get(ctx, who, event, thread)
}
func (s *Service) Set(ctx context.Context, who, event, thread, frequency string) (Follow, error) {
	if err := valid(who, event, thread); err != nil {
		return Follow{}, err
	}
	if err := Frequency(frequency); err != nil {
		return Follow{}, err
	}
	return s.repo.Set(ctx, who, event, thread, frequency)
}
func (s *Service) Remove(ctx context.Context, who, event, thread string) error {
	if err := valid(who, event, thread); err != nil {
		return err
	}
	return s.repo.Remove(ctx, who, event, thread)
}
func ParseQuery(values url.Values, scope string) (Query, error) {
	q := Query{Limit: 20}
	bad := func(field string) (Query, error) {
		return q, &accounts.ValidationError{Field: field, Message: "use limit 1–100 and the next cursor from this private collection"}
	}
	for key, values := range values {
		if (key != "limit" && key != "cursor") || len(values) != 1 {
			return bad(key)
		}
	}
	if raw, ok := values["limit"]; ok {
		n, err := strconv.Atoi(raw[0])
		if err != nil || n < 1 || n > 100 {
			return bad("limit")
		}
		q.Limit = n
	}
	if raw, ok := values["cursor"]; ok {
		if len(raw[0]) > 1024 {
			return bad("cursor")
		}
		data, err := base64.RawURLEncoding.Strict().DecodeString(raw[0])
		var c Cursor
		if err != nil || json.Unmarshal(data, &c) != nil || c.Scope != scope || !accounts.ValidID(c.ID) || c.At.IsZero() || c.At.Year() < 1000 || c.At.Year() > 9999 {
			return bad("cursor")
		}
		q.Cursor = &c
	}
	return q, nil
}
func page[T any](items []T, q Query, scope string, last func(T) (time.Time, string)) Page[T] {
	p := Page[T]{Items: items}
	if items == nil {
		p.Items = []T{}
	}
	if len(items) > q.Limit {
		p.Items = items[:q.Limit]
		at, id := last(p.Items[q.Limit-1])
		data, _ := json.Marshal(Cursor{scope, at.UTC(), id})
		next := base64.RawURLEncoding.EncodeToString(data)
		p.NextCursor = &next
	}
	return p
}
func (s *Service) List(ctx context.Context, who string, values url.Values) (Page[Follow], error) {
	if !accounts.ValidID(who) {
		return Page[Follow]{}, accounts.ErrUnauthenticated
	}
	scope := "discussion-follows:" + who
	q, err := ParseQuery(values, scope)
	if err != nil {
		return Page[Follow]{}, err
	}
	items, err := s.repo.List(ctx, who, q)
	if err != nil {
		return Page[Follow]{}, err
	}
	return page(items, q, scope, func(f Follow) (time.Time, string) { return f.CreatedAt, f.ThreadID }), nil
}
func (s *Service) Notifications(ctx context.Context, who string, values url.Values) (Page[Notification], error) {
	if !accounts.ValidID(who) {
		return Page[Notification]{}, accounts.ErrUnauthenticated
	}
	scope := "discussion-notifications:" + who
	q, err := ParseQuery(values, scope)
	if err != nil {
		return Page[Notification]{}, err
	}
	items, err := s.repo.Notifications(ctx, who, q)
	if err != nil {
		return Page[Notification]{}, err
	}
	for i := range items {
		n := &items[i]
		n.URL = "/events/" + url.PathEscape(n.EventID) + "/discussions/" + n.ThreadID + "?" + url.Values{"post_id": {n.PostID}}.Encode() + "#post-" + n.PostID
	}
	return page(items, q, scope, func(n Notification) (time.Time, string) { return n.AvailableAt, n.ID }), nil
}
func (s *Service) Read(ctx context.Context, who, id string) error {
	if !accounts.ValidID(who) {
		return accounts.ErrUnauthenticated
	}
	if err := discussions.PostID(id); err != nil {
		return err
	}
	return s.repo.Read(ctx, who, id)
}

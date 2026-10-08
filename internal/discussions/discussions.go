// Package discussions owns public, asynchronous event conversations and Helpful
// reactions. Saves, interest, and recommendations remain independent resources.
package discussions

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/sonastea/ticketopia/internal/accounts"
	"github.com/sonastea/ticketopia/internal/models"
	"github.com/sonastea/ticketopia/internal/recommendations"
	"github.com/sonastea/ticketopia/internal/saved"
)

var (
	ErrNotFound    = errors.New("discussion post not found")
	ErrUnavailable = errors.New("discussions unavailable")
	ErrConflict    = errors.New("idempotency key already used for another contribution")
)

const MaxBody = 4000

type Post struct {
	ID           string                  `json:"id"`
	EventID      string                  `json:"event_id"`
	ThreadID     string                  `json:"thread_id"`
	ParentID     string                  `json:"parent_id,omitempty"`
	ParentName   string                  `json:"parent_name,omitempty"`
	Profile      *accounts.PublicProfile `json:"profile,omitempty"`
	Body         string                  `json:"body"`
	CreatedAt    time.Time               `json:"created_at"`
	UpdatedAt    time.Time               `json:"updated_at"`
	Removed      bool                    `json:"removed"`
	ReplyCount   int                     `json:"reply_count"`
	HelpfulCount int                     `json:"helpful_count"`
	Helpful      bool                    `json:"helpful"`
	Event        models.Event            `json:"event"`
	Meta         models.Freshness        `json:"meta"`
}
type List struct {
	Items      []Post  `json:"items"`
	NextCursor *string `json:"next_cursor"`
	Count      int     `json:"count"`
}
type Cursor struct {
	Scope    string    `json:"scope"`
	At       time.Time `json:"at"`
	ID       string    `json:"id"`
	Snapshot time.Time `json:"snapshot"`
}
type Query struct {
	Limit                     int
	City, Country, CategoryID string
	Cursor                    *Cursor
	Snapshot                  time.Time
}
type Create struct {
	Owner, EventID, ThreadID, ParentID, Body, Key string
	Hash                                          [32]byte
}
type Repository interface {
	Snapshot(context.Context) (time.Time, error)
	Retry(context.Context, string, string, [32]byte) (Post, error)
	Create(context.Context, Create, *models.EventDetail) (Post, bool, error)
	Get(context.Context, string, string) (Post, error)
	List(context.Context, string, string, string, Query) ([]Post, int, error)
	Edit(context.Context, string, string, string) (Post, error)
	Remove(context.Context, string, string) error
	React(context.Context, string, string, bool) (Post, error)
}
type Details interface {
	Detail(context.Context, string) (models.EventDetail, error)
}
type Service struct {
	repo    Repository
	details Details
}

func New(repo Repository, details Details) *Service { return &Service{repo, details} }
func invalid(field, message string) error {
	return &accounts.ValidationError{Field: field, Message: message}
}
func Body(value string) (string, error) {
	if !utf8.ValidString(value) || utf8.RuneCountInString(value) > MaxBody || strings.ContainsFunc(value, func(r rune) bool { return unicode.IsControl(r) && r != '\n' && r != '\r' && r != '\t' }) {
		return "", invalid("body", "use plain text from 1 to 4000 characters")
	}
	value = strings.TrimSpace(value)
	if value == "" {
		return "", invalid("body", "write a question, tip, or reply")
	}
	return value, nil
}

var keyPattern = regexp.MustCompile(`^[A-Za-z0-9_-]{16,128}$`)

func Key(key string) error {
	if !keyPattern.MatchString(key) {
		return invalid("idempotency_key", "use a unique 16–128 character key containing letters, digits, hyphens, or underscores")
	}
	return nil
}
func PostID(id string) error {
	if !accounts.ValidID(id) {
		return invalid("post_id", "use a valid discussion post ID")
	}
	return nil
}
func owner(id string) error {
	if !accounts.ValidID(id) {
		return accounts.ErrUnauthenticated
	}
	return nil
}
func (s *Service) Create(ctx context.Context, who, event, thread, parent, body, key string) (Post, bool, error) {
	if err := owner(who); err != nil {
		return Post{}, false, err
	}
	if err := saved.ValidateID(event); err != nil {
		return Post{}, false, err
	}
	if thread != "" {
		if err := PostID(thread); err != nil {
			return Post{}, false, err
		}
	}
	if parent != "" {
		if thread == "" {
			return Post{}, false, invalid("parent_id", "replies require a root thread")
		}
		if err := PostID(parent); err != nil {
			return Post{}, false, err
		}
	}
	if err := Key(key); err != nil {
		return Post{}, false, err
	}
	body, err := Body(body)
	if err != nil {
		return Post{}, false, err
	}
	data, _ := json.Marshal([]string{event, thread, parent, body})
	hash := sha256.Sum256(data)
	post, err := s.repo.Retry(ctx, who, key, hash)
	if err == nil {
		return post, false, nil
	}
	if !errors.Is(err, ErrNotFound) {
		return Post{}, false, err
	}
	var detail *models.EventDetail
	if thread == "" {
		if s.details == nil {
			return Post{}, false, ErrUnavailable
		}
		d, err := s.details.Detail(ctx, event)
		if err != nil {
			return Post{}, false, err
		}
		if d.Item.ID != event {
			return Post{}, false, ErrUnavailable
		}
		detail = &d
	}
	return s.repo.Create(ctx, Create{who, event, thread, parent, body, key, hash}, detail)
}
func (s *Service) Thread(ctx context.Context, viewer, event, id string) (Post, error) {
	if err := saved.ValidateID(event); err != nil {
		return Post{}, err
	}
	if err := PostID(id); err != nil {
		return Post{}, err
	}
	p, err := s.repo.Get(ctx, viewer, id)
	if err == nil && (p.EventID != event || p.ThreadID != p.ID) {
		return Post{}, ErrNotFound
	}
	return p, err
}
func (s *Service) Edit(ctx context.Context, who, id, body string) (Post, error) {
	if err := owner(who); err != nil {
		return Post{}, err
	}
	if err := PostID(id); err != nil {
		return Post{}, err
	}
	body, err := Body(body)
	if err != nil {
		return Post{}, err
	}
	return s.repo.Edit(ctx, who, id, body)
}
func (s *Service) Parent(ctx context.Context, viewer, event, thread, id string) (Post, error) {
	if err := saved.ValidateID(event); err != nil {
		return Post{}, err
	}
	if err := PostID(thread); err != nil {
		return Post{}, err
	}
	if err := PostID(id); err != nil {
		return Post{}, err
	}
	p, err := s.repo.Get(ctx, viewer, id)
	if err == nil && (p.EventID != event || p.ThreadID != thread) {
		return Post{}, ErrNotFound
	}
	return p, err
}
func (s *Service) Remove(ctx context.Context, who, id string) error {
	if err := owner(who); err != nil {
		return err
	}
	if err := PostID(id); err != nil {
		return err
	}
	return s.repo.Remove(ctx, who, id)
}
func (s *Service) React(ctx context.Context, who, id string, set bool) (Post, error) {
	if err := owner(who); err != nil {
		return Post{}, err
	}
	if err := PostID(id); err != nil {
		return Post{}, err
	}
	return s.repo.React(ctx, who, id, set)
}
func queryScope(scope string, q Query) string {
	data, _ := json.Marshal([]string{scope, strings.ToLower(q.City), q.Country, q.CategoryID})
	return fmt.Sprintf("%x", sha256.Sum256(data))
}
func ParseQuery(values url.Values, scope string, community bool) (Query, error) {
	copy := url.Values{}
	for k, v := range values {
		copy[k] = v
	}
	copy.Del("cursor")
	base, err := recommendations.ParseQuery(copy, scope, community)
	q := Query{Limit: base.Limit, City: base.City, Country: base.Country, CategoryID: base.CategoryID, Snapshot: time.Now().UTC()}
	if err != nil {
		var validation *accounts.ValidationError
		if errors.As(err, &validation) {
			return q, invalid(validation.Field, strings.ReplaceAll(validation.Message, "recommendation", "discussion"))
		}
		return q, err
	}
	if raw, ok := values["cursor"]; ok {
		if len(raw) != 1 || len(raw[0]) > 1024 {
			return q, invalid("cursor", "use the next cursor from this discussion collection")
		}
		var c Cursor
		data, err := base64.RawURLEncoding.Strict().DecodeString(raw[0])
		if err != nil || json.Unmarshal(data, &c) != nil || c.Scope != queryScope(scope, q) || !accounts.ValidID(c.ID) || c.At.IsZero() || c.Snapshot.IsZero() || c.At.After(c.Snapshot) || c.At.Year() < 1000 || c.Snapshot.Year() > 9999 {
			return q, invalid("cursor", "use the next cursor from this discussion collection and filters")
		}
		q.Cursor = &c
		q.Snapshot = c.Snapshot
	}
	return q, nil
}
func (s *Service) List(ctx context.Context, viewer, event, thread string, values url.Values) (List, error) {
	if event != "" {
		if err := saved.ValidateID(event); err != nil {
			return List{}, err
		}
	}
	if thread != "" {
		if _, err := s.Thread(ctx, viewer, event, thread); err != nil {
			return List{}, err
		}
	}
	scope := "community:" + viewer
	if event != "" {
		scope = "event:" + event + ":" + viewer
	}
	if thread != "" {
		scope += "/thread:" + thread
	}
	q, err := ParseQuery(values, scope, event == "")
	if err != nil {
		return List{}, err
	}
	// MariaDB owns creation timestamps. Use the same clock for pagination rather
	// than temporarily hiding read-after-write results when app/DB clocks differ.
	boundary, err := s.repo.Snapshot(ctx)
	if err != nil {
		return List{}, err
	}
	if q.Cursor == nil {
		q.Snapshot = boundary.UTC()
	} else if q.Snapshot.After(boundary) {
		return List{}, invalid("cursor", "use the next cursor from this discussion collection")
	}
	items, count, err := s.repo.List(ctx, viewer, event, thread, q)
	if err != nil {
		return List{}, err
	}
	result := List{Items: items, Count: count}
	if items == nil {
		result.Items = []Post{}
	}
	if len(items) > q.Limit {
		result.Items = items[:q.Limit]
		last := result.Items[q.Limit-1]
		data, _ := json.Marshal(Cursor{queryScope(scope, q), last.CreatedAt.UTC(), last.ID, q.Snapshot})
		next := base64.RawURLEncoding.EncodeToString(data)
		result.NextCursor = &next
	}
	return result, nil
}

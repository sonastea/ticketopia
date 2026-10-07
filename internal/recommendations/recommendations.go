// Package recommendations owns public endorsements, independently of saves,
// interest, attendance, and discussion posts.
package recommendations

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/sonastea/ticketopia/internal/accounts"
	"github.com/sonastea/ticketopia/internal/models"
	"github.com/sonastea/ticketopia/internal/saved"
)

var ErrNotFound = errors.New("event recommendation not found")
var ErrUnavailable = errors.New("event recommendations unavailable")

type Item struct {
	Event         models.Event           `json:"event"`
	Profile       accounts.PublicProfile `json:"profile"`
	Reason        string                 `json:"reason"`
	RecommendedAt time.Time              `json:"recommended_at"`
	UpdatedAt     time.Time              `json:"updated_at"`
	Meta          models.Freshness       `json:"meta"`
}
type List struct {
	Items      []Item  `json:"items"`
	NextCursor *string `json:"next_cursor"`
	Count      *int    `json:"count,omitempty"`
}
type Cursor struct {
	Scope     string    `json:"scope"`
	Before    time.Time `json:"before"`
	EventID   string    `json:"event_id"`
	AccountID string    `json:"account_id"`
}
type Query struct {
	Limit                     int
	Cursor                    *Cursor
	City, Country, CategoryID string
}
type Repository interface {
	Get(context.Context, string, string) (Item, error)
	Set(context.Context, string, models.EventDetail, string) (Item, bool, error)
	Remove(context.Context, string, string) error
	List(context.Context, string, string, Query) ([]Item, error)
	Count(context.Context, string) (int, error)
}
type Details interface {
	Detail(context.Context, string) (models.EventDetail, error)
}
type Service struct {
	repo    Repository
	details Details
}

func New(repo Repository, details Details) *Service { return &Service{repo, details} }

// Reasons are plain text. Reject invalid encoding/control characters rather
// than silently truncating user-authored content. Normalize surrounding space.
func Reason(value string) (string, error) {
	if !utf8.ValidString(value) || utf8.RuneCountInString(value) > 500 || strings.ContainsFunc(value, func(r rune) bool { return unicode.IsControl(r) && r != '\n' && r != '\r' && r != '\t' }) {
		return "", &accounts.ValidationError{Field: "reason", Message: "use plain text up to 500 characters"}
	}
	return strings.TrimSpace(value), nil
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

// PUT replaces the optional reason. Repeats preserve publication/edit times;
// editing never bumps an endorsement to the top of recent discovery.
func (s *Service) Set(ctx context.Context, owner, id, reason string) (Item, bool, error) {
	if err := ownerID(owner); err != nil {
		return Item{}, false, err
	}
	if err := saved.ValidateID(id); err != nil {
		return Item{}, false, err
	}
	reason, err := Reason(reason)
	if err != nil {
		return Item{}, false, err
	}
	item, err := s.repo.Get(ctx, owner, id)
	if err != nil && !errors.Is(err, ErrNotFound) {
		return Item{}, false, err
	}
	if err == nil && item.Reason == reason {
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
	return s.repo.Set(ctx, owner, detail, reason)
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

var categoryID = regexp.MustCompile(`^[A-Za-z0-9_-]{1,64}$`)
var countryCode = regexp.MustCompile(`^[A-Za-z]{2}$`)

func ParseQuery(values url.Values, scope string, community bool) (Query, error) {
	q := Query{Limit: 20}
	invalid := func(field, message string) (Query, error) {
		return q, &accounts.ValidationError{Field: field, Message: message}
	}
	for key, entries := range values {
		if len(entries) != 1 || (key != "limit" && key != "cursor" && (!community || (key != "city" && key != "country" && key != "category_id"))) {
			return invalid(key, "use one value for a supported recommendation parameter")
		}
	}
	q.City = strings.TrimSpace(values.Get("city"))
	if !utf8.ValidString(q.City) || utf8.RuneCountInString(q.City) > 120 || strings.ContainsFunc(q.City, unicode.IsControl) {
		return invalid("city", "use a city name up to 120 characters")
	}
	q.Country = strings.ToUpper(strings.TrimSpace(values.Get("country")))
	if q.Country != "" && !countryCode.MatchString(q.Country) {
		return invalid("country", "use a two-letter country code")
	}
	q.CategoryID = strings.TrimSpace(values.Get("category_id"))
	if q.CategoryID == "all" {
		q.CategoryID = ""
	}
	if q.CategoryID != "" && !categoryID.MatchString(q.CategoryID) {
		return invalid("category_id", "use a category ID or all")
	}
	if raw, ok := values["limit"]; ok {
		n, err := strconv.Atoi(raw[0])
		if err != nil || n < 1 || n > 100 {
			return invalid("limit", "use an integer from 1 to 100")
		}
		q.Limit = n
	}
	bound := queryScope(scope, q)
	if raw, ok := values["cursor"]; ok {
		if len(raw[0]) > 1024 {
			return invalid("cursor", "use the next cursor from this recommendation collection and filters")
		}
		var cursor Cursor
		data, err := base64.RawURLEncoding.Strict().DecodeString(raw[0])
		if err != nil || json.Unmarshal(data, &cursor) != nil || cursor.Scope != bound || cursor.Before.IsZero() || cursor.Before.Year() < 1000 || cursor.Before.Year() > 9999 || !accounts.ValidID(cursor.AccountID) || saved.ValidateID(cursor.EventID) != nil {
			return invalid("cursor", "use the next cursor from this recommendation collection and filters")
		}
		q.Cursor = &cursor
	}
	return q, nil
}
func queryScope(scope string, q Query) string {
	data, _ := json.Marshal([]string{scope, strings.ToLower(q.City), q.Country, q.CategoryID})
	return fmt.Sprintf("%x", sha256.Sum256(data))
}
func (s *Service) list(ctx context.Context, owner, event, scope string, community bool, values url.Values) (List, error) {
	q, err := ParseQuery(values, scope, community)
	if err != nil {
		return List{}, err
	}
	items, err := s.repo.List(ctx, owner, event, q)
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
		data, _ := json.Marshal(Cursor{queryScope(scope, q), last.RecommendedAt.UTC(), last.Event.ID, last.Profile.ID})
		next := base64.RawURLEncoding.EncodeToString(data)
		result.NextCursor = &next
	}
	return result, nil
}
func (s *Service) Own(ctx context.Context, owner string, values url.Values) (List, error) {
	if err := ownerID(owner); err != nil {
		return List{}, err
	}
	return s.list(ctx, owner, "", "own:"+owner, false, values)
}
func (s *Service) Public(ctx context.Context, owner string, values url.Values) (List, error) {
	if err := ownerID(owner); err != nil {
		return List{}, err
	}
	return s.list(ctx, owner, "", "public:"+owner, false, values)
}
func (s *Service) Event(ctx context.Context, id string, values url.Values) (List, error) {
	if err := saved.ValidateID(id); err != nil {
		return List{}, err
	}
	list, err := s.list(ctx, "", id, "event:"+id, false, values)
	if err != nil {
		return List{}, err
	}
	count, err := s.repo.Count(ctx, id)
	if err != nil {
		return List{}, err
	}
	list.Count = &count
	return list, nil
}
func (s *Service) Community(ctx context.Context, values url.Values) (List, error) {
	return s.list(ctx, "", "", "community", true, values)
}

// Package recommendations owns public endorsements, independently of saves,
// interest, attendance, and discussion posts.
package recommendations

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
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

// ErrPersonal is an agent trying to withdraw a recommendation the person
// wrote or published themselves; only the person can, on the web.
// ErrChanged is a write whose Expect no longer matches the recommendation.
var ErrChanged = errors.New("the recommendation changed since it was reviewed")

// ErrDraftGone is a Write.Draft that was already consumed, discarded or expired.
var ErrDraftGone = errors.New("draft already published, discarded or expired")

var ErrPersonal = errors.New("this recommendation was written or published by the person")

type Item struct {
	Event   models.Event           `json:"event"`
	Profile accounts.PublicProfile `json:"profile"`
	Reason  string                 `json:"reason"`
	// Via names the connected app an agent published this through, or whose
	// draft the person published; empty for a person's own writing on the web.
	Via string `json:"via,omitempty"`
	// ViaHost is the verified host of that app's metadata document, empty
	// for API-token clients, which identify no app.
	ViaHost       string           `json:"via_host,omitempty"`
	RecommendedAt time.Time        `json:"recommended_at"`
	UpdatedAt     time.Time        `json:"updated_at"`
	Meta          models.Freshness `json:"meta"`
	Observation   *Observation     `json:"-"`
}

// Observation measures committed transitions, not attempts, retries, or edits.
// It is operator metadata, excluded from all public projections.
type Observation struct {
	WindowStart          time.Time
	NewPublications      uint64
	Reactivations        uint64
	TotalNewPublications uint64
	TotalReactivations   uint64
}

const PreviewLimit = 3

type EventGroup struct {
	Event              models.Event     `json:"event"`
	FirstRecommendedAt time.Time        `json:"first_recommended_at"`
	Count              int              `json:"count"`
	Recommendations    []Item           `json:"recommendations"`
	Meta               models.Freshness `json:"meta"`
}
type Feed struct {
	Items      []EventGroup `json:"items"`
	NextCursor *string      `json:"next_cursor"`
}
type FeedCursor struct {
	Scope   string    `json:"scope"`
	Before  time.Time `json:"before"`
	EventID string    `json:"event_id"`
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
	FeedCursor                *FeedCursor
	City, Country, CategoryID string
}

// Write is one publication or edit of an account's recommendation.
type Write struct {
	// Reason replaces the optional plain-text reason.
	Reason string
	// Via names the connected app the publication or its text came from, up
	// to MaxVia characters; empty for a person writing on the web. It is
	// stored only when the write changes the recommendation (a new or
	// reactivated endorsement, or a changed reason).
	Via string
	// ViaHost is the app's verified metadata host, stored and cleared with Via.
	ViaHost string
	// Expect, when set, is the recommendation state the person reviewed; the
	// write fails with ErrChanged, changing nothing, if it no longer holds.
	Expect *Current
	// Draft, when set, is an agent draft of the owner consumed in the same
	// transaction; ErrDraftGone if it was already published, discarded or
	// expired, so a draft is published at most once.
	Draft string
	// KeepExisting leaves an active recommendation exactly as it is, reason
	// and attribution included: an agent recommending without a reason never
	// erases the person's words. It is checked under the row lock, so a
	// concurrent edit cannot be overwritten either.
	KeepExisting bool
}

// Current is a recommendation's state as the person saw it: whether it was
// active, and ReasonHash of its reason. A hash, not the text, travels through
// forms, because browsers rewrite line endings in submitted fields.
type Current struct {
	Active     bool
	ReasonHash string
}

// ReasonHash identifies a reason exactly, for Current.
func ReasonHash(reason string) string {
	sum := sha256.Sum256([]byte(reason))
	return hex.EncodeToString(sum[:])
}

// Matches reports whether the recommendation is still as the person saw it.
func (c Current) Matches(active bool, reason string) bool {
	return c.Active == active && (!active || c.ReasonHash == ReasonHash(reason))
}

// MaxVia bounds a stored app name, matching OAuth client names.
const MaxVia = 80

type Repository interface {
	Get(context.Context, string, string) (Item, error)
	// Write publishes or edits as described by Write, returning whether it
	// created or reactivated the endorsement.
	Write(context.Context, string, models.EventDetail, Write) (Item, bool, error)
	Remove(context.Context, string, string) error
	// WithdrawAttributed withdraws only an active recommendation that an app
	// published and that has no reason, reporting whether it did.
	WithdrawAttributed(context.Context, string, string) (bool, error)
	List(context.Context, string, string, Query) ([]Item, error)
	// EventList returns an event's page and total from one committed state.
	EventList(context.Context, string, Query) ([]Item, int, error)
	Community(context.Context, Query) ([]EventGroup, error)
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
// editing never bumps an endorsement to the top of recent discovery. A
// person's own edit carries no app attribution.
func (s *Service) Set(ctx context.Context, owner, id, reason string) (Item, bool, error) {
	return s.Publish(ctx, owner, id, Write{Reason: reason})
}

// Publish is Set with attribution and KeepExisting; see Write.
func (s *Service) Publish(ctx context.Context, owner, id string, w Write) (Item, bool, error) {
	if err := ownerID(owner); err != nil {
		return Item{}, false, err
	}
	if err := saved.ValidateID(id); err != nil {
		return Item{}, false, err
	}
	reason, err := Reason(w.Reason)
	if err != nil {
		return Item{}, false, err
	}
	w.Reason = reason
	if utf8.RuneCountInString(w.Via) > MaxVia || !utf8.ValidString(w.Via) || len(w.ViaHost) > 253 || (w.Via == "" && w.ViaHost != "") {
		return Item{}, false, &accounts.ValidationError{Field: "via", Message: "use an app name up to 80 characters"}
	}
	item, err := s.repo.Get(ctx, owner, id)
	if err != nil && !errors.Is(err, ErrNotFound) {
		return Item{}, false, err
	}
	if w.Expect != nil && !w.Expect.Matches(err == nil, item.Reason) {
		return Item{}, false, ErrChanged
	}
	// A draft is always consumed by the repository write, even when unchanged.
	if err == nil && w.Draft == "" && (w.KeepExisting || item.Reason == reason) {
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
	return s.repo.Write(ctx, owner, detail, w)
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

// WithdrawForAgent withdraws a recommendation an app published without a
// reason. A recommendation with a reason or published by the person is left
// alone (ErrPersonal): agents never erase the person's words. A missing one
// changes nothing.
func (s *Service) WithdrawForAgent(ctx context.Context, owner, id string) error {
	if err := ownerID(owner); err != nil {
		return err
	}
	if err := saved.ValidateID(id); err != nil {
		return err
	}
	withdrawn, err := s.repo.WithdrawAttributed(ctx, owner, id)
	if err != nil || withdrawn {
		return err
	}
	if _, err := s.repo.Get(ctx, owner, id); errors.Is(err, ErrNotFound) {
		return nil
	} else if err != nil {
		return err
	}
	return ErrPersonal
}

var categoryID = regexp.MustCompile(`^[A-Za-z0-9_-]{1,64}$`)
var countryCode = regexp.MustCompile(`^[A-Za-z]{2}$`)

// FeedParam reports whether key is a supported community feed parameter.
func FeedParam(key string) bool {
	switch key {
	case "limit", "cursor", "city", "country", "category_id":
		return true
	}
	return false
}

func ParseQuery(values url.Values, scope string, community bool) (Query, error) {
	q := Query{Limit: 20}
	invalid := func(field, message string) (Query, error) {
		return q, &accounts.ValidationError{Field: field, Message: message}
	}
	for key, entries := range values {
		if len(entries) != 1 || (key != "limit" && key != "cursor" && (!community || !FeedParam(key))) {
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
func (s *Service) list(ctx context.Context, owner, scope string, community bool, values url.Values) (List, error) {
	q, err := ParseQuery(values, scope, community)
	if err != nil {
		return List{}, err
	}
	items, err := s.repo.List(ctx, owner, "", q)
	if err != nil {
		return List{}, err
	}
	return page(items, scope, q), nil
}
func page(items []Item, scope string, q Query) List {
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
	return result
}
func (s *Service) Own(ctx context.Context, owner string, values url.Values) (List, error) {
	if err := ownerID(owner); err != nil {
		return List{}, err
	}
	return s.list(ctx, owner, "own:"+owner, false, values)
}
func (s *Service) Public(ctx context.Context, owner string, values url.Values) (List, error) {
	if err := ownerID(owner); err != nil {
		return List{}, err
	}
	return s.list(ctx, owner, "public:"+owner, false, values)
}
func (s *Service) Event(ctx context.Context, id string, values url.Values) (List, error) {
	if err := saved.ValidateID(id); err != nil {
		return List{}, err
	}
	scope := "event:" + id
	q, err := ParseQuery(values, scope, false)
	if err != nil {
		return List{}, err
	}
	items, count, err := s.repo.EventList(ctx, id, q)
	if err != nil {
		return List{}, err
	}
	list := page(items, scope, q)
	list.Count = &count
	return list, nil
}
func (s *Service) Recent(ctx context.Context, values url.Values) (List, error) {
	return s.list(ctx, "", "community", true, values)
}
func ParseFeedQuery(values url.Values) (Query, error) {
	filters := url.Values{}
	for k, v := range values {
		filters[k] = v
	}
	filters.Del("cursor")
	q, err := ParseQuery(filters, "community-events", true)
	if err != nil {
		return q, err
	}
	if raw, ok := values["cursor"]; ok {
		var c FeedCursor
		if len(raw) != 1 || len(raw[0]) > 1024 {
			return q, &accounts.ValidationError{Field: "cursor", Message: "use the next cursor from this grouped recommendation feed"}
		}
		data, err := base64.RawURLEncoding.Strict().DecodeString(raw[0])
		if err != nil || json.Unmarshal(data, &c) != nil || c.Scope != queryScope("community-events", q) || c.Before.IsZero() || c.Before.Year() < 1000 || c.Before.Year() > 9999 || saved.ValidateID(c.EventID) != nil {
			return q, &accounts.ValidationError{Field: "cursor", Message: "use the next cursor from this grouped recommendation feed and filters"}
		}
		q.FeedCursor = &c
	}
	return q, nil
}
func (s *Service) Community(ctx context.Context, values url.Values) (Feed, error) {
	q, err := ParseFeedQuery(values)
	if err != nil {
		return Feed{}, err
	}
	items, err := s.repo.Community(ctx, q)
	if err != nil {
		return Feed{}, err
	}
	result := Feed{Items: items}
	if items == nil {
		result.Items = []EventGroup{}
	}
	if len(items) > q.Limit {
		result.Items = items[:q.Limit]
		last := result.Items[q.Limit-1]
		data, _ := json.Marshal(FeedCursor{queryScope("community-events", q), last.FirstRecommendedAt.UTC(), last.Event.ID})
		cursor := base64.RawURLEncoding.EncodeToString(data)
		result.NextCursor = &cursor
	}
	return result, nil
}

// Package useractivity reads one account's public contributions as a single
// newest-first timeline: visible discussion posts and replies, active
// recommendations, and Interested choices the person made public. It never
// includes private interest, saves, follows, reminders or moderation state, and
// it is not a personalised feed of other people's activity.
package useractivity

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

var ErrUnavailable = errors.New("public activity unavailable")

// Item types, in the order they sort when timestamps tie (highest first).
const (
	TypeInterest       = "interest"
	TypeDiscussionPost = "discussion_post"
	TypeRecommendation = "recommendation"
)

// Rank orders item types that share a timestamp, so pagination is total.
func Rank(kind string) int {
	switch kind {
	case TypeInterest:
		return 0
	case TypeDiscussionPost:
		return 1
	case TypeRecommendation:
		return 2
	}
	return -1
}

type Post struct {
	ID       string `json:"id"`
	ThreadID string `json:"thread_id"`
	ParentID string `json:"parent_id,omitempty"`
	Body     string `json:"body"`
	// Via and ViaHost name the connected app whose draft the person
	// published as this post, as on discussions.Post.
	Via       string    `json:"via,omitempty"`
	ViaHost   string    `json:"via_host,omitempty"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

type Recommendation struct {
	Reason        string    `json:"reason"`
	Via           string    `json:"via,omitempty"`
	ViaHost       string    `json:"via_host,omitempty"`
	RecommendedAt time.Time `json:"recommended_at"`
	UpdatedAt     time.Time `json:"updated_at"`
}

// Item is one public contribution. Exactly one of Post or Recommendation is
// set for those types; a public interest carries only its event and time.
type Item struct {
	Type           string           `json:"type"`
	At             time.Time        `json:"at"`
	Event          models.Event     `json:"event"`
	Post           *Post            `json:"post,omitempty"`
	Recommendation *Recommendation  `json:"recommendation,omitempty"`
	Meta           models.Freshness `json:"meta"`
}

// Key is the item's tie-breaking identity within its type: the post ID for a
// discussion post, otherwise the event ID.
func (i Item) Key() string {
	if i.Post != nil {
		return i.Post.ID
	}
	return i.Event.ID
}

type List struct {
	Items      []Item  `json:"items"`
	NextCursor *string `json:"next_cursor"`
}

type Cursor struct {
	Scope string    `json:"scope"`
	At    time.Time `json:"at"`
	Rank  int       `json:"rank"`
	ID    string    `json:"id"`
}

type Query struct {
	Limit  int
	Cursor *Cursor
}

// Repository returns up to Limit+1 items ordered by (At, Rank, Key) descending,
// strictly after the cursor when one is given.
type Repository interface {
	List(context.Context, string, Query) ([]Item, error)
}

type Service struct{ repo Repository }

func New(repo Repository) *Service { return &Service{repo} }

func invalidCursor() error {
	return &accounts.ValidationError{Field: "cursor", Message: "use the next cursor from this activity collection"}
}

func ParseQuery(values url.Values, scope string) (Query, error) {
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
			return q, invalidCursor()
		}
		var c Cursor
		data, err := base64.RawURLEncoding.Strict().DecodeString(raw[0])
		if err != nil || json.Unmarshal(data, &c) != nil || c.Scope != scope || c.At.IsZero() || c.At.Year() < 1000 || c.At.Year() > 9999 {
			return q, invalidCursor()
		}
		switch c.Rank {
		case Rank(TypeDiscussionPost):
			if !accounts.ValidID(c.ID) {
				return q, invalidCursor()
			}
		case Rank(TypeInterest), Rank(TypeRecommendation):
			if saved.ValidateID(c.ID) != nil {
				return q, invalidCursor()
			}
		default:
			return q, invalidCursor()
		}
		q.Cursor = &c
	}
	return q, nil
}

// List reads a public timeline for an account already confirmed to exist.
func (s *Service) List(ctx context.Context, owner string, values url.Values) (List, error) {
	if !accounts.ValidID(owner) {
		return List{}, accounts.ErrNotFound
	}
	scope := "user:" + owner
	q, err := ParseQuery(values, scope)
	if err != nil {
		return List{}, err
	}
	if s == nil || s.repo == nil {
		return List{}, ErrUnavailable
	}
	items, err := s.repo.List(ctx, owner, q)
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
		data, _ := json.Marshal(Cursor{scope, last.At.UTC(), Rank(last.Type), last.Key()})
		next := base64.RawURLEncoding.EncodeToString(data)
		result.NextCursor = &next
	}
	return result, nil
}

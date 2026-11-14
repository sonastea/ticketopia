// Package drafts holds free text an agent wrote for the person to review.
// Agents never publish words: draft_recommendation stores a private draft,
// and only the signed-in person publishes it, on the web (/me/drafts),
// through a CSRF-protected form. Drafts expire after Lifetime.
package drafts

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/sonastea/ticketopia/internal/accounts"
	"github.com/sonastea/ticketopia/internal/models"
	"github.com/sonastea/ticketopia/internal/recommendations"
	"github.com/sonastea/ticketopia/internal/saved"
)

const (
	// KindRecommendation is a recommendation reason. Phase 2 adds discussion kinds.
	KindRecommendation = "recommendation"
	// Lifetime is how long a draft waits for review.
	Lifetime = 7 * 24 * time.Hour
	// MaxPending bounds an account's unexpired drafts; creating more fails.
	MaxPending = 20
	// MaxClientName matches OAuth client names and recommendation attribution.
	MaxClientName = recommendations.MaxVia
	// MaxClientHost bounds a stored host: a DNS name in ASCII (IDNA) form.
	MaxClientHost = 253
)

var (
	// ErrNotFound is a draft that does not exist, expired or belongs to someone else.
	ErrNotFound = errors.New("draft not found")
	// ErrLimit is an account already holding MaxPending drafts.
	ErrLimit = errors.New("too many pending drafts")
	// ErrUnavailable is storage failing.
	ErrUnavailable = errors.New("drafts unavailable")
)

// Draft is one pending piece of agent-written text. It is private to its
// account until the person publishes it.
type Draft struct {
	ID      string       `json:"id"`
	EventID string       `json:"event_id"`
	Event   models.Event `json:"event"`
	Kind    string       `json:"kind"`
	Text    string       `json:"text"`
	// ClientName and ClientHost identify the connected app that wrote it
	// (its self-declared name and verified metadata host); publishing
	// attributes the recommendation to them. API-token drafts have no host
	// and the name APIClientName.
	ClientName string `json:"client_name"`
	ClientHost string `json:"client_host,omitempty"`
	// GrantID is the OAuth connection that wrote it, empty for API tokens.
	// Removing the connection deletes its drafts.
	GrantID   string    `json:"-"`
	CreatedAt time.Time `json:"created_at"`
	ExpiresAt time.Time `json:"expires_at"`
}

type Repository interface {
	// Create stores d with its event's snapshot, after deleting the account's
	// expired drafts, unless the account already holds MaxPending (ErrLimit).
	// An identical pending draft (event, kind, text, connection) is returned instead
	// of adding another, so retries are idempotent.
	Create(ctx context.Context, detail models.EventDetail, d Draft, account string, now time.Time) (Draft, error)
	// List returns the account's unexpired drafts, newest first.
	List(ctx context.Context, account string, now time.Time) ([]Draft, error)
	// Get returns one unexpired draft of the account, or ErrNotFound.
	Get(ctx context.Context, account, id string, now time.Time) (Draft, error)
	// Delete removes one draft of the account; a missing one changes nothing.
	Delete(ctx context.Context, account, id string) error
	// Count is the account's unexpired drafts.
	Count(ctx context.Context, account string, now time.Time) (int, error)
}

// Details reads an event, including any durable last-known fallback.
type Details interface {
	Detail(context.Context, string) (models.EventDetail, error)
}

type Service struct {
	repo    Repository
	details Details
	now     func() time.Time
}

func New(repo Repository, details Details) *Service {
	return &Service{repo: repo, details: details, now: func() time.Time { return time.Now().UTC() }}
}

// SetNow fixes the clock, for tests and review fixtures.
func (s *Service) SetNow(now func() time.Time) { s.now = now }

var idPattern = regexp.MustCompile(`^[0-9a-f]{32}$`)

// ValidID reports whether id has the shape of a draft ID.
func ValidID(id string) bool { return idPattern.MatchString(id) }

func newID() string {
	var b [16]byte
	_, _ = rand.Read(b[:])
	return hex.EncodeToString(b[:])
}

func owner(id string) error {
	if !accounts.ValidID(id) {
		return accounts.ErrUnauthenticated
	}
	return nil
}

// Text validates a draft's text with the recommendation reason rules (plain
// text, up to 500 characters) and requires some: a recommendation without a
// reason needs no review.
func Text(value string) (string, error) {
	text, err := recommendations.Reason(value)
	if err != nil {
		return "", &accounts.ValidationError{Field: "text", Message: "use plain text up to 500 characters"}
	}
	if text == "" {
		return "", &accounts.ValidationError{Field: "text", Message: "write the reason to review; to recommend without one, use recommend_event"}
	}
	return text, nil
}

// APIClientName labels drafts made with an API token: it identifies the
// account, not an app, so nothing more can be claimed.
const APIClientName = "an API client"

// Client identifies the app writing a draft, from the verified connection,
// never from tool input.
type Client struct {
	Name, Host, GrantID string
}

// FromAPIToken reports whether the draft came from an API token rather
// than an identified OAuth app.
func (d Draft) FromAPIToken() bool { return d.ClientHost == "" }

// clientName bounds the app name; an over-long or invalid one is shortened
// rather than refused, since the person did not choose it.
func clientName(name string) string {
	name = strings.TrimSpace(name)
	if !utf8.ValidString(name) {
		name = strings.ToValidUTF8(name, "")
	}
	if utf8.RuneCountInString(name) > MaxClientName {
		name = string([]rune(name)[:MaxClientName])
	}
	if name == "" {
		return APIClientName
	}
	return name
}

// CreateRecommendation stores a private recommendation draft. The event is
// read (and its snapshot stored) now, so the person reviews a real event.
func (s *Service) CreateRecommendation(ctx context.Context, account, eventID, text string, client Client) (Draft, error) {
	if err := owner(account); err != nil {
		return Draft{}, err
	}
	if err := saved.ValidateID(eventID); err != nil {
		return Draft{}, err
	}
	text, err := Text(text)
	if err != nil {
		return Draft{}, err
	}
	if s.details == nil {
		return Draft{}, ErrUnavailable
	}
	detail, err := s.details.Detail(ctx, eventID)
	if err != nil {
		return Draft{}, err
	}
	if detail.Item.ID != eventID {
		return Draft{}, ErrUnavailable
	}
	now := s.now()
	d := Draft{ID: newID(), EventID: eventID, Event: detail.Item, Kind: KindRecommendation, Text: text, CreatedAt: now, ExpiresAt: now.Add(Lifetime)}
	// An OAuth connection is always identified as itself, never downgraded to
	// an API token: its drafts must go with it.
	switch {
	case client.Host == "" && client.GrantID == "":
		d.ClientName = APIClientName
	case client.Host == "" || client.GrantID == "" || len(client.Host) > MaxClientHost:
		return Draft{}, ErrUnavailable
	default:
		d.ClientName, d.ClientHost, d.GrantID = clientName(client.Name), client.Host, client.GrantID
	}
	return s.repo.Create(ctx, detail, d, account, now)
}

func (s *Service) List(ctx context.Context, account string) ([]Draft, error) {
	if err := owner(account); err != nil {
		return nil, err
	}
	return s.repo.List(ctx, account, s.now())
}

func (s *Service) Get(ctx context.Context, account, id string) (Draft, error) {
	if err := owner(account); err != nil {
		return Draft{}, err
	}
	if !ValidID(id) {
		return Draft{}, ErrNotFound
	}
	return s.repo.Get(ctx, account, id, s.now())
}

// Discard deletes a draft; discarding a missing one changes nothing.
func (s *Service) Discard(ctx context.Context, account, id string) error {
	if err := owner(account); err != nil {
		return err
	}
	if !ValidID(id) {
		return nil
	}
	return s.repo.Delete(ctx, account, id)
}

func (s *Service) Count(ctx context.Context, account string) (int, error) {
	if err := owner(account); err != nil {
		return 0, err
	}
	return s.repo.Count(ctx, account, s.now())
}

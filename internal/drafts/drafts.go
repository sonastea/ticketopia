// Package drafts holds free text an agent wrote for the person to review.
// draft_recommendation, draft_question and draft_reply store a private
// draft; the person publishes it on the web (/me/drafts) through a
// CSRF-protected form, or an agent or API client the person allowed to post
// publishes it (publish_draft, POST /api/v1/me/drafts/{id}/publish). Drafts
// expire after Lifetime.
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
	"github.com/sonastea/ticketopia/internal/discussions"
	"github.com/sonastea/ticketopia/internal/models"
	"github.com/sonastea/ticketopia/internal/recommendations"
	"github.com/sonastea/ticketopia/internal/saved"
)

const (
	// KindRecommendation is a recommendation reason.
	KindRecommendation = "recommendation"
	// KindQuestion is a new question or tip on an event.
	KindQuestion = "question"
	// KindReply is a reply in an existing question or tip's thread.
	KindReply = "reply"
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
	// ThreadID is a reply's question or tip; ParentID the reply it answers
	// within that thread, empty when it answers the question or tip itself.
	ThreadID string `json:"thread_id,omitempty"`
	ParentID string `json:"parent_id,omitempty"`
	Text     string `json:"text"`
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
	// Create stores d with its event's snapshot (nil when the event's stored
	// snapshot is used, as for replies), after deleting the account's expired
	// drafts, unless the account already holds MaxPending (ErrLimit). An
	// identical pending draft (event, kind, thread, parent, text, connection)
	// is returned instead of adding another, so retries are idempotent.
	Create(ctx context.Context, detail *models.EventDetail, d Draft, account string, now time.Time) (Draft, error)
	// List returns the account's unexpired drafts, newest first.
	List(ctx context.Context, account string, now time.Time) ([]Draft, error)
	// Get returns one unexpired draft of the account, or ErrNotFound.
	Get(ctx context.Context, account, id string, now time.Time) (Draft, error)
	// Delete removes one draft of the account, reporting whether one was
	// removed; a missing one (or another account's) changes nothing.
	Delete(ctx context.Context, account, id string) (bool, error)
	// Count is the account's unexpired drafts.
	Count(ctx context.Context, account string, now time.Time) (int, error)
}

// Details reads an event, including any durable last-known fallback.
type Details interface {
	Detail(context.Context, string) (models.EventDetail, error)
}

// Threads reads a reply draft's conversation with the discussion routes'
// rules, so a draft answers a real question or tip on its event.
type Threads interface {
	Thread(ctx context.Context, viewer, event, id string) (discussions.Post, error)
	Parent(ctx context.Context, viewer, event, thread, id string) (discussions.Post, error)
}

type Service struct {
	repo    Repository
	details Details
	threads Threads
	now     func() time.Time
}

// New returns the drafts service; threads may be nil, leaving reply drafts
// unavailable.
func New(repo Repository, details Details, threads Threads) *Service {
	return &Service{repo: repo, details: details, threads: threads, now: func() time.Time { return time.Now().UTC() }}
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
	detail, err := s.event(ctx, eventID)
	if err != nil {
		return Draft{}, err
	}
	return s.create(ctx, account, &detail, Draft{EventID: eventID, Event: detail.Item, Kind: KindRecommendation, Text: text}, client)
}

// CreateQuestion stores a private draft of a new question or tip on an
// event, with the discussion body rules (plain text, 1 to 4000 characters).
func (s *Service) CreateQuestion(ctx context.Context, account, eventID, body string, client Client) (Draft, error) {
	if err := owner(account); err != nil {
		return Draft{}, err
	}
	if err := saved.ValidateID(eventID); err != nil {
		return Draft{}, err
	}
	body, err := discussions.Body(body)
	if err != nil {
		return Draft{}, err
	}
	detail, err := s.event(ctx, eventID)
	if err != nil {
		return Draft{}, err
	}
	return s.create(ctx, account, &detail, Draft{EventID: eventID, Event: detail.Item, Kind: KindQuestion, Text: body}, client)
}

// CreateReply stores a private draft of a reply in a question or tip's
// thread, answering parentID within it when set. The thread must be a
// question or tip on eventID, and the parent a post in that thread, as for
// replies on the web; the thread's stored event snapshot is used, so no
// provider read is needed. Not-found errors are discussions.ErrNotFound,
// wrapped in ErrParentNotFound for the parent.
func (s *Service) CreateReply(ctx context.Context, account, eventID, threadID, parentID, body string, client Client) (Draft, error) {
	if err := owner(account); err != nil {
		return Draft{}, err
	}
	if err := saved.ValidateID(eventID); err != nil {
		return Draft{}, err
	}
	if !accounts.ValidID(threadID) {
		return Draft{}, &accounts.ValidationError{Field: "thread_id", Message: "use a question or tip id"}
	}
	if parentID != "" && !accounts.ValidID(parentID) {
		return Draft{}, &accounts.ValidationError{Field: "parent_id", Message: "use a reply id from the same thread, or omit it to answer the question or tip"}
	}
	body, err := discussions.Body(body)
	if err != nil {
		return Draft{}, err
	}
	if s.threads == nil {
		return Draft{}, ErrUnavailable
	}
	root, err := s.threads.Thread(ctx, account, eventID, threadID)
	if err != nil {
		return Draft{}, err
	}
	// Answering the root is the same reply as omitting parent_id.
	if parentID == threadID {
		parentID = ""
	}
	if parentID != "" {
		if _, err := s.threads.Parent(ctx, account, eventID, threadID, parentID); err != nil {
			if errors.Is(err, discussions.ErrNotFound) {
				return Draft{}, ErrParentNotFound
			}
			return Draft{}, err
		}
	}
	return s.create(ctx, account, nil, Draft{EventID: eventID, Event: root.Event, Kind: KindReply, ThreadID: threadID, ParentID: parentID, Text: body}, client)
}

// ErrParentNotFound is a reply draft's parent_id that is not a post in its thread.
var ErrParentNotFound = errors.New("reply parent not found in this thread")

// event reads an event for a draft that stores its snapshot.
func (s *Service) event(ctx context.Context, eventID string) (models.EventDetail, error) {
	if s.details == nil {
		return models.EventDetail{}, ErrUnavailable
	}
	detail, err := s.details.Detail(ctx, eventID)
	if err != nil {
		return models.EventDetail{}, err
	}
	if detail.Item.ID != eventID {
		return models.EventDetail{}, ErrUnavailable
	}
	return detail, nil
}

// create identifies the app and stores the validated draft.
func (s *Service) create(ctx context.Context, account string, detail *models.EventDetail, d Draft, client Client) (Draft, error) {
	now := s.now()
	d.ID, d.CreatedAt, d.ExpiresAt = newID(), now, now.Add(Lifetime)
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

// Discussion reports whether the draft publishes as a discussion post.
func (d Draft) Discussion() bool { return d.Kind == KindQuestion || d.Kind == KindReply }

// PublishKey is the discussion idempotency key of the draft's post: one
// draft is one contribution, so a repeated publication finds the same post.
func (d Draft) PublishKey() string { return discussions.ReservedKeyPrefix + "draft-" + d.ID }

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

// Discard deletes a draft, reporting whether one was pending. Discarding a
// missing one, or another account's, changes nothing and reports false,
// without saying which. A malformed ID is a validation error, so a caller
// passing a post or event ID learns of the mistake.
func (s *Service) Discard(ctx context.Context, account, id string) (bool, error) {
	if err := owner(account); err != nil {
		return false, err
	}
	if !ValidID(id) {
		return false, &accounts.ValidationError{Field: "draft_id", Message: "use a draft id from my_drafts"}
	}
	return s.repo.Delete(ctx, account, id)
}

func (s *Service) Count(ctx context.Context, account string) (int, error) {
	if err := owner(account); err != nil {
		return 0, err
	}
	return s.repo.Count(ctx, account, s.now())
}

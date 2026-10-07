// Package accounts owns account identity, credentials, and private preferences.
// HTTP clients never choose an owner ID for a personal write.
package accounts

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strings"
	"time"
	_ "time/tzdata"
	"unicode"
	"unicode/utf8"
)

var (
	ErrUnauthenticated = errors.New("sign in to continue")
	ErrNotFound        = errors.New("account not found")
	ErrFlow            = errors.New("sign-in expired or invalid; please start again")
	ErrLimited         = errors.New("too many attempts; please try again later")
	ErrCredentialLimit = errors.New("revoke an existing API token before creating another")
)

const (
	GoogleIssuer       = "https://accounts.google.com"
	FlowLifetime       = 10 * time.Minute
	CredentialLifetime = 30 * 24 * time.Hour
)

type ValidationError struct{ Field, Message string }

func (e *ValidationError) Error() string { return e.Field + ": " + e.Message }

type Preferences struct {
	City                string   `json:"city"`
	Country             string   `json:"country"`
	Timezone            string   `json:"timezone"`
	CategoryIDs         []string `json:"category_ids"`
	EmailReminders      bool     `json:"email_reminders"`
	WeeklyDigest        bool     `json:"weekly_digest"`
	NotificationsPaused bool     `json:"notifications_paused"`
}

type Account struct {
	ID                 string      `json:"id"`
	DisplayName        string      `json:"display_name"`
	Bio                string      `json:"bio"`
	InterestVisibility string      `json:"interest_visibility"`
	Email              string      `json:"email"`
	Preferences        Preferences `json:"preferences"`
	CreatedAt          time.Time   `json:"created_at"`
}

// PublicProfile is a separate allowlisted projection, not a redacted Account.
type PublicProfile struct {
	ID          string `json:"id"`
	DisplayName string `json:"display_name"`
	Bio         string `json:"bio"`
}

func (a Account) Public() PublicProfile { return PublicProfile{a.ID, a.DisplayName, a.Bio} }

type ProfilePatch struct {
	DisplayName        *string `json:"display_name"`
	Bio                *string `json:"bio"`
	InterestVisibility *string `json:"interest_visibility"`
}
type PreferencesPatch struct {
	City                *string   `json:"city"`
	Country             *string   `json:"country"`
	Timezone            *string   `json:"timezone"`
	CategoryIDs         *[]string `json:"category_ids"`
	EmailReminders      *bool     `json:"email_reminders"`
	WeeklyDigest        *bool     `json:"weekly_digest"`
	NotificationsPaused *bool     `json:"notifications_paused"`
}

type Identity struct{ Issuer, Subject, Email string }
type Flow struct {
	StateHash, BrowserHash    [32]byte
	Verifier, Nonce, ReturnTo string
	ExpiresAt                 time.Time
}
type Credential struct {
	ID        string    `json:"id"`
	AccountID string    `json:"-"`
	Hash      [32]byte  `json:"-"`
	Kind      string    `json:"kind"`
	Name      string    `json:"name"`
	CreatedAt time.Time `json:"created_at"`
	ExpiresAt time.Time `json:"expires_at"`
}
type Principal struct {
	Account    Account
	Credential Credential
	CSRF       string
}
type Login struct{ State, Browser, Verifier, Nonce string }

type Repository interface {
	PutFlow(context.Context, Flow) error
	ConsumeFlow(context.Context, [32]byte, [32]byte, time.Time) (Flow, error)
	Login(context.Context, Identity, Credential) (Account, error)
	Authenticate(context.Context, [32]byte, string, time.Time) (Account, Credential, error)
	Get(context.Context, string) (Account, error)
	UpdateProfile(context.Context, string, ProfilePatch) (Account, error)
	UpdatePreferences(context.Context, string, PreferencesPatch) (Account, error)
	ListTokens(context.Context, string, time.Time) ([]Credential, error)
	CreateToken(context.Context, Credential, time.Time) error
	Revoke(context.Context, string, string) error
	RevokeAll(context.Context, string) error
	Allow(context.Context, [32]byte, time.Time, time.Duration, int) (bool, error)
}
type Provider interface {
	AuthorizationURL(Login) string
	Verify(context.Context, string, Flow) (Identity, error)
}

type Service struct {
	repo     Repository
	provider Provider
	now      func() time.Time
}

func New(repo Repository, provider Provider) *Service {
	return &Service{repo: repo, provider: provider, now: func() time.Time { return time.Now().UTC() }}
}

func secret() string {
	var b [32]byte
	// crypto/rand.Read cannot fail on supported Go platforms.
	_, _ = rand.Read(b[:])
	return base64.RawURLEncoding.EncodeToString(b[:])
}
func ID() string { var b [16]byte; _, _ = rand.Read(b[:]); return hex.EncodeToString(b[:]) }

var idPattern = regexp.MustCompile(`^[0-9a-f]{32}$`)

func ValidID(id string) bool { return idPattern.MatchString(id) }
func validSecret(raw string) bool {
	b, err := base64.RawURLEncoding.Strict().DecodeString(raw)
	return err == nil && len(b) == 32
}
func Hash(raw string) [32]byte { return sha256.Sum256([]byte(raw)) }
func csrf(raw string) string {
	m := hmac.New(sha256.New, []byte(raw))
	m.Write([]byte("ticketopia:csrf:v1"))
	return base64.RawURLEncoding.EncodeToString(m.Sum(nil))
}
func (s *Service) Begin(ctx context.Context, returnTo string) (Login, string, error) {
	l := Login{secret(), secret(), secret(), secret()}
	f := Flow{Hash(l.State), Hash(l.Browser), l.Verifier, l.Nonce, returnTo, s.now().Add(FlowLifetime)}
	if err := s.repo.PutFlow(ctx, f); err != nil {
		return Login{}, "", err
	}
	return l, s.provider.AuthorizationURL(l), nil
}
func (s *Service) Complete(ctx context.Context, state, browser, code string) (Account, string, string, error) {
	if !validSecret(state) || !validSecret(browser) || code == "" || len(code) > 4096 {
		return Account{}, "", "", ErrFlow
	}
	f, err := s.repo.ConsumeFlow(ctx, Hash(state), Hash(browser), s.now())
	if err != nil {
		return Account{}, "", "", err
	}
	identity, err := s.provider.Verify(ctx, code, f)
	if err != nil {
		return Account{}, "", "", err
	}
	// Email never establishes identity or links two accounts.
	if identity.Issuer != GoogleIssuer || identity.Subject == "" || len(identity.Subject) > 255 || identity.Email == "" || len(identity.Email) > 320 || !utf8.ValidString(identity.Email) {
		return Account{}, "", "", ErrFlow
	}
	raw := secret()
	c := s.credential("", "session", "Browser session", raw)
	a, err := s.repo.Login(ctx, identity, c)
	if err != nil {
		return Account{}, "", "", err
	}
	return a, raw, f.ReturnTo, nil
}

func (s *Service) Cancel(ctx context.Context, state, browser string) (string, error) {
	if !validSecret(state) || !validSecret(browser) {
		return "", ErrFlow
	}
	f, err := s.repo.ConsumeFlow(ctx, Hash(state), Hash(browser), s.now())
	return f.ReturnTo, err
}
func (s *Service) credential(owner, kind, name, raw string) Credential {
	now := s.now()
	return Credential{ID(), owner, Hash(raw), kind, name, now, now.Add(CredentialLifetime)}
}
func (s *Service) Authenticate(ctx context.Context, raw, kind string) (Principal, error) {
	if !validSecret(raw) || (kind != "session" && kind != "api") {
		return Principal{}, ErrUnauthenticated
	}
	a, c, err := s.repo.Authenticate(ctx, Hash(raw), kind, s.now())
	if err != nil {
		return Principal{}, err
	}
	return Principal{a, c, csrf(raw)}, nil
}
func (s *Service) Public(ctx context.Context, id string) (PublicProfile, error) {
	if !ValidID(id) {
		return PublicProfile{}, ErrNotFound
	}
	a, err := s.repo.Get(ctx, id)
	return a.Public(), err
}
func validateText(field string, v string, min, max int, multiline bool) error {
	if !utf8.ValidString(v) || utf8.RuneCountInString(v) < min || utf8.RuneCountInString(v) > max {
		return &ValidationError{field, fmt.Sprintf("use %d–%d characters", min, max)}
	}
	for _, c := range v {
		if unicode.IsControl(c) && !(multiline && (c == '\n' || c == '\t')) {
			return &ValidationError{field, "remove control characters"}
		}
	}
	return nil
}
func (s *Service) UpdateProfile(ctx context.Context, owner string, p ProfilePatch) (Account, error) {
	if p.DisplayName == nil && p.Bio == nil && p.InterestVisibility == nil {
		return Account{}, &ValidationError{"body", "provide at least one profile field"}
	}
	if p.DisplayName != nil {
		v := strings.TrimSpace(*p.DisplayName)
		p.DisplayName = &v
		if err := validateText("display_name", v, 1, 80, false); err != nil {
			return Account{}, err
		}
	}
	if p.Bio != nil {
		// Native textarea form submissions use CRLF even when the editor shows LF.
		v := strings.ReplaceAll(strings.ReplaceAll(*p.Bio, "\r\n", "\n"), "\r", "\n")
		p.Bio = &v
		if err := validateText("bio", v, 0, 500, true); err != nil {
			return Account{}, err
		}
	}
	if p.InterestVisibility != nil && *p.InterestVisibility != "private" && *p.InterestVisibility != "public" {
		return Account{}, &ValidationError{"interest_visibility", "choose private or public"}
	}
	return s.repo.UpdateProfile(ctx, owner, p)
}

var categoryPattern = regexp.MustCompile(`^[A-Za-z0-9_-]{1,128}$`)

func (s *Service) UpdatePreferences(ctx context.Context, owner string, p PreferencesPatch) (Account, error) {
	if p == (PreferencesPatch{}) {
		return Account{}, &ValidationError{"body", "provide at least one preference field"}
	}
	if p.City != nil {
		// Match discovery's normalized, byte-bounded city filter so saved
		// locations can always be carried by pagination URLs.
		v := strings.Join(strings.Fields(*p.City), " ")
		p.City = &v
		if err := validateText("city", v, 0, 120, false); err != nil {
			return Account{}, err
		}
		if len(v) > 120 {
			return Account{}, &ValidationError{"city", "use a shorter city name (120 bytes or fewer)"}
		}
	}
	if p.Country != nil {
		v := strings.ToUpper(strings.TrimSpace(*p.Country))
		p.Country = &v
		if v != "" && (len(v) != 2 || v[0] < 'A' || v[0] > 'Z' || v[1] < 'A' || v[1] > 'Z') {
			return Account{}, &ValidationError{"country", "use a two-letter country code or leave blank"}
		}
	}
	if p.Timezone != nil {
		if len(*p.Timezone) > 64 || *p.Timezone == "" || *p.Timezone == "Local" {
			return Account{}, &ValidationError{"timezone", "use an IANA time zone, for example Europe/Berlin"}
		}
		if _, err := time.LoadLocation(*p.Timezone); err != nil {
			return Account{}, &ValidationError{"timezone", "use an IANA time zone, for example Europe/Berlin"}
		}
	}
	if p.CategoryIDs != nil {
		if len(*p.CategoryIDs) > 16 {
			return Account{}, &ValidationError{"category_ids", "choose at most 16 categories"}
		}
		ids := slices.Clone(*p.CategoryIDs)
		for _, id := range ids {
			if !categoryPattern.MatchString(id) {
				return Account{}, &ValidationError{"category_ids", "use category IDs from the catalog"}
			}
		}
		slices.Sort(ids)
		ids = slices.Compact(ids)
		if ids == nil {
			ids = []string{}
		}
		p.CategoryIDs = &ids
	}
	return s.repo.UpdatePreferences(ctx, owner, p)
}
func (s *Service) Tokens(ctx context.Context, owner string) ([]Credential, error) {
	return s.repo.ListTokens(ctx, owner, s.now())
}
func (s *Service) CreateToken(ctx context.Context, owner, name string) (Credential, string, error) {
	name = strings.TrimSpace(name)
	if err := validateText("name", name, 1, 80, false); err != nil {
		return Credential{}, "", err
	}
	raw := secret()
	c := s.credential(owner, "api", name, raw)
	if err := s.repo.CreateToken(ctx, c, s.now()); err != nil {
		return Credential{}, "", err
	}
	return c, raw, nil
}
func (s *Service) Revoke(ctx context.Context, owner, id string) error {
	if !ValidID(id) {
		return &ValidationError{"credential_id", "invalid credential ID"}
	}
	return s.repo.Revoke(ctx, owner, id)
}
func (s *Service) RevokeAll(ctx context.Context, owner string) error {
	return s.repo.RevokeAll(ctx, owner)
}
func (s *Service) Allow(ctx context.Context, scope, ip string) error {
	ok, err := s.repo.Allow(ctx, Hash(scope+":"+ip), s.now(), 10*time.Minute, 20)
	if err != nil {
		return err
	}
	if !ok {
		return ErrLimited
	}
	return nil
}

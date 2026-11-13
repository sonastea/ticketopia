// Package oauth is Ticketopia's OAuth 2.1 authorization server for the agent
// endpoint (/mcp). Clients identify themselves with Client ID Metadata
// Documents (an HTTPS URL as client_id), sign people in through the existing
// account sign-in, and receive opaque, hashed, revocable tokens bound to /mcp.
// Only public clients with PKCE (S256) and the authorization code grant are
// supported; access tokens never work on /api/v1.
package oauth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"net/url"
	"regexp"
	"slices"
	"strings"
	"time"
)

const (
	// ScopeRead covers public tools and reading the account's own saves and reminders.
	ScopeRead = "read"
	// ScopeWrite also allows changing saves and reminders.
	ScopeWrite = "write"

	CodeLifetime   = 5 * time.Minute
	AccessLifetime = time.Hour
	// GrantLifetime is how long a connection lasts without a refresh; each
	// refresh extends it, so an agent in use stays connected until revoked.
	GrantLifetime = 30 * 24 * time.Hour
	// MaxGrants per account; connecting another removes the oldest.
	MaxGrants = 10

	// Token prefixes tell access tokens apart from API tokens without a
	// database lookup and help secret scanners recognize leaked tokens.
	AccessPrefix  = "tko_at_"
	RefreshPrefix = "tko_rt_"
)

// Scopes lists every scope in canonical order.
var Scopes = []string{ScopeRead, ScopeWrite}

var (
	// ErrNotFound is a grant that does not exist for the account.
	ErrNotFound = errors.New("connection not found")
	// ErrUnauthenticated is an unknown, expired or revoked access token.
	ErrUnauthenticated = errors.New("invalid, expired or revoked access token")
	// ErrInvalidGrant is an unknown, expired or used code or refresh token.
	ErrInvalidGrant = errors.New("invalid grant")
	// ErrInvalidScope is a refresh asking for scopes the connection lacks.
	ErrInvalidScope = errors.New("invalid scope")
)

// Error is an OAuth error response (RFC 6749 sections 4.1.2.1 and 5.2).
type Error struct {
	Code, Description string
}

func (e *Error) Error() string { return e.Code + ": " + e.Description }

func oauthError(code, description string) *Error { return &Error{code, description} }

// ClientError is an authorization request that must not redirect back to the
// client: its client_id or redirect_uri cannot be trusted. It is shown to the
// person in the browser instead.
type ClientError struct{ Message string }

func (e *ClientError) Error() string { return e.Message }

// Client is a validated Client ID Metadata Document.
type Client struct {
	ID           string
	Name         string
	RedirectURIs []string
	// Refresh is set when the document lists the refresh_token grant.
	Refresh bool
}

// Host is the hostname serving the client's metadata document: the identity a
// person approves, since no one has reviewed CIMD clients in advance.
func (c Client) Host() string {
	u, err := url.Parse(c.ID)
	if err != nil {
		return ""
	}
	return u.Hostname()
}

// ClientResolver fetches and validates a client's metadata document.
type ClientResolver interface {
	Client(ctx context.Context, id string) (Client, error)
}

// Request is a validated authorization request.
type Request struct {
	Client      Client
	RedirectURI string
	State       string
	Challenge   string
	Scopes      []string
}

// Write reports whether the request asks to change saves and reminders.
func (r Request) Write() bool { return slices.Contains(r.Scopes, ScopeWrite) }

// LocalRedirect reports whether the client receives the code on this
// device's loopback interface (desktop and command-line agents).
func (r Request) LocalRedirect() bool {
	u, err := url.Parse(r.RedirectURI)
	return err == nil && loopback(u)
}

// RedirectHost is where the browser returns after a decision.
func (r Request) RedirectHost() string {
	u, err := url.Parse(r.RedirectURI)
	if err != nil {
		return ""
	}
	if u.Host == "" {
		return u.Scheme + ":"
	}
	return u.Hostname()
}

// Code is a stored, single-use authorization code.
type Code struct {
	Hash        [32]byte
	AccountID   string
	ClientID    string
	ClientName  string
	RedirectURI string
	Challenge   string
	Scopes      []string
	Refresh     bool
	ExpiresAt   time.Time
}

// Grant is one connected agent: the person's consent plus its current tokens.
type Grant struct {
	ID          string    `json:"id"`
	AccountID   string    `json:"-"`
	ClientID    string    `json:"client_id"`
	ClientName  string    `json:"client_name"`
	Scopes      []string  `json:"scopes"`
	CreatedAt   time.Time `json:"created_at"`
	ExpiresAt   time.Time `json:"expires_at"`
	AccessHash  [32]byte  `json:"-"`
	AccessUntil time.Time `json:"-"`
	// RefreshHash is zero when the client cannot refresh.
	RefreshHash [32]byte `json:"-"`
}

// Host is the hostname of the client's metadata document.
func (g Grant) Host() string { return Client{ID: g.ClientID}.Host() }

// Write reports whether the connection may change saves and reminders.
func (g Grant) Write() bool { return slices.Contains(g.Scopes, ScopeWrite) }

// Access is a verified access token.
type Access struct {
	AccountID string
	GrantID   string
	Scopes    []string
}

// Token is a token endpoint response.
type Token struct {
	AccessToken  string `json:"access_token"`
	TokenType    string `json:"token_type"`
	ExpiresIn    int    `json:"expires_in"`
	RefreshToken string `json:"refresh_token,omitempty"`
	Scope        string `json:"scope"`
}

// Repository stores codes and grants. Codes and refresh tokens work at most once.
type Repository interface {
	PutCode(ctx context.Context, code Code) error
	// Redeem consumes an unexpired code and, when grant accepts it, stores the
	// returned grant in the same transaction, serialized with RevokeAll for the
	// code's account. The code is consumed even when grant returns an error,
	// which Redeem then returns unchanged.
	Redeem(ctx context.Context, hash [32]byte, now time.Time, grant func(Code) (Grant, error)) (Grant, error)
	// Refresh replaces the grant's tokens when old is its current refresh
	// token, the grant belongs to clientID, has not expired and holds every
	// requested scope (ErrInvalidScope otherwise, changing nothing). A rotated
	// refresh token presented again revokes its grant: either it leaked or the
	// client raced itself, and the two cannot be told apart (OAuth 2.1
	// section 4.3.1), so the safe outcome is reconnecting.
	Refresh(ctx context.Context, old [32]byte, clientID string, scopes []string, next Grant, now time.Time) (Grant, error)
	Authenticate(ctx context.Context, access [32]byte, now time.Time) (Grant, error)
	ListGrants(ctx context.Context, account string, now time.Time) ([]Grant, error)
	RevokeGrant(ctx context.Context, account, id string) error
	// RevokeToken removes the grant whose current access token (or, when
	// refresh is set, current or rotated refresh token) has this hash, if it
	// belongs to clientID. An unknown token or another client's token changes
	// nothing.
	RevokeToken(ctx context.Context, hash [32]byte, refresh bool, clientID string) (Revocation, error)
	RevokeAll(ctx context.Context, account string) error
}

// Service implements the authorization and token endpoints' rules.
type Service struct {
	repo     Repository
	clients  ClientResolver
	issuer   string
	resource string
	now      func() time.Time
}

// New serves issuer (the site origin, AUTH_BASE_URL) and binds tokens to its
// /mcp resource.
func New(repo Repository, clients ClientResolver, issuer string) *Service {
	issuer = strings.TrimRight(issuer, "/")
	return &Service{repo: repo, clients: clients, issuer: issuer, resource: issuer + "/mcp", now: func() time.Time { return time.Now().UTC() }}
}

// Issuer is the authorization server identifier.
func (s *Service) Issuer() string { return s.issuer }

// Resource is the protected resource tokens are issued for.
func (s *Service) Resource() string { return s.resource }

func secret() string {
	var b [32]byte
	// crypto/rand.Read cannot fail on supported Go platforms.
	_, _ = rand.Read(b[:])
	return base64.RawURLEncoding.EncodeToString(b[:])
}

func newID() string {
	var b [16]byte
	_, _ = rand.Read(b[:])
	return hex.EncodeToString(b[:])
}

var idPattern = regexp.MustCompile(`^[0-9a-f]{32}$`)

// ValidID reports whether id has the shape of a grant ID.
func ValidID(id string) bool { return idPattern.MatchString(id) }

// Hash is the stored form of a code or token.
func Hash(raw string) [32]byte { return sha256.Sum256([]byte(raw)) }

// pkcePattern is RFC 7636's code_verifier/code_challenge alphabet and length.
var pkcePattern = regexp.MustCompile(`^[A-Za-z0-9._~-]{43,128}$`)

// single returns a parameter that must appear at most once.
func single(values url.Values, key string) (string, bool) {
	v := values[key]
	if len(v) > 1 {
		return "", false
	}
	if len(v) == 0 {
		return "", true
	}
	return v[0], true
}

// ParseScopes accepts space-separated known scopes. Empty means every scope:
// the consent page still lets the person withhold changes.
func ParseScopes(raw string) ([]string, bool) {
	fields := strings.Fields(raw)
	if len(fields) == 0 {
		return slices.Clone(Scopes), true
	}
	var result []string
	for _, f := range fields {
		if !slices.Contains(Scopes, f) {
			return nil, false
		}
		if !slices.Contains(result, f) {
			result = append(result, f)
		}
	}
	// Changing saves implies reading them.
	if slices.Contains(result, ScopeWrite) && !slices.Contains(result, ScopeRead) {
		result = append(result, ScopeRead)
	}
	slices.SortFunc(result, func(a, b string) int { return slices.Index(Scopes, a) - slices.Index(Scopes, b) })
	return result, true
}

// ParseAuthorization validates an authorization request. A *ClientError
// means the request cannot be answered through its redirect_uri; any other
// *Error is redirected to the client with ErrorRedirect.
func (s *Service) ParseAuthorization(ctx context.Context, values url.Values) (Request, error) {
	for _, key := range []string{"client_id", "redirect_uri", "response_type", "state", "scope", "code_challenge", "code_challenge_method", "resource"} {
		if _, ok := single(values, key); !ok {
			if key == "client_id" || key == "redirect_uri" {
				return Request{}, &ClientError{"The app sent an invalid sign-in request. Return to the app and try connecting again."}
			}
		}
	}
	id, _ := single(values, "client_id")
	if id == "" {
		return Request{}, &ClientError{"The app didn't identify itself. Return to the app and try connecting again."}
	}
	client, err := s.clients.Client(ctx, id)
	if err != nil {
		var ce *ClientError
		if errors.As(err, &ce) {
			return Request{}, err
		}
		return Request{}, &ClientError{"We couldn't verify this app right now. Try connecting again shortly."}
	}
	redirect, _ := single(values, "redirect_uri")
	if redirect == "" || !RedirectAllowed(client.RedirectURIs, redirect) {
		return Request{}, &ClientError{"This app asked to return somewhere it hasn't registered, so we stopped. Return to the app and try connecting again."}
	}
	r := Request{Client: client, RedirectURI: redirect}
	for _, key := range []string{"response_type", "state", "scope", "code_challenge", "code_challenge_method", "resource"} {
		if _, ok := single(values, key); !ok {
			return r, oauthError("invalid_request", key+" must not be repeated")
		}
	}
	r.State, _ = single(values, "state")
	if len(r.State) > 1024 {
		r.State = ""
		return r, oauthError("invalid_request", "state is too long")
	}
	if rt, _ := single(values, "response_type"); rt != "code" {
		return r, oauthError("unsupported_response_type", "only response_type=code is supported")
	}
	if method, _ := single(values, "code_challenge_method"); method != "S256" {
		return r, oauthError("invalid_request", "PKCE with code_challenge_method=S256 is required")
	}
	r.Challenge, _ = single(values, "code_challenge")
	if !pkcePattern.MatchString(r.Challenge) {
		return r, oauthError("invalid_request", "code_challenge is missing or malformed")
	}
	if resource, _ := single(values, "resource"); resource != "" && !s.validResource(resource) {
		return r, oauthError("invalid_target", "resource must be "+s.resource)
	}
	scope, _ := single(values, "scope")
	scopes, ok := ParseScopes(scope)
	if !ok {
		return r, oauthError("invalid_scope", "supported scopes are read and write")
	}
	r.Scopes = scopes
	return r, nil
}

func (s *Service) validResource(raw string) bool {
	return strings.TrimRight(raw, "/") == s.resource
}

// ErrorRedirect returns the client's redirect URI carrying an error response.
func (s *Service) ErrorRedirect(r Request, err *Error) string {
	values := url.Values{"error": {err.Code}}
	if err.Description != "" {
		values.Set("error_description", err.Description)
	}
	return s.redirect(r, values)
}

func (s *Service) redirect(r Request, values url.Values) string {
	if r.State != "" {
		values.Set("state", r.State)
	}
	// RFC 9207: name the issuer so clients can detect mix-up attacks.
	values.Set("iss", s.issuer)
	u, _ := url.Parse(r.RedirectURI)
	query := u.Query()
	for key, v := range values {
		query[key] = v
	}
	u.RawQuery = query.Encode()
	return u.String()
}

// Approve issues a code for the scopes the person allowed, a subset of the
// request's, and returns the client redirect carrying it.
func (s *Service) Approve(ctx context.Context, account string, r Request, scopes []string) (string, error) {
	granted := []string{}
	for _, scope := range scopes {
		if slices.Contains(r.Scopes, scope) && !slices.Contains(granted, scope) {
			granted = append(granted, scope)
		}
	}
	if !slices.Contains(granted, ScopeRead) {
		granted = append([]string{ScopeRead}, granted...)
	}
	raw := secret()
	code := Code{Hash(raw), account, r.Client.ID, r.Client.Name, r.RedirectURI, r.Challenge, granted, r.Client.Refresh, s.now().Add(CodeLifetime)}
	if err := s.repo.PutCode(ctx, code); err != nil {
		return "", err
	}
	return s.redirect(r, url.Values{"code": {raw}}), nil
}

// Exchange answers a token request (authorization_code or refresh_token).
func (s *Service) Exchange(ctx context.Context, form url.Values) (Token, error) {
	for key, v := range form {
		if len(v) > 1 {
			return Token{}, oauthError("invalid_request", key+" must not be repeated")
		}
	}
	if resource := form.Get("resource"); resource != "" && !s.validResource(resource) {
		return Token{}, oauthError("invalid_target", "resource must be "+s.resource)
	}
	clientID := form.Get("client_id")
	if clientID == "" {
		return Token{}, oauthError("invalid_client", "client_id is required")
	}
	switch form.Get("grant_type") {
	case "authorization_code":
		return s.exchangeCode(ctx, clientID, form)
	case "refresh_token":
		return s.refresh(ctx, clientID, form)
	case "":
		return Token{}, oauthError("invalid_request", "grant_type is required")
	default:
		return Token{}, oauthError("unsupported_grant_type", "use authorization_code or refresh_token")
	}
}

func (s *Service) exchangeCode(ctx context.Context, clientID string, form url.Values) (Token, error) {
	raw, verifier := form.Get("code"), form.Get("code_verifier")
	if raw == "" || len(raw) > 128 {
		return Token{}, oauthError("invalid_request", "code is required")
	}
	if !pkcePattern.MatchString(verifier) {
		return Token{}, oauthError("invalid_request", "code_verifier is missing or malformed")
	}
	now := s.now()
	access, refresh := AccessPrefix+secret(), ""
	sum := sha256.Sum256([]byte(verifier))
	challenge := base64.RawURLEncoding.EncodeToString(sum[:])
	g, err := s.repo.Redeem(ctx, Hash(raw), now, func(code Code) (Grant, error) {
		// OAuth 2.1 drops redirect_uri from token requests (PKCE binds the
		// code); when a client still sends it, it must match.
		redirect, sent := form["redirect_uri"]
		if code.ClientID != clientID || (sent && redirect[0] != code.RedirectURI) ||
			subtle.ConstantTimeCompare([]byte(challenge), []byte(code.Challenge)) != 1 {
			return Grant{}, oauthError("invalid_grant", "the code does not match this client, redirect_uri or code_verifier")
		}
		g := Grant{ID: newID(), AccountID: code.AccountID, ClientID: code.ClientID, ClientName: code.ClientName, Scopes: code.Scopes,
			CreatedAt: now, ExpiresAt: now.Add(GrantLifetime), AccessHash: Hash(access), AccessUntil: now.Add(AccessLifetime)}
		if code.Refresh {
			refresh = RefreshPrefix + secret()
			g.RefreshHash = Hash(refresh)
		}
		return g, nil
	})
	if errors.Is(err, ErrInvalidGrant) {
		return Token{}, oauthError("invalid_grant", "the code is invalid, expired or already used")
	}
	if err != nil {
		return Token{}, err
	}
	return Token{access, "Bearer", int(AccessLifetime.Seconds()), refresh, strings.Join(g.Scopes, " ")}, nil
}

func (s *Service) refresh(ctx context.Context, clientID string, form url.Values) (Token, error) {
	raw := form.Get("refresh_token")
	if !strings.HasPrefix(raw, RefreshPrefix) || len(raw) > 128 {
		return Token{}, oauthError("invalid_grant", "the refresh token is invalid, expired or revoked")
	}
	var requested []string
	if scope := form.Get("scope"); scope != "" {
		var ok bool
		if requested, ok = ParseScopes(scope); !ok {
			return Token{}, oauthError("invalid_scope", "supported scopes are read and write")
		}
	}
	now := s.now()
	access, refresh := AccessPrefix+secret(), RefreshPrefix+secret()
	next := Grant{ExpiresAt: now.Add(GrantLifetime), AccessHash: Hash(access), AccessUntil: now.Add(AccessLifetime), RefreshHash: Hash(refresh)}
	g, err := s.repo.Refresh(ctx, Hash(raw), clientID, requested, next, now)
	switch {
	case errors.Is(err, ErrInvalidGrant):
		return Token{}, oauthError("invalid_grant", "the refresh token is invalid, expired or revoked")
	case errors.Is(err, ErrInvalidScope):
		return Token{}, oauthError("invalid_scope", "a refresh cannot add scopes; connect again to allow changes")
	case err != nil:
		return Token{}, err
	}
	return Token{access, "Bearer", int(AccessLifetime.Seconds()), refresh, strings.Join(g.Scopes, " ")}, nil
}

// Subset reports whether every requested scope is held.
func Subset(requested, held []string) bool {
	for _, scope := range requested {
		if !slices.Contains(held, scope) {
			return false
		}
	}
	return true
}

// Authenticate verifies an access token for /mcp.
func (s *Service) Authenticate(ctx context.Context, raw string) (Access, error) {
	if !strings.HasPrefix(raw, AccessPrefix) || len(raw) > 128 {
		return Access{}, ErrUnauthenticated
	}
	g, err := s.repo.Authenticate(ctx, Hash(raw), s.now())
	if err != nil {
		return Access{}, err
	}
	return Access{AccountID: g.AccountID, GrantID: g.ID, Scopes: g.Scopes}, nil
}

// Grants lists the account's connected agents, newest first.
func (s *Service) Grants(ctx context.Context, account string) ([]Grant, error) {
	return s.repo.ListGrants(ctx, account, s.now())
}

// Revoke disconnects one agent; its tokens stop working immediately.
func (s *Service) Revoke(ctx context.Context, account, id string) error {
	if !ValidID(id) {
		return ErrNotFound
	}
	return s.repo.RevokeGrant(ctx, account, id)
}

// Revocation is what a revocation request did. Clients always get the same
// answer; this is for logs.
type Revocation string

const (
	// Revoked: a current token ended its connection.
	Revoked Revocation = "revoked"
	// RevokedRotated: a rotated refresh token ended its connection. Either
	// the client kept a stale token or it leaked; as with a replay at the
	// token endpoint, the safe outcome is reconnecting.
	RevokedRotated Revocation = "revoked_rotated"
	// RevokeUnknown: no connection holds the token (unknown, expired, already
	// revoked, a replaced access token, or not a Ticketopia OAuth token).
	RevokeUnknown Revocation = "unknown"
	// RevokeOtherClient: the token belongs to a different client_id, so
	// nothing changed: a client bug, or a token in the wrong hands.
	RevokeOtherClient Revocation = "other_client"
)

// RevokeToken answers an RFC 7009 revocation request: a client disconnecting
// itself. Either of a connection's current tokens, or a refresh token it has
// rotated away, ends the whole connection, as the account page's Disconnect
// does. Unknown, expired or other clients' tokens succeed without changing
// anything (section 2.2), so the response reveals nothing about tokens the
// caller does not hold. The token's prefix identifies its type, so
// token_type_hint is accepted and ignored.
func (s *Service) RevokeToken(ctx context.Context, form url.Values) (Revocation, error) {
	for key, v := range form {
		if len(v) > 1 {
			return "", oauthError("invalid_request", key+" must not be repeated")
		}
	}
	clientID := form.Get("client_id")
	if clientID == "" {
		return "", oauthError("invalid_client", "client_id is required")
	}
	raw := form.Get("token")
	if raw == "" {
		return "", oauthError("invalid_request", "token is required")
	}
	if len(raw) > 128 {
		return RevokeUnknown, nil
	}
	switch {
	case strings.HasPrefix(raw, AccessPrefix):
		return s.repo.RevokeToken(ctx, Hash(raw), false, clientID)
	case strings.HasPrefix(raw, RefreshPrefix):
		return s.repo.RevokeToken(ctx, Hash(raw), true, clientID)
	default:
		return RevokeUnknown, nil
	}
}

// RevokeAll disconnects every agent and discards pending codes.
func (s *Service) RevokeAll(ctx context.Context, account string) error {
	return s.repo.RevokeAll(ctx, account)
}

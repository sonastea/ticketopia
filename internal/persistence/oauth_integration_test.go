package persistence

import (
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/sonastea/ticketopia/internal/accounts"
	"github.com/sonastea/ticketopia/internal/oauth"
	"github.com/sonastea/ticketopia/internal/oauth/oauthtest"
)

func TestMariaDBOAuthCodesGrantsAndRevocation(t *testing.T) {
	f := newMaria(t)
	f.migrate(t)
	pools := []*Pool{f.open(t), f.open(t), f.open(t)}
	auth := accounts.New(pools[0].Accounts(), accountProviderFixture{})
	owner, _ := loginAccount(t, auth, "oauth-owner")
	other, _ := loginAccount(t, auth, "oauth-other")
	const client = "https://agent.example/oauth/client.json"
	clients := oauthtest.Clients{client: {ID: client, Name: "Agent ✓", RedirectURIs: []string{"http://127.0.0.1/callback"}, Refresh: true}}
	services := make([]*oauth.Service, len(pools))
	for i, p := range pools {
		services[i] = oauth.New(p.OAuth(), clients, "https://ticketopia.example")
	}
	const verifier = "a-verifier-that-is-long-enough-for-pkce-0123456789"
	sum := sha256.Sum256([]byte(verifier))
	challenge := base64.RawURLEncoding.EncodeToString(sum[:])
	approve := func(account string, scopes ...string) string {
		t.Helper()
		values := url.Values{"response_type": {"code"}, "client_id": {client}, "redirect_uri": {"http://127.0.0.1:5000/callback"},
			"code_challenge": {challenge}, "code_challenge_method": {"S256"}}
		r, err := services[0].ParseAuthorization(t.Context(), values)
		if err != nil {
			t.Fatal(err)
		}
		redirect, err := services[0].Approve(t.Context(), account, r, scopes)
		if err != nil {
			t.Fatal(err)
		}
		u, _ := url.Parse(redirect)
		return u.Query().Get("code")
	}
	exchange := func(i int, form url.Values) (oauth.Token, error) {
		return services[i%len(services)].Exchange(t.Context(), form)
	}
	codeForm := func(code, v string) url.Values {
		return url.Values{"grant_type": {"authorization_code"}, "code": {code}, "client_id": {client},
			"redirect_uri": {"http://127.0.0.1:5000/callback"}, "code_verifier": {v}}
	}
	if _, err := exchange(0, codeForm(approve(owner.ID), strings.Repeat("x", 43))); err == nil {
		t.Fatal("wrong verifier accepted")
	}

	// A code is redeemed exactly once across replicas.
	code := approve(owner.ID, oauth.ScopeRead, oauth.ScopeWrite)
	var issued atomic.Int32
	var token oauth.Token
	var mu sync.Mutex
	var wg sync.WaitGroup
	for i := range 9 {
		wg.Go(func() {
			got, err := exchange(i, codeForm(code, verifier))
			if err == nil {
				issued.Add(1)
				mu.Lock()
				token = got
				mu.Unlock()
			}
		})
	}
	wg.Wait()
	if issued.Load() != 1 || token.Scope != "read write" {
		t.Fatalf("code redeemed %d times, token %+v", issued.Load(), token)
	}
	access, err := services[1].Authenticate(t.Context(), token.AccessToken)
	if err != nil || access.AccountID != owner.ID || len(access.Scopes) != 2 {
		t.Fatalf("access %+v %v", access, err)
	}

	// A refresh token rotates exactly once across replicas; the losing
	// presentations are replays, which revoke the connection.
	refresh := url.Values{"grant_type": {"refresh_token"}, "refresh_token": {token.RefreshToken}, "client_id": {client}}
	issued.Store(0)
	var rotated oauth.Token
	for i := range 9 {
		wg.Go(func() {
			got, err := exchange(i, refresh)
			if err == nil {
				issued.Add(1)
				mu.Lock()
				rotated = got
				mu.Unlock()
			}
		})
	}
	wg.Wait()
	if issued.Load() != 1 {
		t.Fatalf("refresh token rotated %d times", issued.Load())
	}
	if _, err := services[2].Authenticate(t.Context(), token.AccessToken); !errors.Is(err, oauth.ErrUnauthenticated) {
		t.Fatalf("replaced access token: %v", err)
	}
	if _, err := services[2].Authenticate(t.Context(), rotated.AccessToken); !errors.Is(err, oauth.ErrUnauthenticated) {
		t.Fatalf("connection survived replayed refreshes: %v", err)
	}
	// Sequentially: a replay after a successful rotation revokes the new tokens too.
	rotated, err = exchange(0, codeForm(approve(owner.ID, oauth.ScopeRead), verifier))
	if err != nil {
		t.Fatal(err)
	}
	refresh = url.Values{"grant_type": {"refresh_token"}, "refresh_token": {rotated.RefreshToken}, "client_id": {client}}
	if rotated, err = exchange(1, refresh); err != nil {
		t.Fatal(err)
	}
	if _, err := exchange(1, refresh); err == nil {
		t.Fatal("replayed refresh token accepted")
	}
	if _, err := services[2].Authenticate(t.Context(), rotated.AccessToken); !errors.Is(err, oauth.ErrUnauthenticated) {
		t.Fatalf("replay left the connection active: %v", err)
	}
	if _, err := exchange(2, url.Values{"grant_type": {"refresh_token"}, "refresh_token": {rotated.RefreshToken}, "client_id": {client}}); err == nil {
		t.Fatal("latest refresh token survived a replay")
	}
	rotated, err = exchange(0, codeForm(approve(owner.ID, oauth.ScopeRead), verifier))
	if err != nil {
		t.Fatal(err)
	}

	// Connections are owner-only and capped, oldest removed first.
	readOnly, err := exchange(0, codeForm(approve(other.ID), verifier))
	if err != nil || readOnly.Scope != "read" {
		t.Fatalf("other account: %+v %v", readOnly, err)
	}
	for range oauth.MaxGrants {
		if _, err := exchange(0, codeForm(approve(owner.ID), verifier)); err != nil {
			t.Fatal(err)
		}
	}
	grants, err := services[0].Grants(t.Context(), owner.ID)
	if err != nil || len(grants) != oauth.MaxGrants || grants[0].ClientName != "Agent ✓" {
		t.Fatalf("owner grants %d %v", len(grants), err)
	}
	if _, err := services[0].Authenticate(t.Context(), rotated.AccessToken); !errors.Is(err, oauth.ErrUnauthenticated) {
		t.Fatalf("oldest connection survived the cap: %v", err)
	}
	if err := services[0].Revoke(t.Context(), owner.ID, mustOtherGrant(t, services[0], other.ID)); err != nil {
		t.Fatal(err)
	}
	if _, err := services[0].Authenticate(t.Context(), readOnly.AccessToken); err != nil {
		t.Fatal("an account revoked another account's connection")
	}
	if err := services[0].Revoke(t.Context(), other.ID, mustOtherGrant(t, services[0], other.ID)); err != nil {
		t.Fatal(err)
	}
	if _, err := services[0].Authenticate(t.Context(), readOnly.AccessToken); !errors.Is(err, oauth.ErrUnauthenticated) {
		t.Fatalf("revoked connection: %v", err)
	}
	pending := approve(owner.ID)
	if err := services[1].RevokeAll(t.Context(), owner.ID); err != nil {
		t.Fatal(err)
	}
	if grants, _ := services[0].Grants(t.Context(), owner.ID); len(grants) != 0 {
		t.Fatalf("connections after revoke all: %d", len(grants))
	}
	if _, err := exchange(0, codeForm(pending, verifier)); err == nil {
		t.Fatal("pending code survived revoke all")
	}
	// Redemptions racing "sign out everywhere" never leave a connection: each
	// either finishes first (and is removed) or finds its code gone.
	pendingCodes := make([]string, 12)
	for i := range pendingCodes {
		pendingCodes[i] = approve(owner.ID)
	}
	for i, c := range pendingCodes {
		wg.Go(func() { _, _ = exchange(i, codeForm(c, verifier)) })
		if i == len(pendingCodes)/2 {
			wg.Go(func() {
				if err := services[2].RevokeAll(t.Context(), owner.ID); err != nil {
					t.Error(err)
				}
			})
		}
	}
	wg.Wait()
	// RevokeAll removed every pending code, so no later redemption succeeded either.
	if grants, _ := services[0].Grants(t.Context(), owner.ID); len(grants) != 0 {
		t.Fatalf("connections outlived sign-out: %d", len(grants))
	}
	// Expired codes are removed when new ones are stored.
	execSQL(t, f.admin, "UPDATE "+f.runtime.Database+".oauth_codes SET expires_at=UTC_TIMESTAMP(6) - INTERVAL 1 SECOND")
	approve(owner.ID)
	var codes int
	if err := f.admin.QueryRowContext(t.Context(), "SELECT COUNT(*) FROM "+f.runtime.Database+".oauth_codes").Scan(&codes); err != nil || codes != 1 {
		t.Fatalf("codes after cleanup: %d %v", codes, err)
	}
}

func mustOtherGrant(t *testing.T, s *oauth.Service, account string) string {
	t.Helper()
	grants, err := s.Grants(t.Context(), account)
	if err != nil || len(grants) != 1 {
		t.Fatalf("grants %d %v", len(grants), err)
	}
	return grants[0].ID
}

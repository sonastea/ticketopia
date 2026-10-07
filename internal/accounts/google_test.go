package accounts

import (
	"crypto/rand"
	"crypto/rsa"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
	jose "github.com/go-jose/go-jose/v4"
	"golang.org/x/oauth2"
)

func TestGoogleAuthorizationUsesCodePKCEAndNonce(t *testing.T) {
	g := NewGoogle(t.Context(), Config{true, "https://events.example", "google-client", "secret"})
	l := Login{secret(), secret(), secret(), secret()}
	u, err := url.Parse(g.AuthorizationURL(l))
	if err != nil {
		t.Fatal(err)
	}
	q := u.Query()
	if u.Host != "accounts.google.com" || q.Get("response_type") != "code" || q.Get("state") != l.State || q.Get("nonce") != l.Nonce || q.Get("code_challenge") != oauth2.S256ChallengeFromVerifier(l.Verifier) || q.Get("code_challenge_method") != "S256" || q.Get("scope") != "openid email" || q.Get("redirect_uri") != "https://events.example/auth/google/callback" || strings.Contains(u.String(), g.oauth.ClientSecret) {
		t.Fatal("unsafe Google authorization request", u.String())
	}
}

func TestGoogleVerifiesSignedIdentityAndRejectsInvalidClaims(t *testing.T) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	other, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	flow := Flow{Verifier: secret(), Nonce: secret()}
	for _, tc := range []struct {
		name     string
		modify   func(map[string]any)
		wrongKey bool
		valid    bool
	}{
		{"valid", nil, false, true},
		{"wrong issuer", func(c map[string]any) { c["iss"] = "https://attacker.example" }, false, false},
		{"wrong audience", func(c map[string]any) { c["aud"] = "other-client" }, false, false},
		{"expired", func(c map[string]any) { c["exp"] = time.Now().Add(-time.Hour).Unix() }, false, false},
		{"wrong nonce", func(c map[string]any) { c["nonce"] = "replayed-nonce" }, false, false},
		{"missing nonce", func(c map[string]any) { delete(c, "nonce") }, false, false},
		{"unverified email", func(c map[string]any) { c["email_verified"] = false }, false, false},
		{"missing email", func(c map[string]any) { delete(c, "email") }, false, false},
		{"wrong authorized party", func(c map[string]any) { c["azp"] = "other-client" }, false, false},
		{"wrong signature", nil, true, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			claims := map[string]any{"iss": GoogleIssuer, "sub": "stable-google-subject", "aud": "google-client", "iat": time.Now().Unix(), "exp": time.Now().Add(time.Hour).Unix(), "nonce": flow.Nonce, "email": "verified@example.com", "email_verified": true}
			if tc.modify != nil {
				tc.modify(claims)
			}
			signKey := key
			if tc.wrongKey {
				signKey = other
			}
			signer, err := jose.NewSigner(jose.SigningKey{Algorithm: jose.RS256, Key: signKey}, (&jose.SignerOptions{}).WithHeader("kid", "fixture"))
			if err != nil {
				t.Fatal(err)
			}
			payload, _ := json.Marshal(claims)
			signed, err := signer.Sign(payload)
			if err != nil {
				t.Fatal(err)
			}
			raw, err := signed.CompactSerialize()
			if err != nil {
				t.Fatal(err)
			}
			tokenServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				if r.URL.Path == "/keys" {
					_ = json.NewEncoder(w).Encode(jose.JSONWebKeySet{Keys: []jose.JSONWebKey{{Key: &key.PublicKey, KeyID: "fixture", Algorithm: "RS256", Use: "sig"}}})
					return
				}
				if err := r.ParseForm(); err != nil {
					t.Error(err)
				}
				id, secret, ok := r.BasicAuth()
				if !ok || id != "google-client" || secret != "oauth-secret" || r.Form.Get("grant_type") != "authorization_code" || r.Form.Get("code") != "provider-code" || r.Form.Get("code_verifier") != flow.Verifier || r.Form.Get("redirect_uri") != "https://events.example/auth/google/callback" {
					t.Error("wrong code exchange")
				}
				_ = json.NewEncoder(w).Encode(map[string]any{"access_token": "unused-google-access-secret", "token_type": "Bearer", "expires_in": 3600, "id_token": raw})
			}))
			defer tokenServer.Close()
			g := NewGoogle(t.Context(), Config{true, "https://events.example", "google-client", "oauth-secret"})
			g.oauth.Endpoint = oauth2.Endpoint{TokenURL: tokenServer.URL + "/token", AuthStyle: oauth2.AuthStyleInHeader}
			g.verifier = oidc.NewVerifier(GoogleIssuer, oidc.NewRemoteKeySet(t.Context(), tokenServer.URL+"/keys"), &oidc.Config{ClientID: "google-client"})
			identity, err := g.Verify(t.Context(), "provider-code", flow)
			if tc.valid {
				if err != nil || identity.Subject != "stable-google-subject" || identity.Email != "verified@example.com" {
					t.Fatal(identity, err)
				}
			} else if err == nil {
				t.Fatal("invalid signed identity accepted")
			}
			if err != nil && (strings.Contains(err.Error(), raw) || strings.Contains(err.Error(), "oauth-secret") || strings.Contains(err.Error(), "unused-google-access-secret")) {
				t.Fatal("provider credentials leaked")
			}
		})
	}
}

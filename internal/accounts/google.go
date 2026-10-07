package accounts

import (
	"context"
	"crypto/subtle"
	"errors"
	"net/http"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"
)

// oauth2 implements the authorization-code exchange; OIDC verifies the signed
// identity assertion. Access/refresh tokens are neither persisted nor exposed.
type Google struct {
	oauth    oauth2.Config
	verifier *oidc.IDTokenVerifier
	client   *http.Client
}

func NewGoogle(ctx context.Context, c Config) *Google {
	client := &http.Client{Timeout: 10 * time.Second}
	ctx = oidc.ClientContext(ctx, client)
	keys := oidc.NewRemoteKeySet(ctx, "https://www.googleapis.com/oauth2/v3/certs")
	return &Google{oauth2.Config{ClientID: c.ClientID, ClientSecret: c.ClientSecret,
		RedirectURL: c.BaseURL + "/auth/google/callback", Endpoint: google.Endpoint,
		Scopes: []string{"openid", "email"}}, oidc.NewVerifier(GoogleIssuer, keys, &oidc.Config{ClientID: c.ClientID}), client}
}
func (g *Google) AuthorizationURL(l Login) string {
	return g.oauth.AuthCodeURL(l.State, oauth2.S256ChallengeOption(l.Verifier), oauth2.SetAuthURLParam("nonce", l.Nonce))
}

var ErrProvider = errors.New("Google sign-in could not be completed; please try again")

func (g *Google) Verify(ctx context.Context, code string, f Flow) (Identity, error) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	ctx = context.WithValue(ctx, oauth2.HTTPClient, g.client)
	token, err := g.oauth.Exchange(ctx, code, oauth2.VerifierOption(f.Verifier))
	if err != nil {
		return Identity{}, ErrProvider
	}
	raw, ok := token.Extra("id_token").(string)
	if !ok || len(raw) > 16384 {
		return Identity{}, ErrFlow
	}
	id, err := g.verifier.Verify(ctx, raw)
	if err != nil {
		return Identity{}, ErrFlow
	}
	var claims struct {
		Nonce           string `json:"nonce"`
		Email           string `json:"email"`
		Verified        bool   `json:"email_verified"`
		AuthorizedParty string `json:"azp"`
	}
	if err := id.Claims(&claims); err != nil || !claims.Verified || claims.Email == "" || subtle.ConstantTimeCompare([]byte(claims.Nonce), []byte(f.Nonce)) != 1 || (claims.AuthorizedParty != "" && claims.AuthorizedParty != g.oauth.ClientID) {
		return Identity{}, ErrFlow
	}
	return Identity{GoogleIssuer, id.Subject, claims.Email}, nil
}

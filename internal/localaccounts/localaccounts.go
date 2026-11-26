// Package localaccounts creates throwaway accounts for trying Ticketopia's
// agent tools (/mcp) and account pages against a local server and database:
// each with a browser session and API token, plus OAuth connections like an
// approved agent's, so the publish scope can be exercised without a hosted
// client. It refuses anything but a loopback site and database. Accounts are
// marked (persistence.LocalTestSubjectPrefix and LocalTestEmailDomain) and
// removed with persistence.Pool.RemoveLocalTestAccounts.
package localaccounts

import (
	"context"
	"fmt"
	"net"
	"net/url"
	"strings"
	"time"

	"github.com/sonastea/ticketopia/internal/accounts"
	"github.com/sonastea/ticketopia/internal/oauth"
	"github.com/sonastea/ticketopia/internal/persistence"
)

// ClientID and ClientName identify the agent the OAuth connections stand
// for; posts it drafts show "via Local Test Agent (local-test-agent.invalid)".
const (
	ClientID   = "https://local-test-agent.invalid/oauth/client.json"
	ClientName = "Local Test Agent"
)

// Account is one throwaway account's credentials. Session is the browser
// session cookie's value; OAuthPublish holds read, write and publish and
// OAuthReadOnly only read, each an access token valid until OAuthExpiresAt.
type Account struct {
	ID             string    `json:"id"`
	DisplayName    string    `json:"display_name"`
	Session        string    `json:"session"`
	APIToken       string    `json:"api_token"`
	OAuthPublish   string    `json:"oauth_publish"`
	OAuthReadOnly  string    `json:"oauth_read_only"`
	OAuthExpiresAt time.Time `json:"oauth_expires_at"`
}

// Result is what a person or agent needs to call the local server.
type Result struct {
	BaseURL       string             `json:"base_url"`
	MCPURL        string             `json:"mcp_url"`
	DraftsURL     string             `json:"drafts_url"`
	SessionCookie string             `json:"session_cookie"`
	Accounts      map[string]Account `json:"accounts"`
	Note          string             `json:"note"`
}

// Guard refuses a site or database host that is not loopback, so the tool
// never creates or deletes accounts on a shared or production deployment.
func Guard(baseURL, dbHost string) error {
	u, err := url.Parse(baseURL)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || !loopback(u.Hostname()) {
		return fmt.Errorf("AUTH_BASE_URL must be a localhost address, got %q", baseURL)
	}
	if !loopback(dbHost) {
		return fmt.Errorf("DB_HOST must be localhost or a loopback address, got %q", dbHost)
	}
	return nil
}

func loopback(host string) bool {
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(strings.Trim(host, "[]"))
	return ip != nil && ip.IsLoopback()
}

// provider stands in for Google: the code is the marked subject.
type provider struct{}

func (provider) AuthorizationURL(l accounts.Login) string {
	return "https://accounts.invalid/authorize?state=" + l.State
}
func (provider) Verify(_ context.Context, code string, _ accounts.Flow) (accounts.Identity, error) {
	return accounts.Identity{Issuer: accounts.GoogleIssuer, Subject: code,
		Email: strings.TrimPrefix(code, persistence.LocalTestSubjectPrefix) + persistence.LocalTestEmailDomain}, nil
}

// Create replaces any existing throwaway accounts with two fresh ones,
// "owner" (city Chicago, US) and "other", through the same sign-in, token
// and OAuth services the app uses. p needs operator (migration) access.
func Create(ctx context.Context, p *persistence.Pool, baseURL string) (Result, error) {
	if _, err := p.RemoveLocalTestAccounts(ctx); err != nil {
		return Result{}, err
	}
	base := strings.TrimRight(baseURL, "/")
	auth := accounts.New(p.Accounts(), provider{})
	run := fmt.Sprint(time.Now().UnixNano())
	result := Result{BaseURL: base, MCPURL: base + "/mcp", DraftsURL: base + "/me/drafts",
		SessionCookie: accounts.Config{BaseURL: base}.CookieName("session"), Accounts: map[string]Account{},
		Note: "Local test accounts; rerun to refresh OAuth tokens (one hour). Remove with make local-test-accounts-clean."}
	for _, who := range []struct{ key, name, first string }{{"owner", "Local Test Owner", "Owner"}, {"other", "Local Test Other", "Other"}} {
		l, _, err := auth.Begin(ctx, "/me")
		if err != nil {
			return Result{}, err
		}
		a, session, _, err := auth.Complete(ctx, l.State, l.Browser, persistence.LocalTestSubjectPrefix+who.key+"-"+run)
		if err != nil {
			return Result{}, err
		}
		bio := "Throwaway account for local agent testing."
		if _, err := auth.UpdateProfile(ctx, a.ID, accounts.ProfilePatch{DisplayName: &who.name, ActivityFirstName: &who.first, Bio: &bio}); err != nil {
			return Result{}, err
		}
		if who.key == "owner" {
			city, country, zone := "Chicago", "US", "America/Chicago"
			if _, err := auth.UpdatePreferences(ctx, a.ID, accounts.PreferencesPatch{City: &city, Country: &country, Timezone: &zone}); err != nil {
				return Result{}, err
			}
		}
		_, token, err := auth.CreateToken(ctx, a.ID, "local agent testing", []string{accounts.ScopeRead, accounts.ScopeWrite})
		if err != nil {
			return Result{}, err
		}
		account := Account{ID: a.ID, DisplayName: who.name, Session: session, APIToken: token}
		now := time.Now().UTC()
		account.OAuthExpiresAt = now.Add(oauth.AccessLifetime)
		for _, grant := range []struct {
			token  *string
			scopes []string
		}{
			{&account.OAuthPublish, []string{oauth.ScopeRead, oauth.ScopeWrite, oauth.ScopePublish}},
			{&account.OAuthReadOnly, []string{oauth.ScopeRead}},
		} {
			*grant.token = oauth.AccessPrefix + accounts.ID()
			if err := p.OAuth().CreateGrant(ctx, oauth.Grant{ID: accounts.ID(), AccountID: a.ID, ClientID: ClientID, ClientName: ClientName,
				Scopes: grant.scopes, CreatedAt: now, ExpiresAt: now.Add(oauth.GrantLifetime),
				AccessHash: oauth.Hash(*grant.token), AccessUntil: account.OAuthExpiresAt}, now); err != nil {
				return Result{}, err
			}
		}
		result.Accounts[who.key] = account
	}
	return result, nil
}

package appmcp

import (
	"fmt"
	"net/url"
	"os"
	"strconv"
	"strings"
)

// Config enables the /mcp endpoint. It is off unless explicitly enabled.
type Config struct {
	Enabled bool
	// RatePerMinute bounds tool requests per client IP on each replica.
	RatePerMinute int
	// MaxInFlight bounds concurrent requests on each replica, below the
	// database pool, so agent bursts cannot take connections web users need.
	MaxInFlight int
	// AllowedOrigins are the browser origins (scheme://host[:port]) accepted
	// besides loopback; requests without an Origin header are unaffected.
	AllowedOrigins []string
	// OAuth serves an authorization server so agents can sign in through the
	// browser instead of pasting an API token. It needs accounts.
	OAuth bool
	// RequireSignIn answers every /mcp request without a valid token with a
	// sign-in challenge, so all agent traffic is attributed to an account and
	// limited per account. It needs accounts.
	RequireSignIn bool

	// Whether MCP_OAUTH_ENABLED and MCP_REQUIRE_SIGN_IN were set; Resolve
	// applies the defaults to the rest.
	oauthSet, requireSet bool
}

// Resolve applies the defaults that depend on accounts: with accounts on,
// OAuth sign-in and required sign-in default to on; without accounts nobody
// can sign in, so they default to off and explicitly enabling them is an error.
func (c Config) Resolve(accounts bool) (Config, error) {
	if !c.Enabled {
		return c, nil
	}
	if !c.oauthSet {
		c.OAuth = accounts
	}
	if !c.requireSet {
		c.RequireSignIn = accounts
	}
	if !accounts && c.OAuth {
		return c, fmt.Errorf("MCP_OAUTH_ENABLED requires accounts (AUTH_ENABLED=true)")
	}
	if !accounts && c.RequireSignIn {
		return c, fmt.Errorf("MCP_REQUIRE_SIGN_IN requires accounts (AUTH_ENABLED=true)")
	}
	return c, nil
}

const (
	DefaultRatePerMinute = 60
	DefaultMaxInFlight   = 8
)

// ConfigFromEnv reads MCP_* settings. MCP_OAUTH_ENABLED and MCP_REQUIRE_SIGN_IN
// default by Resolve, on when accounts are. MCP_ALLOWED_ORIGINS (comma-separated)
// defaults to AUTH_BASE_URL, read directly so it applies without accounts and
// trimmed of trailing slashes as accounts do. Nothing else is read or
// validated while MCP is disabled, so it cannot affect startup.
func ConfigFromEnv() (Config, error) {
	c := Config{RatePerMinute: DefaultRatePerMinute, MaxInFlight: DefaultMaxInFlight}
	if raw := os.Getenv("MCP_ENABLED"); raw != "" {
		v, err := strconv.ParseBool(raw)
		if err != nil {
			return c, fmt.Errorf("MCP_ENABLED must be true or false")
		}
		c.Enabled = v
	}
	if !c.Enabled {
		return c, nil
	}
	for _, setting := range []struct {
		name        string
		target, set *bool
	}{{"MCP_OAUTH_ENABLED", &c.OAuth, &c.oauthSet}, {"MCP_REQUIRE_SIGN_IN", &c.RequireSignIn, &c.requireSet}} {
		if raw := os.Getenv(setting.name); raw != "" {
			v, err := strconv.ParseBool(raw)
			if err != nil {
				return c, fmt.Errorf("%s must be true or false", setting.name)
			}
			*setting.target, *setting.set = v, true
		}
	}
	for _, setting := range []struct {
		name   string
		target *int
		max    int
	}{{"MCP_RATE_PER_MINUTE", &c.RatePerMinute, 6000}, {"MCP_MAX_IN_FLIGHT", &c.MaxInFlight, 256}} {
		if raw := os.Getenv(setting.name); raw != "" {
			v, err := strconv.Atoi(raw)
			if err != nil || v < 1 || v > setting.max {
				return c, fmt.Errorf("%s must be between 1 and %d", setting.name, setting.max)
			}
			*setting.target = v
		}
	}
	raw, name := os.Getenv("MCP_ALLOWED_ORIGINS"), "MCP_ALLOWED_ORIGINS"
	if raw == "" {
		raw, name = strings.TrimRight(strings.TrimSpace(os.Getenv("AUTH_BASE_URL")), "/"), "AUTH_BASE_URL"
	}
	for _, value := range strings.Split(raw, ",") {
		if value = strings.TrimSpace(value); value == "" {
			continue
		}
		origin, ok := Origin(value)
		if !ok {
			return c, fmt.Errorf("%s must contain http(s) origins such as https://ticketopia.example", name)
		}
		c.AllowedOrigins = append(c.AllowedOrigins, origin)
	}
	return c, nil
}

// Origin normalizes an http(s) origin to scheme://host[:port], rejecting
// paths, credentials and opaque values such as "null".
func Origin(raw string) (string, bool) {
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" || u.User != nil || (u.Scheme != "http" && u.Scheme != "https") ||
		(u.Path != "" && u.Path != "/") || u.RawQuery != "" || u.Fragment != "" {
		return "", false
	}
	return strings.ToLower(u.Scheme + "://" + u.Host), true
}

package accounts

import (
	"fmt"
	"net/url"
	"os"
	"strconv"
	"strings"
)

type Config struct {
	Enabled                         bool
	BaseURL, ClientID, ClientSecret string
}

func ConfigFromEnv() (Config, error) {
	c := Config{}
	if raw := os.Getenv("AUTH_ENABLED"); raw != "" {
		var err error
		c.Enabled, err = strconv.ParseBool(raw)
		if err != nil {
			return c, fmt.Errorf("AUTH_ENABLED must be true or false")
		}
	}
	if !c.Enabled {
		return c, nil
	}
	c.BaseURL = strings.TrimRight(strings.TrimSpace(os.Getenv("AUTH_BASE_URL")), "/")
	c.ClientID, c.ClientSecret = os.Getenv("GOOGLE_CLIENT_ID"), os.Getenv("GOOGLE_CLIENT_SECRET")
	if err := c.Validate(); err != nil {
		return Config{}, err
	}
	return c, nil
}
func (c Config) Validate() error {
	if !c.Enabled {
		return nil
	}
	u, err := url.Parse(c.BaseURL)
	if err != nil || u.Host == "" || u.User != nil || u.Path != "" || u.RawPath != "" || u.RawQuery != "" || u.Fragment != "" || u.Opaque != "" || u.ForceQuery {
		return fmt.Errorf("AUTH_BASE_URL must be an absolute origin without path, query, or credentials")
	}
	local := u.Hostname() == "localhost" || u.Hostname() == "127.0.0.1" || u.Hostname() == "::1"
	if u.Scheme != "https" && !(u.Scheme == "http" && local) {
		return fmt.Errorf("AUTH_BASE_URL requires HTTPS except on localhost")
	}
	if strings.TrimSpace(c.ClientID) == "" || strings.TrimSpace(c.ClientSecret) == "" {
		return fmt.Errorf("GOOGLE_CLIENT_ID and GOOGLE_CLIENT_SECRET are required when auth is enabled")
	}
	return nil
}
func (c Config) Secure() bool { return strings.HasPrefix(c.BaseURL, "https://") }
func (c Config) CookieName(kind string) string {
	if c.Secure() {
		return "__Host-ticketopia_" + kind
	}
	return "ticketopia_" + kind
}

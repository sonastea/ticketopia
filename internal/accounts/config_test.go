package accounts

import (
	"strings"
	"testing"
)

func TestAuthConfiguration(t *testing.T) {
	for _, origin := range []string{"https://events.example", "http://localhost:8080", "http://127.0.0.1:8080", "http://[::1]:8080"} {
		c := Config{true, origin, "client", "private-secret"}
		if err := c.Validate(); err != nil {
			t.Errorf("%s: %v", origin, err)
		}
		if c.Secure() && !strings.HasPrefix(c.CookieName("session"), "__Host-") {
			t.Fatal("production cookie is not host-only")
		}
	}
	for _, origin := range []string{"http://events.example", "//events.example", "https://user:password@events.example", "https://events.example/path", "https://events.example?query=x", "https://events.example/#hash", ""} {
		c := Config{true, origin, "client", "private-secret"}
		if err := c.Validate(); err == nil || strings.Contains(err.Error(), c.ClientSecret) {
			t.Errorf("bad configuration/redaction: %s %v", origin, err)
		}
	}
	t.Setenv("AUTH_ENABLED", "")
	c, err := ConfigFromEnv()
	if err != nil || c.Enabled {
		t.Fatal("auth must be disabled by default")
	}
	t.Setenv("AUTH_ENABLED", "perhaps")
	if _, err := ConfigFromEnv(); err == nil {
		t.Fatal("invalid flag accepted")
	}
	t.Setenv("AUTH_ENABLED", "true")
	t.Setenv("AUTH_BASE_URL", "https://events.example")
	t.Setenv("GOOGLE_CLIENT_ID", "")
	t.Setenv("GOOGLE_CLIENT_SECRET", "hidden-secret")
	if _, err := ConfigFromEnv(); err == nil || strings.Contains(err.Error(), "hidden-secret") {
		t.Fatal("missing credentials not rejected/redacted")
	}
}

package reviewfixture

import (
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"testing"

	"github.com/sonastea/ticketopia/internal/persistence/persistencetest"
)

func TestStartRejectsBeforeDatabaseWork(t *testing.T) {
	for name, o := range map[string]Options{
		"unknown scenario":  {Scenario: "nope", Addr: "127.0.0.1:0", StateDir: t.TempDir(), Database: persistencetest.Options{Addr: "127.0.0.1:3307"}},
		"public listener":   {Scenario: "baseline", Addr: "0.0.0.0:0", StateDir: t.TempDir(), Database: persistencetest.Options{Addr: "127.0.0.1:3307"}},
		"remote database":   {Scenario: "baseline", Addr: "127.0.0.1:0", StateDir: t.TempDir(), Database: persistencetest.Options{Addr: "10.0.0.5:3306"}},
		"hostname database": {Scenario: "baseline", Addr: "127.0.0.1:0", StateDir: t.TempDir(), Database: persistencetest.Options{Addr: "db.example:3306"}},
		"remote SES":        {Scenario: "baseline", Addr: "127.0.0.1:0", StateDir: t.TempDir(), Database: persistencetest.Options{Addr: "127.0.0.1:3307"}, SESEndpoint: "https://email.us-east-1.amazonaws.com"},
		"SES fragment":      {Scenario: "baseline", Addr: "127.0.0.1:0", StateDir: t.TempDir(), Database: persistencetest.Options{Addr: "127.0.0.1:3307"}, SESEndpoint: "http://localhost:4566#x"},
		"SES empty query":   {Scenario: "baseline", Addr: "127.0.0.1:0", StateDir: t.TempDir(), Database: persistencetest.Options{Addr: "127.0.0.1:3307"}, SESEndpoint: "http://localhost:4566?"},
	} {
		if s, err := Start(t.Context(), o); err == nil {
			s.Close()
			t.Errorf("%s: accepted", name)
		}
	}
}

// Every scenario seeds, serves its manifest paths to each account and to a
// signed-out browser, and removes its state files on Close.
func TestMariaDBScenarios(t *testing.T) {
	database, ok := persistencetest.OptionsFromEnv(filepath.Join("..", "..", ".local", "mariadb", "certs", "ca.crt"))
	if !ok {
		t.Skip("set MARIADB_TEST_ADDR to run real MariaDB tests")
	}
	for _, name := range ScenarioNames() {
		t.Run(name, func(t *testing.T) {
			ses := os.Getenv("SES_TEST_ENDPOINT")
			if name == "reminder-email" && ses == "" {
				t.Skip("set SES_TEST_ENDPOINT after make ses-setup to send through MiniStack")
			}
			state := t.TempDir()
			s, err := Start(t.Context(), Options{Scenario: name, Addr: "127.0.0.1:0", StateDir: state, Database: database, SESEndpoint: ses})
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { s.Close() })
			m := s.Manifest()
			if name == "discover-last-known" {
				// Before other page visits: only the removed event's row is
				// last-known, and the list itself is fresh.
				body := get(t, m, "owner", m.Paths["discover"])
				if strings.Count(body, `class="event-last-known"`) != 1 || strings.Contains(body, "Showing last-known results") {
					t.Error("stored Discover must label only the removed row and show no last-known banner")
				}
				// The app MCP tool returns the same stored rows, with per-event meta.
				result := mcpCall(t, m.BaseURL, "find_events", map[string]any{"city": "Chicago", "country": "US"})
				var list struct {
					Items []struct {
						Name string
						Meta *struct{ Stale bool }
					}
					Meta struct{ Stale bool }
				}
				if err := json.Unmarshal(result, &list); err != nil || list.Meta.Stale || len(list.Items) != 7 {
					t.Fatalf("find_events: %v %s", err, result)
				}
				for _, item := range list.Items {
					if (item.Meta != nil && item.Meta.Stale) != (item.Name == removedUpstream) {
						t.Errorf("find_events item %q meta %+v", item.Name, item.Meta)
					}
				}
				// Opening the removed event (provider 404) keeps it last-known.
				label := regexp.MustCompile(`Last-known as of [^<]+`).FindString(body)
				get(t, m, "owner", m.ScenarioPaths["removed_event"])
				if after := regexp.MustCompile(`Last-known as of [^<]+`).FindString(get(t, m, "owner", m.Paths["discover"])); label == "" || after != label {
					t.Errorf("removed row label changed after opening the event: %q -> %q", label, after)
				}
			}
			paths := map[string]string{"discover": m.Paths["discover"], "event": m.Paths["event"], "discussion": m.Paths["discussion"]}
			for key, path := range m.ScenarioPaths {
				paths[key] = path
			}
			for _, who := range []string{"owner", "other", "moderator", ""} {
				var cookie *http.Cookie
				if who != "" {
					var state struct {
						Cookies []struct{ Name, Value string }
					}
					data, err := os.ReadFile(m.Accounts[who].StorageState)
					if err != nil || json.Unmarshal(data, &state) != nil || len(state.Cookies) != 1 {
						t.Fatalf("%s storage state: %v", who, err)
					}
					cookie = &http.Cookie{Name: state.Cookies[0].Name, Value: state.Cookies[0].Value}
				}
				for key, path := range paths {
					want := http.StatusOK
					switch {
					case key == "unavailable_event":
						want = http.StatusServiceUnavailable
					case who == "" && (key == "reminders" || key == "paused_reminders_other"):
						want = http.StatusSeeOther
					}
					request, _ := http.NewRequestWithContext(t.Context(), http.MethodGet, m.BaseURL+path, nil)
					if cookie != nil {
						request.AddCookie(cookie)
					}
					response, err := (&http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}).Do(request)
					if err != nil {
						t.Fatal(err)
					}
					response.Body.Close()
					if response.StatusCode != want {
						t.Errorf("%q %s: status %d, want %d", who, key, response.StatusCode, want)
					}
				}
			}
			if name == "reminder-email" {
				messages, err := Messages(t.Context(), ses, m.Run)
				if err != nil {
					t.Fatal(err)
				}
				if len(messages) != 1 || !strings.Contains(messages[0].Subject, reminderEmail) || messages[0].To[0] != m.Accounts["owner"].Email {
					t.Fatalf("captured messages for this run: %+v", messages)
				}
				if body := get(t, m, "owner", "/me/reminders"); !strings.Contains(body, "Accepted by SES") {
					t.Error("owner reminder is not shown as SES accepted")
				}
			}
			if name == "reminder-states" {
				// Every state the worker can reach is visible to the owner.
				body := get(t, m, "owner", "/me/reminders")
				for _, detail := range []string{"Waiting for a known public-sale time", "Target configured", "Fresh event details are missing",
					"An email attempt is in progress", "SES rejected the attempt", "Accepted by SES", "The outcome of the send is unknown",
					"The email could not be sent", "The reminder window has passed", "The reminder is stopped or the event is cancelled"} {
					if !strings.Contains(body, detail) {
						t.Errorf("owner reminders missing %q", detail)
					}
				}
				if body := get(t, m, "other", "/me/reminders"); !strings.Contains(body, "Withheld by email permission") {
					t.Error("other's reminder is not paused")
				}
			}
			if err := s.Close(); err != nil {
				t.Fatal(err)
			}
			if entries, _ := os.ReadDir(state); len(entries) != 0 {
				t.Fatalf("state files left behind: %d", len(entries))
			}
		})
	}
}

// get fetches path as a manifest account and returns the response body.
func get(t *testing.T, m Manifest, who, path string) string {
	t.Helper()
	var state struct {
		Cookies []struct{ Name, Value string }
	}
	data, err := os.ReadFile(m.Accounts[who].StorageState)
	if err != nil || json.Unmarshal(data, &state) != nil || len(state.Cookies) != 1 {
		t.Fatalf("%s storage state: %v", who, err)
	}
	request, _ := http.NewRequestWithContext(t.Context(), http.MethodGet, m.BaseURL+path, nil)
	request.AddCookie(&http.Cookie{Name: state.Cookies[0].Name, Value: state.Cookies[0].Value})
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	body, err := io.ReadAll(response.Body)
	if err != nil || response.StatusCode != http.StatusOK {
		t.Fatalf("%s %s: status %d %v", who, path, response.StatusCode, err)
	}
	return string(body)
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

// Fixtures stay deterministic when the environment enables external
// providers: every non-loopback request through the default transport (used
// by the Ticketmaster and IP lookup clients) is recorded and refused.
func TestMariaDBFixturesMakeNoExternalRequests(t *testing.T) {
	database, ok := persistencetest.OptionsFromEnv(filepath.Join("..", "..", ".local", "mariadb", "certs", "ca.crt"))
	if !ok {
		t.Skip("set MARIADB_TEST_ADDR to run real MariaDB tests")
	}
	t.Setenv("TICKETMASTER_KEY", "fixture-key-that-must-not-be-used")
	t.Setenv("IP_GEOLOCATION_ENABLED", "true")
	// Trust the loopback test client as a proxy, so a forwarded public address
	// reaches IP lookup on the signed-out default Discover page.
	t.Setenv("TRUSTED_PROXY_CIDRS", "127.0.0.1/32")
	original := http.DefaultTransport
	t.Cleanup(func() { http.DefaultTransport = original })
	var mu sync.Mutex
	var external []string
	http.DefaultTransport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if ip := net.ParseIP(r.URL.Hostname()); ip == nil || !ip.IsLoopback() {
			mu.Lock()
			external = append(external, r.URL.Host)
			mu.Unlock()
			return nil, errors.New("external request refused by test")
		}
		return original.RoundTrip(r)
	})
	s, err := Start(t.Context(), Options{Scenario: "event-data-gaps", Addr: "127.0.0.1:0", StateDir: t.TempDir(), Database: database})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	m := s.Manifest()
	// Uncached reads would reach the provider if it were enabled.
	for _, path := range []string{m.ScenarioPaths["unavailable_event"], m.ScenarioPaths["stale_event"], "/?city=Denver&country=US"} {
		response, err := http.Get(m.BaseURL + path)
		if err != nil {
			t.Fatal(err)
		}
		response.Body.Close()
	}
	request, _ := http.NewRequestWithContext(t.Context(), http.MethodGet, m.BaseURL+"/", nil)
	request.Header.Set("X-Forwarded-For", "8.8.8.8")
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	mu.Lock()
	defer mu.Unlock()
	if len(external) != 0 {
		t.Fatalf("external requests: %v", external)
	}
}

// mcpCall posts one stateless tools/call to the review server's /mcp and
// returns the tool's structured content.
func mcpCall(t *testing.T, base, tool string, args map[string]any) json.RawMessage {
	t.Helper()
	body, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": 1, "method": "tools/call", "params": map[string]any{"name": tool, "arguments": args}})
	request, _ := http.NewRequestWithContext(t.Context(), http.MethodPost, base+"/mcp", strings.NewReader(string(body)))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Accept", "application/json, text/event-stream")
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	var reply struct {
		Result struct {
			IsError           bool            `json:"isError"`
			StructuredContent json.RawMessage `json:"structuredContent"`
		}
	}
	if err := json.NewDecoder(response.Body).Decode(&reply); err != nil || response.StatusCode != http.StatusOK || reply.Result.IsError {
		t.Fatalf("%s: status %d %v %+v", tool, response.StatusCode, err, reply)
	}
	return reply.Result.StructuredContent
}

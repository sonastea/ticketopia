package reviewfixture

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"testing"

	"github.com/sonastea/ticketopia/internal/persistence/persistencetest"
)

func TestStartRejectsBeforeDatabaseWork(t *testing.T) {
	for name, o := range map[string]Options{
		"unknown scenario":  {Scenario: "nope", Addr: "127.0.0.1:0", StateDir: t.TempDir(), Database: persistencetest.Options{Addr: "127.0.0.1:3307"}},
		"public listener":   {Scenario: "baseline", Addr: "0.0.0.0:0", StateDir: t.TempDir(), Database: persistencetest.Options{Addr: "127.0.0.1:3307"}},
		"remote database":   {Scenario: "baseline", Addr: "127.0.0.1:0", StateDir: t.TempDir(), Database: persistencetest.Options{Addr: "10.0.0.5:3306"}},
		"hostname database": {Scenario: "baseline", Addr: "127.0.0.1:0", StateDir: t.TempDir(), Database: persistencetest.Options{Addr: "db.example:3306"}},
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
			state := t.TempDir()
			s, err := Start(t.Context(), Options{Scenario: name, Addr: "127.0.0.1:0", StateDir: state, Database: database})
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { s.Close() })
			m := s.Manifest()
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
			if err := s.Close(); err != nil {
				t.Fatal(err)
			}
			if entries, _ := os.ReadDir(state); len(entries) != 0 {
				t.Fatalf("state files left behind: %d", len(entries))
			}
		})
	}
}

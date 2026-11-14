package reviewfixture

import (
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"slices"
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
				// Real stored details, with collection history, pass output validation.
				var detail struct {
					Item    struct{ Name string }
					History *struct {
						FirstSeen string `json:"first_seen"`
					}
				}
				if err := json.Unmarshal(mcpCall(t, m.BaseURL, "event_details", map[string]any{"event_id": "ticketmaster:Review_0"}), &detail); err != nil ||
					detail.Item.Name == "" || detail.History == nil {
					t.Fatalf("event_details: %v %+v", err, detail)
				}
				// Opening the removed event (provider 404) keeps it last-known.
				label := regexp.MustCompile(`Last-known as of [^<]+`).FindString(body)
				get(t, m, "owner", m.ScenarioPaths["removed_event"])
				if after := regexp.MustCompile(`Last-known as of [^<]+`).FindString(get(t, m, "owner", m.Paths["discover"])); label == "" || after != label {
					t.Errorf("removed row label changed after opening the event: %q -> %q", label, after)
				}
			}
			if name == "populated" {
				checkMembers(t, m)
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
		Error json.RawMessage
	}
	if err := json.NewDecoder(response.Body).Decode(&reply); err != nil || response.StatusCode != http.StatusOK || reply.Result.IsError ||
		reply.Error != nil || len(reply.Result.StructuredContent) == 0 {
		t.Fatalf("%s: status %d %v %+v", tool, response.StatusCode, err, reply)
	}
	return reply.Result.StructuredContent
}

type mcpReply struct {
	Status  int
	IsError bool
	Text    string
	Content json.RawMessage
}

// mcpTool posts one stateless tools/call with an optional bearer token and
// optional extra headers.
func mcpTool(t *testing.T, base, token, tool string, args map[string]any, headers ...string) mcpReply {
	t.Helper()
	body, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": 1, "method": "tools/call", "params": map[string]any{"name": tool, "arguments": args}})
	request, _ := http.NewRequestWithContext(t.Context(), http.MethodPost, base+"/mcp", strings.NewReader(string(body)))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Accept", "application/json, text/event-stream")
	if token != "" {
		request.Header.Set("Authorization", "Bearer "+token)
	}
	for i := 0; i+1 < len(headers); i += 2 {
		request.Header.Set(headers[i], headers[i+1])
	}
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	var reply struct {
		Result struct {
			IsError           bool            `json:"isError"`
			StructuredContent json.RawMessage `json:"structuredContent"`
			Content           []struct{ Text string }
		}
	}
	_ = json.NewDecoder(response.Body).Decode(&reply)
	r := mcpReply{Status: response.StatusCode, IsError: reply.Result.IsError, Content: reply.Result.StructuredContent}
	if len(reply.Result.Content) > 0 {
		r.Text = reply.Result.Content[0].Text
	}
	return r
}

func savedIDs(t *testing.T, r mcpReply) []string {
	t.Helper()
	var list struct {
		Items []struct{ Event struct{ ID string } }
	}
	if r.Status != http.StatusOK || r.IsError || json.Unmarshal(r.Content, &list) != nil {
		t.Fatalf("saved list: %+v", r)
	}
	ids := []string{}
	for _, item := range list.Items {
		ids = append(ids, item.Event.ID)
	}
	return ids
}

// Signed-in tools act only on the token's account: each account sees and
// changes only its own saves and reminders, cookies are never accepted, and
// invalid tokens are refused.
func TestMariaDBPrivateToolsArePerAccount(t *testing.T) {
	database, ok := persistencetest.OptionsFromEnv(filepath.Join("..", "..", ".local", "mariadb", "certs", "ca.crt"))
	if !ok {
		t.Skip("set MARIADB_TEST_ADDR to run real MariaDB tests")
	}
	s, err := Start(t.Context(), Options{Scenario: "baseline", Addr: "127.0.0.1:0", StateDir: t.TempDir(), Database: database})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	m := s.Manifest()
	base, owner, other := m.BaseURL, m.Accounts["owner"].APIToken, m.Accounts["other"].APIToken
	unsaved, alsoUnsaved := m.EventIDs[4], m.EventIDs[5]

	if ids := savedIDs(t, mcpTool(t, base, owner, "my_saved_events", map[string]any{})); len(ids) != 4 {
		t.Fatalf("owner saves: %v", ids)
	}
	if ids := savedIDs(t, mcpTool(t, base, other, "my_saved_events", map[string]any{})); len(ids) != 0 {
		t.Fatalf("other sees saves: %v", ids)
	}
	if r := mcpTool(t, base, other, "save_event", map[string]any{"event_id": unsaved}); r.IsError || !strings.Contains(string(r.Content), `"created":true`) {
		t.Fatalf("other save: %+v", r)
	}
	if r := mcpTool(t, base, other, "save_event", map[string]any{"event_id": unsaved}); r.IsError || !strings.Contains(string(r.Content), `"created":false`) {
		t.Fatalf("repeat save is not idempotent: %+v", r)
	}
	if ids := savedIDs(t, mcpTool(t, base, owner, "my_saved_events", map[string]any{})); len(ids) != 4 || slices.Contains(ids, unsaved) {
		t.Fatalf("other's save reached owner: %v", ids)
	}

	// Reminders follow the account API's rules: the event must be saved first.
	// It says to save first, not that the event is missing.
	if r := mcpTool(t, base, other, "set_reminder", map[string]any{"event_id": alsoUnsaved, "lead_minutes": 60}); !r.IsError || !strings.Contains(r.Text, "Save this event before setting a reminder") {
		t.Fatalf("reminder on an unsaved event: %+v", r)
	}
	if r := mcpTool(t, base, other, "set_reminder", map[string]any{"event_id": unsaved, "lead_minutes": 60}); r.IsError || !strings.Contains(string(r.Content), `"lead_minutes":60`) {
		t.Fatalf("set reminder: %+v", r)
	}
	reminders := func(token string) string {
		return string(mcpTool(t, base, token, "my_reminders", map[string]any{}).Content)
	}
	if !strings.Contains(reminders(other), unsaved) || strings.Contains(reminders(owner), unsaved) {
		t.Fatal("reminder not limited to its account")
	}
	if r := mcpTool(t, base, other, "stop_reminder", map[string]any{"event_id": unsaved}); r.IsError || strings.Contains(reminders(other), unsaved) {
		t.Fatalf("stop reminder: %+v", r)
	}
	if r := mcpTool(t, base, other, "unsave_event", map[string]any{"event_id": unsaved}); r.IsError || len(savedIDs(t, mcpTool(t, base, other, "my_saved_events", map[string]any{}))) != 0 {
		t.Fatalf("unsave: %+v", r)
	}

	// The scheme is case-insensitive; an explicitly empty cursor is invalid, as
	// in the account API, rather than a silent first page.
	if r := mcpTool(t, base, "", "my_saved_events", map[string]any{}, "Authorization", "bearer "+owner); r.Status != http.StatusOK || r.IsError {
		t.Fatalf("lowercase bearer: %+v", r)
	}
	for _, name := range []string{"my_saved_events", "my_reminders"} {
		if r := mcpTool(t, base, owner, name, map[string]any{"cursor": ""}); !r.IsError || !strings.Contains(r.Text, "cursor") {
			t.Fatalf("%s empty cursor: %+v", name, r)
		}
	}

	// A browser session cookie is not a credential for /mcp.
	var state struct {
		Cookies []struct{ Name, Value string }
	}
	data, _ := os.ReadFile(m.Accounts["owner"].StorageState)
	_ = json.Unmarshal(data, &state)
	cookie := state.Cookies[0].Name + "=" + state.Cookies[0].Value
	// Review servers enable OAuth, so the cookie-only call is challenged to sign in.
	if r := mcpTool(t, base, "", "my_saved_events", map[string]any{}, "Cookie", cookie); r.Status != http.StatusUnauthorized || r.IsError || r.Content != nil {
		t.Fatalf("cookie accepted: %+v", r)
	}
	if r := mcpTool(t, base, "not-a-valid-token-but-long-enough-to-check-0123456789", "my_saved_events", map[string]any{}); r.Status != http.StatusUnauthorized {
		t.Fatalf("invalid token: %+v", r)
	}
}

// The radar, follow and Interested tools act through the real services on
// the token's account only: an agent finds a venue, follows it, sees its
// events on the radar, and marks Interested only privately; find_events
// defaults to the account's saved city. It uses a
// stored-catalog scenario, whose stand-in provider answers venue searches.
func TestMariaDBPersonalToolsArePerAccount(t *testing.T) {
	database, ok := persistencetest.OptionsFromEnv(filepath.Join("..", "..", ".local", "mariadb", "certs", "ca.crt"))
	if !ok {
		t.Skip("set MARIADB_TEST_ADDR to run real MariaDB tests")
	}
	s, err := Start(t.Context(), Options{Scenario: "discover-last-known", Addr: "127.0.0.1:0", StateDir: t.TempDir(), Database: database})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	m := s.Manifest()
	base, owner, other := m.BaseURL, m.Accounts["owner"].APIToken, m.Accounts["other"].APIToken
	decode := func(r mcpReply, into any) {
		t.Helper()
		if r.Status != http.StatusOK || r.IsError || json.Unmarshal(r.Content, into) != nil {
			t.Fatalf("tool reply: %+v", r)
		}
	}

	// The owner's account remembers Chicago; an explicit location wins.
	var search struct {
		Items           []struct{ ID string }
		DefaultLocation *struct{ City, Source string } `json:"default_location"`
	}
	decode(mcpTool(t, base, owner, "find_events", map[string]any{}), &search)
	if search.DefaultLocation == nil || search.DefaultLocation.City != "Chicago" || len(search.Items) == 0 {
		t.Fatalf("owner default search: %+v", search)
	}
	search.DefaultLocation = nil
	decode(mcpTool(t, base, owner, "find_events", map[string]any{"city": "Chicago", "country": "US"}), &search)
	if search.DefaultLocation != nil {
		t.Fatalf("explicit city reported a default: %+v", search.DefaultLocation)
	}

	// Find a venue, follow it, and see its events on the radar.
	var venues struct{ Items []struct{ ID, Name string } }
	decode(mcpTool(t, base, owner, "find_venues", map[string]any{"keyword": "riverside", "city": "Chicago"}), &venues)
	if len(venues.Items) != 1 || venues.Items[0].Name != "The Riverside Hall" {
		t.Fatalf("venue search: %+v", venues)
	}
	venue := venues.Items[0].ID
	var artists struct{ Items []any }
	decode(mcpTool(t, base, owner, "find_artists", map[string]any{"keyword": "anyone"}), &artists)
	if len(artists.Items) != 0 {
		t.Fatalf("fixture artists: %+v", artists)
	}
	follow := map[string]any{"kind": "venue", "target_id": venue}
	for _, created := range []string{`"created":true`, `"created":false`} {
		if r := mcpTool(t, base, owner, "follow_artist_or_venue", follow); r.IsError || !strings.Contains(string(r.Content), created) {
			t.Fatalf("follow (%s): %+v", created, r)
		}
	}
	followed := func(token string) string {
		return string(mcpTool(t, base, token, "my_follows", map[string]any{}).Content)
	}
	if !strings.Contains(followed(owner), venue) || strings.Contains(followed(other), venue) {
		t.Fatal("follow not limited to its account")
	}
	var picks struct {
		Mode  string
		Items []struct {
			Event   struct{ ID string }
			Reasons []struct {
				Code     string
				TargetID string `json:"target_id"`
			}
		}
	}
	decode(mcpTool(t, base, owner, "my_radar", map[string]any{}), &picks)
	matched := false
	for _, item := range picks.Items {
		for _, reason := range item.Reasons {
			matched = matched || (reason.Code == "followed_venue" && reason.TargetID == venue)
		}
	}
	if picks.Mode != "personalized" || !matched {
		t.Fatalf("radar does not show the followed venue: %+v", picks)
	}
	if r := mcpTool(t, base, owner, "unfollow_artist_or_venue", follow); r.IsError || strings.Contains(followed(owner), venue) {
		t.Fatalf("unfollow: %+v", r)
	}

	// Interested: each account sees only its own choices, and agents never
	// make one public.
	interested := func(token string) []string {
		var list struct {
			Items []struct{ Event struct{ ID string } }
		}
		decode(mcpTool(t, base, token, "my_interested_events", map[string]any{}), &list)
		ids := []string{}
		for _, item := range list.Items {
			ids = append(ids, item.Event.ID)
		}
		return ids
	}
	if len(interested(owner)) != 2 || len(interested(other)) != 1 {
		t.Fatalf("seeded interest: owner %v other %v", interested(owner), interested(other))
	}
	event := m.EventIDs[3]
	var mark struct {
		Created bool
		Item    struct{ Visibility string }
	}
	decode(mcpTool(t, base, other, "mark_interested", map[string]any{"event_id": event}), &mark)
	if !mark.Created || mark.Item.Visibility != "private" {
		t.Fatalf("new choice is not private: %+v", mark)
	}
	// The owner's own public choice stays public when an agent marks it again.
	decode(mcpTool(t, base, owner, "mark_interested", map[string]any{"event_id": m.EventIDs[0]}), &mark)
	if mark.Created || mark.Item.Visibility != "public" {
		t.Fatalf("existing public choice: %+v", mark)
	}
	if !slices.Contains(interested(other), event) || slices.Contains(interested(owner), event) {
		t.Fatal("Interested not limited to its account")
	}
	if r := mcpTool(t, base, other, "unmark_interested", map[string]any{"event_id": event}); r.IsError || slices.Contains(interested(other), event) {
		t.Fatalf("unmark: %+v", r)
	}
}

// Each member's session signs in as that member alone, and can act
// through the browser's form posts, as make simulate-users does.
func checkMembers(t *testing.T, m Manifest) {
	t.Helper()
	if len(m.Members) != memberCount || m.SessionCookie == "" {
		t.Fatalf("populated: %d members, cookie %q", len(m.Members), m.SessionCookie)
	}
	csrf := ""
	for _, member := range m.Members {
		request, _ := http.NewRequestWithContext(t.Context(), http.MethodGet, m.BaseURL+"/api/v1/me", nil)
		request.AddCookie(&http.Cookie{Name: m.SessionCookie, Value: member.Session})
		response, err := http.DefaultClient.Do(request)
		if err != nil {
			t.Fatal(err)
		}
		var me struct {
			Item struct{ ID string }
			CSRF string `json:"csrf_token"`
		}
		err = json.NewDecoder(response.Body).Decode(&me)
		response.Body.Close()
		if err != nil || me.Item.ID != member.ID || me.CSRF == "" {
			t.Fatalf("%s session: %v %+v", member.DisplayName, err, me.Item)
		}
		if csrf == "" {
			csrf = me.CSRF
		}
	}
	form := url.Values{"action": {"save"}, "csrf_token": {csrf}}
	request, _ := http.NewRequestWithContext(t.Context(), http.MethodPost, m.BaseURL+"/saved/"+url.PathEscape(m.EventIDs[0]), strings.NewReader(form.Encode()))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	request.Header.Set("Accept", "application/json")
	request.Header.Set("Origin", m.BaseURL)
	request.AddCookie(&http.Cookie{Name: m.SessionCookie, Value: m.Members[0].Session})
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("member save form: %d", response.StatusCode)
	}
}

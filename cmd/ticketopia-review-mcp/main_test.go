package main

import (
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// connect runs the command over in-memory transports. Closing the returned
// session is the stdin EOF a real client produces when it exits.
func connect(t *testing.T, args ...string) (*mcp.ClientSession, <-chan error) {
	t.Helper()
	serverTransport, clientTransport := mcp.NewInMemoryTransports()
	done := make(chan error, 1)
	go func() { done <- run(args, serverTransport) }()
	client := mcp.NewClient(&mcp.Implementation{Name: "test", Version: "0"}, nil)
	session, err := client.Connect(t.Context(), clientTransport, nil)
	if err != nil {
		t.Fatal(err)
	}
	// Disconnecting stops the review server even when an assertion fails first.
	t.Cleanup(func() { session.Close() })
	return session, done
}

func call[T any](t *testing.T, s *mcp.ClientSession, name string, args any) (T, *mcp.CallToolResult) {
	t.Helper()
	var out T
	result, err := s.CallTool(t.Context(), &mcp.CallToolParams{Name: name, Arguments: args})
	if err != nil {
		t.Fatal(err)
	}
	if !result.IsError {
		data, _ := json.Marshal(result.StructuredContent)
		if err := json.Unmarshal(data, &out); err != nil {
			t.Fatal(err)
		}
	}
	return out, result
}

func TestScenarioToolsWithoutDatabase(t *testing.T) {
	session, done := connect(t, "-state-dir", t.TempDir())
	list, _ := call[listOutput](t, session, "scenario_list", map[string]any{})
	if len(list.Scenarios) < 5 || list.Scenarios[0].Name != "baseline" {
		t.Fatalf("scenarios: %+v", list.Scenarios)
	}
	if m, _ := call[manifestOutput](t, session, "manifest", map[string]any{}); m.Running {
		t.Fatal("manifest reported a server before scenario_apply")
	}
	if _, result := call[messagesOutput](t, session, "ses_messages", map[string]any{}); !result.IsError {
		t.Fatal("ses_messages answered without a running server")
	}
	// Unknown names are rejected before any database work.
	if _, result := call[manifestOutput](t, session, "scenario_apply", map[string]any{"name": "nope"}); !result.IsError {
		t.Fatal("unknown scenario accepted")
	}
	session.Close()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}

func TestScenarioApplyResetAndDisconnectCleanup(t *testing.T) {
	if os.Getenv("MARIADB_TEST_ADDR") == "" {
		t.Skip("set MARIADB_TEST_ADDR to run the review MCP lifecycle against MariaDB")
	}
	state := t.TempDir()
	session, done := connect(t, "-addr", "127.0.0.1:0", "-state-dir", state,
		"-db-addr", os.Getenv("MARIADB_TEST_ADDR"), "-db-ca", filepath.Join("..", "..", ".local", "mariadb", "certs", "ca.crt"))
	first, result := call[manifestOutput](t, session, "scenario_apply", map[string]any{"name": "event-edge-cases"})
	if result.IsError || !first.Running || first.Manifest.ScenarioPaths["busy_discussion"] == "" {
		t.Fatalf("apply: %+v %+v", first, result.Content)
	}
	get := func(base, path string) int {
		t.Helper()
		response, err := http.Get(base + path)
		if err != nil {
			return 0
		}
		response.Body.Close()
		return response.StatusCode
	}
	if code := get(first.Manifest.BaseURL, first.Manifest.ScenarioPaths["edge_event"]); code != http.StatusOK {
		t.Fatalf("edge event: %d", code)
	}
	// Applying again resets: the old server stops and a new one starts.
	second, _ := call[manifestOutput](t, session, "scenario_apply", map[string]any{"name": "baseline"})
	if !second.Running || second.Manifest.Scenario != "baseline" || second.Manifest.DiscussionThreadID == first.Manifest.DiscussionThreadID {
		t.Fatalf("reset: %+v", second)
	}
	if code := get(first.Manifest.BaseURL, "/"); first.Manifest.BaseURL != second.Manifest.BaseURL && code != 0 {
		t.Fatalf("previous server still answering: %d", code)
	}
	current, _ := call[manifestOutput](t, session, "manifest", map[string]any{})
	if current.Manifest == nil || current.Manifest.BaseURL != second.Manifest.BaseURL {
		t.Fatalf("manifest: %+v", current)
	}
	session.Close()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(30 * time.Second):
		t.Fatal("server did not exit after disconnect")
	}
	if _, err := os.Stat(filepath.Join(state, "manifest.json")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("manifest left behind: %v", err)
	}
	if code := get(second.Manifest.BaseURL, "/"); code != 0 {
		t.Fatalf("review server still answering after disconnect: %d", code)
	}
}

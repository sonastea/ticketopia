package persistence_test

import (
	"flag"
	"maps"
	"os"
	"os/signal"
	"path/filepath"
	"slices"
	"syscall"
	"testing"

	"github.com/sonastea/ticketopia/internal/persistence/persistencetest"
	"github.com/sonastea/ticketopia/internal/reviewfixture"
)

var (
	reviewScenario = flag.String("review.scenario", "baseline", "TestMariaDBReviewServer fixture scenario")
	reviewSES      = flag.String("review.ses-endpoint", reviewfixture.DefaultSESEndpoint, "local SES emulator for email scenarios")
)

// Interactive manual-review server (make review-server); see reviewfixture.
// Stop with Ctrl-C so the schema, users and state files are removed.
func TestMariaDBReviewServer(t *testing.T) {
	addr := os.Getenv("REVIEW_SERVER_ADDR")
	if addr == "" {
		t.Skip("set REVIEW_SERVER_ADDR (for example 127.0.0.1:8090) to serve signed-in review fixtures")
	}
	stateDir := os.Getenv("REVIEW_SERVER_STATE_DIR")
	if stateDir == "" {
		stateDir = filepath.Join(os.TempDir(), "ticketopia-review")
	}
	database, _ := persistencetest.OptionsFromEnv(filepath.Join("..", "..", ".local", "mariadb", "certs", "ca.crt"))
	server, err := reviewfixture.Start(t.Context(), reviewfixture.Options{
		Scenario: *reviewScenario, Addr: addr, StateDir: stateDir, Database: database, SESEndpoint: *reviewSES})
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := server.Close(); err != nil {
			t.Error(err)
		}
	}()
	m := server.Manifest()
	t.Logf("review server listening on %s (scenario %s)", m.BaseURL, m.Scenario)
	for _, name := range slices.Sorted(maps.Keys(m.ScenarioPaths)) {
		t.Logf("scenario %s: %s%s", name, m.BaseURL, m.ScenarioPaths[name])
	}
	t.Logf("storage states and manifest: %s", stateDir)
	if m.Scenario == "reminder-email" {
		t.Logf("captured email: %s/_ministack/ses/messages (fixture addresses contain +%s@)", *reviewSES, m.Run)
	}
	t.Logf("signed-in discussion example (owner): %s%s", m.BaseURL, m.Paths["discussion"])
	t.Log("press Ctrl-C to stop and drop the isolated schema")

	stop, cancel := signal.NotifyContext(t.Context(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	select {
	case <-stop.Done():
	case err := <-server.Done():
		t.Fatal(err)
	}
}

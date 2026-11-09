// Command ticketopia-review-mcp exposes the local review fixtures to MCP
// clients over stdio: list scenarios, (re)start the review server on one, and
// read its manifest. Local development only; see docs/testing.md.
//
// Stdout carries the protocol exclusively; diagnostics go to stderr. The
// review server and its disposable schema are removed when stdin closes or on
// SIGINT/SIGTERM. A killed process leaves its ticketopia_test_* schema behind.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"slices"
	"sync"
	"syscall"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/sonastea/ticketopia/internal/persistence/persistencetest"
	"github.com/sonastea/ticketopia/internal/reviewfixture"
)

func main() {
	log.SetOutput(os.Stderr)
	log.SetFlags(0)
	if err := run(os.Args[1:], &mcp.StdioTransport{}); err != nil {
		log.Print(err)
		os.Exit(1)
	}
}

func run(args []string, transport mcp.Transport) error {
	flags := flag.NewFlagSet("ticketopia-review-mcp", flag.ContinueOnError)
	flags.SetOutput(os.Stderr)
	addr := flags.String("addr", "127.0.0.1:8090", "loopback address for the review server")
	stateDir := flags.String("state-dir", filepath.Join(os.TempDir(), "ticketopia-review-mcp"), "directory for manifest.json and storage states")
	dbAddr := flags.String("db-addr", "127.0.0.1:3307", "loopback address of the local MariaDB (make db-setup)")
	dbCA := flags.String("db-ca", filepath.Join(".local", "mariadb", "certs", "ca.crt"), "local MariaDB CA certificate")
	if err := flags.Parse(args); err != nil {
		return err
	}
	ca, err := filepath.Abs(*dbCA)
	if err != nil {
		return err
	}
	// Administrator overrides reuse the opt-in suite's MARIADB_TEST_ADMIN_* settings.
	database, _ := persistencetest.OptionsFromEnv(ca)
	database.Addr, database.CAFile = *dbAddr, ca
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	r := &reviews{ctx: ctx, options: reviewfixture.Options{Addr: *addr, StateDir: *stateDir, Database: database}}
	defer func() {
		if err := r.close(); err != nil {
			log.Printf("review server cleanup: %v", err)
		}
	}()
	err = newServer(r).Run(ctx, transport)
	if errors.Is(err, io.EOF) || errors.Is(err, context.Canceled) {
		return nil
	}
	return err
}

// reviews owns at most one running review server. Its lifetime follows the
// process context, not the tool call that started it.
type reviews struct {
	ctx     context.Context
	options reviewfixture.Options
	mu      sync.Mutex
	current *reviewfixture.Server
}

// apply sets up under both the process and the calling request, so a client
// that disconnects mid-setup stops provisioning promptly; the running server
// then lives until the next apply or process exit.
func (r *reviews) apply(request context.Context, name string) (reviewfixture.Manifest, error) {
	if !slices.Contains(reviewfixture.ScenarioNames(), name) {
		return reviewfixture.Manifest{}, fmt.Errorf("unknown scenario %q; call scenario_list", name)
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if err := r.closeLocked(); err != nil {
		return reviewfixture.Manifest{}, fmt.Errorf("stopping the previous review server: %w", err)
	}
	o := r.options
	o.Scenario = name
	setup, cancel := context.WithCancel(r.ctx)
	defer cancel()
	defer context.AfterFunc(request, cancel)()
	s, err := reviewfixture.Start(setup, o)
	if err != nil {
		return reviewfixture.Manifest{}, err
	}
	r.current = s
	log.Printf("review server running scenario %s at %s", name, s.Manifest().BaseURL)
	return s.Manifest(), nil
}

func (r *reviews) manifest() *reviewfixture.Manifest {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.current == nil {
		return nil
	}
	select {
	case err := <-r.current.Done():
		log.Printf("review server stopped: %v", err)
		_ = r.closeLocked()
		return nil
	default:
	}
	m := r.current.Manifest()
	return &m
}

func (r *reviews) close() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.closeLocked()
}

func (r *reviews) closeLocked() error {
	if r.current == nil {
		return nil
	}
	err := r.current.Close()
	r.current = nil
	return err
}

type listOutput struct {
	Scenarios []reviewfixture.ScenarioInfo `json:"scenarios"`
}
type applyInput struct {
	Name string `json:"name" jsonschema:"scenario name from scenario_list"`
}
type manifestOutput struct {
	Running  bool                    `json:"running"`
	Manifest *reviewfixture.Manifest `json:"manifest,omitempty"`
}

func newServer(r *reviews) *mcp.Server {
	server := mcp.NewServer(&mcp.Implementation{Name: "ticketopia-review", Version: "0.1.0"}, nil)
	mcp.AddTool(server, &mcp.Tool{Name: "scenario_list",
		Description: "List Ticketopia review fixture scenarios. Each adds seeded states to the baseline."},
		func(context.Context, *mcp.CallToolRequest, struct{}) (*mcp.CallToolResult, listOutput, error) {
			return nil, listOutput{reviewfixture.Scenarios()}, nil
		})
	mcp.AddTool(server, &mcp.Tool{Name: "scenario_apply",
		Description: "Start the local review server on a fresh disposable database seeded with the named scenario, replacing any running one (also a reset when the name is unchanged). Returns the manifest: base URL, routes, and per-account Playwright storage-state files. Earlier sessions and IDs become invalid. Open paths without storage state for the signed-out view."},
		func(ctx context.Context, _ *mcp.CallToolRequest, in applyInput) (*mcp.CallToolResult, manifestOutput, error) {
			m, err := r.apply(ctx, in.Name)
			if err != nil {
				return nil, manifestOutput{}, err
			}
			return nil, manifestOutput{Running: true, Manifest: &m}, nil
		})
	mcp.AddTool(server, &mcp.Tool{Name: "manifest",
		Description: "Return the running review server's manifest, or running=false if scenario_apply has not started one."},
		func(context.Context, *mcp.CallToolRequest, struct{}) (*mcp.CallToolResult, manifestOutput, error) {
			m := r.manifest()
			return nil, manifestOutput{Running: m != nil, Manifest: m}, nil
		})
	return server
}

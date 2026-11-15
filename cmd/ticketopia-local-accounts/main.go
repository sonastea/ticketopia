// Command ticketopia-local-accounts creates or removes throwaway accounts for
// trying agent tools against a local server: `create` writes their sessions,
// API tokens and OAuth tokens to .local/local-test-accounts.json; `remove`
// deletes the accounts and everything they made. It reads .env, uses the
// migration credentials, and refuses anything but a loopback site and
// database. It is a development tool and is not built into the image.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"

	"github.com/joho/godotenv"
	"github.com/sonastea/ticketopia/internal/localaccounts"
	"github.com/sonastea/ticketopia/internal/persistence"
)

const output = ".local/local-test-accounts.json"

func main() {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	if len(os.Args) != 2 || (os.Args[1] != "create" && os.Args[1] != "remove") {
		fmt.Fprintln(os.Stderr, "usage: ticketopia-local-accounts create|remove")
		os.Exit(2)
	}
	if err := run(ctx, os.Args[1]); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(ctx context.Context, action string) error {
	if err := godotenv.Load(); err != nil && !os.IsNotExist(err) {
		return errors.New("could not load .env")
	}
	// Without a configured site, the app answers on its fixed local port.
	if os.Getenv("AUTH_BASE_URL") == "" {
		os.Setenv("AUTH_BASE_URL", "http://localhost:8080")
	}
	if err := localaccounts.Guard(os.Getenv("AUTH_BASE_URL"), os.Getenv("DB_HOST")); err != nil {
		return err
	}
	config, err := persistence.ConfigFromEnv(true)
	if err != nil {
		return err
	}
	pool, err := persistence.Open(ctx, config)
	if err != nil {
		return err
	}
	defer pool.Close()
	if action == "remove" {
		n, err := pool.RemoveLocalTestAccounts(ctx)
		if err != nil {
			return err
		}
		if err := os.Remove(output); err != nil && !os.IsNotExist(err) {
			return err
		}
		fmt.Printf("Removed %d local test accounts and everything they made.\n", n)
		return nil
	}
	result, err := localaccounts.Create(ctx, pool, os.Getenv("AUTH_BASE_URL"))
	if err != nil {
		return err
	}
	data, _ := json.MarshalIndent(result, "", "  ")
	if err := os.MkdirAll(filepath.Dir(output), 0o700); err != nil {
		return err
	}
	if err := os.WriteFile(output, append(data, '\n'), 0o600); err != nil {
		return err
	}
	// Credentials stay in the file, never on the terminal.
	fmt.Printf("Created local test accounts owner (%s) and other (%s).\n", result.Accounts["owner"].ID, result.Accounts["other"].ID)
	fmt.Printf("Credentials: %s (MCP at %s; OAuth tokens last one hour, rerun to refresh).\n", output, result.MCPURL)
	return nil
}

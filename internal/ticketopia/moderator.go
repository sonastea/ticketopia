package ticketopia

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/joho/godotenv"
	"github.com/sonastea/ticketopia/internal/accounts"
	"github.com/sonastea/ticketopia/internal/moderation"
	"github.com/sonastea/ticketopia/internal/persistence"
)

type moderatorCommand struct {
	AccountID, Operator string
	Grant               bool
}

func parseModeratorCommand(args []string) (moderatorCommand, error) {
	var c moderatorCommand
	if len(args) == 0 || (args[0] != "grant" && args[0] != "revoke") {
		return c, fmt.Errorf("choose grant or revoke")
	}
	c.Grant = args[0] == "grant"
	f := flag.NewFlagSet("moderator", flag.ContinueOnError)
	f.SetOutput(io.Discard)
	f.StringVar(&c.AccountID, "account-id", "", "existing Ticketopia account ID")
	f.StringVar(&c.Operator, "operator", "", "operator label recorded in the audit trail")
	if err := f.Parse(args[1:]); err != nil {
		return c, fmt.Errorf("invalid moderator command flags")
	}
	if f.NArg() != 0 || !accounts.ValidID(c.AccountID) {
		return c, fmt.Errorf("provide --account-id with an existing 32-character account ID")
	}
	label, err := moderation.Text("operator", c.Operator, true)
	if err != nil || len(label) > 120 || strings.ContainsAny(label, "\n\t") {
		return c, fmt.Errorf("provide --operator with a single-line label up to 120 bytes")
	}
	c.Operator = label
	return c, nil
}

// ExecuteModerator uses operator credentials, never the running app's role
// grants. It does not migrate, create accounts, or assign any role by default.
func ExecuteModerator(ctx context.Context, args []string) int {
	c, err := parseModeratorCommand(args)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		fmt.Fprintln(os.Stderr, "usage: ticketopia moderator grant|revoke --account-id ID --operator LABEL")
		return 2
	}
	if err = godotenv.Load(); err != nil && !os.IsNotExist(err) {
		fmt.Fprintln(os.Stderr, "Could not load .env file")
		return 1
	}
	config, err := persistence.ConfigFromEnv(true)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	pool, err := persistence.Open(ctx, config)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	defer pool.Close()
	changed, err := pool.SetModerator(ctx, c.AccountID, c.Operator, c.Grant)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	if changed {
		fmt.Fprintln(os.Stdout, "Moderator membership changed and audited.")
	} else {
		fmt.Fprintln(os.Stdout, "Moderator membership already matches; no change.")
	}
	return 0
}

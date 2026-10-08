package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/sonastea/ticketopia/internal/ticketopia"
)

func main() {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	var status int
	switch {
	case len(os.Args) == 1:
		status = ticketopia.Execute(ctx)
	case len(os.Args) == 2 && os.Args[1] == "migrate":
		status = ticketopia.ExecuteMigrations(ctx)
	case len(os.Args) >= 2 && os.Args[1] == "moderator":
		status = ticketopia.ExecuteModerator(ctx, os.Args[2:])
	default:
		fmt.Fprintln(os.Stderr, "usage: ticketopia [migrate | moderator grant|revoke --account-id ID --operator LABEL]")
		status = 2
	}
	os.Exit(status)
}

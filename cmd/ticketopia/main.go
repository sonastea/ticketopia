package main

import (
	"context"
	"os"
	"os/signal"
	"syscall"

	"github.com/sonastea/ticketopia/internal/ticketopia"
)

func main() {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	status := ticketopia.Execute(ctx)
	os.Exit(status)
}

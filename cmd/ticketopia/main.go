package main

import (
	"context"
	"os"
	"os/signal"

	"github.com/sonastea/ticketopia/internal/ticketopia"
)

func main() {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, os.Kill)
	defer cancel()

	status := ticketopia.Execute(ctx)
	os.Exit(status)
}

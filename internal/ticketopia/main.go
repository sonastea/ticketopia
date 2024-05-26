package ticketopia

import (
	"context"

	"github.com/sonastea/ticketopia/internal/api"
)

func Execute(ctx context.Context) int {
  srv := api.InitializeEcho(ctx)

  go func() {
  _ = srv.Start(":8080")
  }()

  <-ctx.Done()

  _ = srv.Shutdown(ctx)

  return 0
}

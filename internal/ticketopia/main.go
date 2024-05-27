package ticketopia

import (
	"context"

	"github.com/sonastea/ticketopia/internal/api"
	"github.com/sonastea/ticketopia/internal/logger"
)

func Execute(ctx context.Context) int {
  logger := logger.NewLogger(ctx, "component", "api")

  api := api.NewAPI(ctx, logger)
  srv := api.Server(8080)

  go func() {
    _ = srv.ListenAndServe()
  }()

  logger.Info().Msg("API started")

  <-ctx.Done()

  _ = srv.Shutdown(ctx)

  return 0
}

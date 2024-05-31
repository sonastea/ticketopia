package ticketopia

import (
	"context"
	"net/http"

	"github.com/joho/godotenv"
	"github.com/sonastea/ticketopia/internal/api"
	"github.com/sonastea/ticketopia/internal/infra"
	"github.com/sonastea/ticketopia/internal/logger"
)

func Execute(ctx context.Context) int {
	logger := logger.NewLogger(ctx, "component", "api")

	err := godotenv.Load()
	if err != nil {
		logger.Fatal().Err(err).Msg("Error loading .env file...")
	}

	redis, err := infra.NewRedisClient(ctx)
	if err != nil {
		logger.Fatal().Err(err).Msg("Error creating new redis client...")
		return 1
	}

	api := api.NewAPI(ctx, logger, redis)
	srv := api.Server(8080)

	srvCh := make(chan error, 1)

	go func() {
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			srvCh <- err
		}
		close(srvCh)
	}()

	logger.Info().Msg("API started...")

	select {
	case <-ctx.Done():
		if err := srv.Shutdown(ctx); err != nil {
			logger.Error().Err(err).Msg("Server shutdown failed...")
			return 1
		}
		logger.Info().Msg("Server stopped gracefully...")

	case err := <-srvCh:
		logger.Error().Err(err).Msg("Server experienced an error...")
		return 1
	}

	return 0
}

package ticketopia

import (
	"context"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/joho/godotenv"
	"github.com/sonastea/ticketopia/internal/api"
	"github.com/sonastea/ticketopia/internal/infra"
	"github.com/sonastea/ticketopia/internal/logger"
)

func Execute(ctx context.Context) int {
	logger := logger.NewLogger(ctx, "component", "api")

	err := godotenv.Load()
	if err != nil && !os.IsNotExist(err) {
		logger.Error().Err(err).Msg("Error loading .env file...")
		return 1
	}

	cache := infra.NewCache(ctx, logger)
	defer func() {
		if err := cache.Close(); err != nil {
			logger.Error().Err(err).Msg("Error closing cache")
		}
	}()

	api, err := api.NewAPI(ctx, logger, cache)
	if err != nil {
		logger.Error().Err(err).Msg("Invalid API configuration")
		return 1
	}
	srv := api.Server(8080)

	srvCh := make(chan error, 1)

	go func() {
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			srvCh <- err
		}
		close(srvCh)
	}()

	sourceCommit := strings.TrimSpace(os.Getenv("SOURCE_COMMIT"))
	if sourceCommit == "" {
		sourceCommit = "unknown"
	}
	logger.Info().Str("source_commit", sourceCommit).Msg("API started...")

	select {
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := srv.Shutdown(shutdownCtx); err != nil {
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

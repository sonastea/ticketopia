package ticketopia

import (
	"context"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/joho/godotenv"
	"github.com/rs/zerolog"
	"github.com/sonastea/ticketopia/internal/accounts"
	"github.com/sonastea/ticketopia/internal/api"
	"github.com/sonastea/ticketopia/internal/discovery"
	"github.com/sonastea/ticketopia/internal/events"
	"github.com/sonastea/ticketopia/internal/history"
	"github.com/sonastea/ticketopia/internal/infra"
	"github.com/sonastea/ticketopia/internal/logger"
	"github.com/sonastea/ticketopia/internal/persistence"
)

func Execute(ctx context.Context) int {
	logger := logger.NewLogger(ctx, "component", "api")

	err := godotenv.Load()
	if err != nil && !os.IsNotExist(err) {
		logger.Error().Msg("Error loading .env file")
		return 1
	}
	config, err := persistence.ConfigFromEnv(false)
	if err != nil {
		logger.Error().Err(err).Msg("Invalid persistence configuration")
		return 1
	}
	var options []api.Option
	historyConfig, err := history.ConfigFromEnv()
	if err != nil {
		logger.Error().Err(err).Msg("Invalid event history configuration")
		return 1
	}
	if historyConfig.Enabled && !config.Enabled {
		logger.Error().Msg("Event history collection requires PERSISTENCE_MODE=mariadb")
		return 1
	}
	providerConfig, err := discovery.ConfigFromEnv()
	if err != nil {
		logger.Error().Err(err).Msg("Invalid Ticketmaster configuration")
		return 1
	}
	var historyRepository *persistence.HistoryRepository
	authConfig, err := accounts.ConfigFromEnv()
	if err != nil {
		logger.Error().Err(err).Msg("Invalid authentication configuration")
		return 1
	}
	if authConfig.Enabled && !config.Enabled {
		logger.Error().Msg("Authentication requires PERSISTENCE_MODE=mariadb")
		return 1
	}
	if config.Enabled {
		pool, err := persistence.Open(ctx, config)
		if err != nil {
			logger.Error().Err(err).Msg("Persistence initialization failed")
			return 1
		}
		// Execute does not return until HTTP shutdown (or forced close) finishes.
		defer func() {
			if err := pool.Close(); err != nil {
				logger.Error().Err(err).Msg("Error closing database pool")
			}
		}()
		options = append(options, api.WithPersistence(pool.Ready, events.New(pool.Events())))
		providerConfig.Coordinator = pool.ProviderBudget(providerConfig.APIKey, providerConfig.DailyBudget)
		historyRepository = pool.History()
		// Shared snapshot fallback is public metadata, independent of accounts.
		options = append(options, api.WithSavedEvents(pool.Saved()))
		if authConfig.Enabled {
			options = append(options, api.WithEventInterests(pool.Interests()))
			options = append(options, api.WithEventRecommendations(pool.Recommendations()))
			options = append(options, api.WithEventDiscussions(pool.Discussions()))
			options = append(options, api.WithModeration(pool.Moderation()))
			options = append(options, api.WithAccounts(authConfig, accounts.New(pool.Accounts(), accounts.NewGoogle(ctx, authConfig))))
		}
	}

	cache := infra.NewCache(ctx, logger)
	defer func() {
		if err := cache.Close(); err != nil {
			logger.Error().Err(err).Msg("Error closing cache")
		}
	}()

	provider := discovery.New(ctx, cache, logger, providerConfig)
	options = append([]api.Option{api.WithDiscovery(provider)}, options...)
	api, err := api.NewAPI(ctx, logger, cache, options...)
	if err != nil {
		logger.Error().Err(err).Msg("Invalid API configuration")
		return 1
	}
	if historyConfig.Enabled {
		workerCtx, cancel := context.WithCancel(ctx)
		done := make(chan struct{})
		go func() {
			defer close(done)
			history.New(historyRepository, provider, historyConfig, logger).Run(workerCtx)
		}()
		defer func() { cancel(); <-done }()
	}
	srv := api.Server(8080)
	return serve(ctx, srv, srv.ListenAndServe, logger)
}

// serve returns only after HTTP has drained (or has been forcibly closed).
// Execute's resource defers therefore run after the HTTP lifecycle, not before.
func serve(ctx context.Context, srv *http.Server, listen func() error, logger zerolog.Logger) int {
	srvCh := make(chan error, 1)

	go func() {
		if err := listen(); err != nil && err != http.ErrServerClosed {
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
			_ = srv.Close()
			logger.Error().Err(err).Msg("Server shutdown failed...")
			return 1
		}
		logger.Info().Msg("Server stopped gracefully...")

	case err := <-srvCh:
		_ = srv.Close()
		logger.Error().Err(err).Msg("Server experienced an error...")
		return 1
	}

	return 0
}

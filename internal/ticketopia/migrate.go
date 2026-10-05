package ticketopia

import (
	"context"
	"os"

	"github.com/joho/godotenv"
	"github.com/sonastea/ticketopia/internal/logger"
	"github.com/sonastea/ticketopia/internal/persistence"
)

// ExecuteMigrations is a shell-free, non-root command using separate credentials.
func ExecuteMigrations(ctx context.Context) int {
	log := logger.NewLogger(ctx, "component", "migrations")
	if err := godotenv.Load(); err != nil && !os.IsNotExist(err) {
		log.Error().Msg("Error loading .env file")
		return 1
	}
	config, err := persistence.ConfigFromEnv(true)
	if err == nil {
		err = persistence.Migrate(ctx, config)
	}
	if err != nil {
		log.Error().Err(err).Msg("Migration failed")
		return 1
	}
	log.Info().Int("schema_version", persistence.SupportedSchemaVersion).Msg("Migrations complete")
	return 0
}

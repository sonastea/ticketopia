package logger

import (
	"context"
	"os"

	"github.com/rs/zerolog"
)

func NewLogger(ctx context.Context, label, name string) zerolog.Logger {
	logger := zerolog.New(os.Stdout).With().Str(label, name).Logger()
	logger.WithContext(ctx)

	return logger
}

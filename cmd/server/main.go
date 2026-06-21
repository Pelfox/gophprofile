package main

import (
	"context"
	"os"

	"github.com/pelfox/gophprofile/internal/app"
	"github.com/pelfox/gophprofile/internal/config"
	"github.com/pelfox/gophprofile/internal/observability"
	"github.com/rs/zerolog"
)

func main() {
	zerolog.TimeFieldFormat = zerolog.TimeFormatUnix
	logger := zerolog.New(os.Stdout).With().Timestamp().Logger()

	cfg, err := config.Load()
	if err != nil {
		logger.Fatal().Err(err).Msg("failed to load configuration")
	}

	// Initializing OpenTelemetry tracing.
	shutdownTracing, err := observability.InitTracing(
		context.Background(),
		"gophprofile-api",
		cfg.TelemetryEndpoint,
	)
	if err != nil {
		logger.Fatal().Err(err).Msg("failed to initialize OpenTelemetry tracing")
	}
	defer shutdownTracing(context.Background())

	if err := app.Run(logger, cfg); err != nil {
		logger.Fatal().Err(err).Msg("failed to run the application")
	}
}

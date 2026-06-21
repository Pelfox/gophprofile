package main

import (
	"context"
	"os"
	"os/signal"
	"syscall"

	"github.com/pelfox/gophprofile/internal/config"
	"github.com/pelfox/gophprofile/internal/observability"
	"github.com/pelfox/gophprofile/internal/worker"
	"github.com/rs/zerolog"
)

func main() {
	zerolog.TimeFieldFormat = zerolog.TimeFormatUnix
	logger := zerolog.New(os.Stdout).With().Timestamp().Logger()

	cfg, err := config.LoadWorkerConfig()
	if err != nil {
		logger.Fatal().Err(err).Msg("failed to load worker configuration")
	}

	// Initializing OpenTelemetry tracing.
	shutdownTracing, err := observability.InitTracing(
		context.Background(),
		"gophprofile-worker",
		cfg.TelemetryEndpoint,
	)
	if err != nil {
		logger.Fatal().Err(err).Msg("failed to initialize OpenTelemetry tracing")
	}
	defer shutdownTracing(context.Background())

	ctx, stop := signal.NotifyContext(
		context.Background(),
		os.Interrupt,
		syscall.SIGTERM,
	)
	defer stop()

	if err := worker.Run(ctx, logger, cfg); err != nil {
		logger.Fatal().Err(err).Msg("failed to run the worker")
	}
}

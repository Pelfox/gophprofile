package main

import (
	"context"
	"os"

	"github.com/pelfox/gophprofile/internal/app"
	"github.com/pelfox/gophprofile/internal/config"
	"github.com/pelfox/gophprofile/internal/observability"
	"github.com/prometheus/client_golang/prometheus"
)

func main() {
	logger := observability.NewLogger(os.Stdout)

	cfg, err := config.Load()
	if err != nil {
		logger.Fatal().Err(err).Msg("failed to load configuration")
	}

	metricsRegistry := prometheus.NewRegistry()
	if err := observability.InitMetrics(metricsRegistry); err != nil {
		logger.Fatal().Err(err).Msg("failed to initialize Prometheus metrics")
	}
	metricsHandler := observability.MetricsHandler(metricsRegistry)

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

	if err := app.Run(logger, cfg, metricsHandler); err != nil {
		logger.Fatal().Err(err).Msg("failed to run the application")
	}
}

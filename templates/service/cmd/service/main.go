// TEMPLATE: Entry point for __SERVICE_FULL__
// Copy this file and replace all __PLACEHOLDER__ markers with your service values.
package main

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"

	// TEMPLATE: Update these imports to match your service module path.
	"git.cscs.ch/openchami/__SERVICE_FULL__/internal/config"
	"git.cscs.ch/openchami/__SERVICE_FULL__/internal/server"
	"git.cscs.ch/openchami/__SERVICE_FULL__/internal/store"

	// Shared library packages.
	"git.cscs.ch/openchami/chamicore-lib/dbutil"
	"git.cscs.ch/openchami/chamicore-lib/otel"
)

// version is set at build time via -ldflags.
var (
	version   = "dev"
	commit    = "none"
	buildDate = "unknown"
)

func main() {
	// ---------------------------------------------------------------
	// 1. Load configuration from environment variables.
	// ---------------------------------------------------------------
	cfg, err := config.Load()
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to load config: %v\n", err)
		os.Exit(1)
	}

	// ---------------------------------------------------------------
	// 2. Initialise zerolog.
	// ---------------------------------------------------------------
	level, err := zerolog.ParseLevel(cfg.LogLevel)
	if err != nil {
		level = zerolog.InfoLevel
	}
	zerolog.SetGlobalLevel(level)

	if cfg.DevMode {
		log.Logger = log.Output(zerolog.ConsoleWriter{Out: os.Stderr, TimeFormat: time.RFC3339})
	} else {
		zerolog.TimeFieldFormat = zerolog.TimeFormatUnixMs
		log.Logger = zerolog.New(os.Stderr).With().
			Timestamp().
			Str("service", "__SERVICE__").
			Str("version", version).
			Logger()
	}

	logger := log.With().Str("component", "main").Logger()
	logger.Info().
		Str("version", version).
		Str("commit", commit).
		Str("build_date", buildDate).
		Msg("starting __SERVICE_FULL__")

	if cfg.DevMode {
		logger.Warn().Msg("DEV MODE ENABLED — authentication is bypassed; do not use in production")
	}

	// ---------------------------------------------------------------
	// 3. Initialise OpenTelemetry (traces + metrics).
	// ---------------------------------------------------------------
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	shutdownOTel, err := otel.Init(ctx, otel.Config{
		ServiceName:    "__SERVICE_FULL__",
		ServiceVersion: version,
		MetricsEnabled: cfg.MetricsEnabled,
		TracesEnabled:  cfg.TracesEnabled,
	})
	if err != nil {
		logger.Fatal().Err(err).Msg("failed to initialise OpenTelemetry")
	}
	defer func() {
		shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer shutdownCancel()
		if err := shutdownOTel(shutdownCtx); err != nil {
			logger.Error().Err(err).Msg("failed to shut down OpenTelemetry")
		}
	}()

	// ---------------------------------------------------------------
	// 4. Connect to PostgreSQL.
	// ---------------------------------------------------------------
	db, err := dbutil.Connect(ctx, dbutil.PoolConfig{
		DSN: cfg.DBDSN,
		// TEMPLATE: Tune pool sizes per service needs and ADR-014 pool budget.
	})
	if err != nil {
		logger.Fatal().Err(err).Msg("failed to connect to database")
	}
	defer db.Close()
	logger.Info().Msg("connected to PostgreSQL")

	// ---------------------------------------------------------------
	// 5. Run database migrations.
	// ---------------------------------------------------------------
	result, err := dbutil.RunMigrations(db, "file://migrations/postgres")
	if err != nil {
		logger.Fatal().Err(err).Msg("failed to run database migrations")
	}
	logger.Info().Uint("version", result.Version).Bool("dirty", result.Dirty).Msg("database migration complete")

	// ---------------------------------------------------------------
	// 6. Create store.
	// ---------------------------------------------------------------
	st := store.NewPostgresStore(db)

	// ---------------------------------------------------------------
	// 7. Create server (chi router + middleware).
	// ---------------------------------------------------------------
	srv := server.New(st, cfg, version, commit, buildDate)
	httpServer := &http.Server{
		Addr:              cfg.ListenAddr,
		Handler:           srv.Router(),
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      60 * time.Second,
		IdleTimeout:       120 * time.Second,
	}

	// ---------------------------------------------------------------
	// 8. Graceful shutdown on SIGTERM / SIGINT.
	// ---------------------------------------------------------------
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	errCh := make(chan error, 1)
	go func() {
		logger.Info().Str("addr", cfg.ListenAddr).Msg("HTTP server listening")
		if err := httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			errCh <- err
		}
	}()

	select {
	case sig := <-sigCh:
		logger.Info().Str("signal", sig.String()).Msg("received shutdown signal")
	case err := <-errCh:
		logger.Error().Err(err).Msg("HTTP server error")
	}

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer shutdownCancel()
	if err := httpServer.Shutdown(shutdownCtx); err != nil {
		logger.Error().Err(err).Msg("HTTP server shutdown error")
	}
	logger.Info().Msg("server stopped gracefully")
}

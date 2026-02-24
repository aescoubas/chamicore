// Package main is the entry point for the chamicore-power service.
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

	"git.cscs.ch/openchami/chamicore-lib/dbutil"
	"git.cscs.ch/openchami/chamicore-lib/otel"
	"git.cscs.ch/openchami/chamicore-power/api"
	"git.cscs.ch/openchami/chamicore-power/internal/config"
	"git.cscs.ch/openchami/chamicore-power/internal/server"
	"git.cscs.ch/openchami/chamicore-power/internal/store"
)

var (
	version   = "dev"
	commit    = "none"
	buildDate = "unknown"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to load config: %v\n", err)
		os.Exit(1)
	}

	level, err := zerolog.ParseLevel(cfg.LogLevel)
	if err != nil {
		level = zerolog.InfoLevel
	}
	zerolog.SetGlobalLevel(level)

	if cfg.DevMode {
		log.Logger = log.Output(zerolog.ConsoleWriter{Out: os.Stderr, TimeFormat: time.RFC3339})
	} else {
		zerolog.TimeFieldFormat = zerolog.TimeFormatUnixMs
		log.Logger = zerolog.New(os.Stderr).With().Timestamp().Str("service", "power").Str("version", version).Logger()
	}

	logger := log.With().Str("component", "main").Logger()
	logger.Info().Str("version", version).Str("commit", commit).Str("build_date", buildDate).Msg("starting chamicore-power")
	if cfg.DevMode {
		logger.Warn().Msg("DEV MODE ENABLED - authentication is bypassed; do not use in production")
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	shutdownOTel, err := otel.Init(ctx, otel.Config{
		ServiceName:    "chamicore-power",
		ServiceVersion: version,
		MetricsEnabled: cfg.MetricsEnabled,
		TracesEnabled:  cfg.TracesEnabled,
	})
	if err != nil {
		logger.Fatal().Err(err).Msg("failed to initialize OpenTelemetry")
	}
	defer func() {
		shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer shutdownCancel()
		if shutdownErr := shutdownOTel(shutdownCtx); shutdownErr != nil {
			logger.Error().Err(shutdownErr).Msg("failed to shut down OpenTelemetry")
		}
	}()

	db, err := dbutil.Connect(ctx, dbutil.PoolConfig{DSN: cfg.DBDSN})
	if err != nil {
		logger.Fatal().Err(err).Msg("failed to connect to database")
	}
	defer db.Close()
	logger.Info().Msg("connected to PostgreSQL")

	if _, schemaErr := db.ExecContext(ctx, "CREATE SCHEMA IF NOT EXISTS power"); schemaErr != nil {
		logger.Fatal().Err(schemaErr).Msg("failed to ensure power schema exists")
	}

	result, err := dbutil.RunMigrations(db, "migrations/postgres")
	if err != nil {
		logger.Fatal().Err(err).Msg("failed to run database migrations")
	}
	logger.Info().Uint("version", result.Version).Bool("dirty", result.Dirty).Msg("database migration complete")

	st := store.NewPostgresStore(db)
	srv := server.New(st, cfg, version, commit, buildDate, server.WithOpenAPISpec(api.OpenAPISpec))

	httpServer := &http.Server{
		Addr:              cfg.ListenAddr,
		Handler:           srv.Router(),
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      60 * time.Second,
		IdleTimeout:       120 * time.Second,
	}

	errCh := make(chan error, 1)
	go func() {
		logger.Info().Str("addr", cfg.ListenAddr).Msg("HTTP server listening")
		if serveErr := httpServer.ListenAndServe(); serveErr != nil && serveErr != http.ErrServerClosed {
			errCh <- serveErr
		}
	}()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	select {
	case sig := <-sigCh:
		logger.Info().Str("signal", sig.String()).Msg("received shutdown signal")
	case serveErr := <-errCh:
		logger.Error().Err(serveErr).Msg("HTTP server error")
	}
	cancel()

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer shutdownCancel()
	if shutdownErr := httpServer.Shutdown(shutdownCtx); shutdownErr != nil {
		logger.Error().Err(shutdownErr).Msg("HTTP server shutdown error")
	}
	logger.Info().Msg("server stopped gracefully")
}

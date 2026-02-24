// Package main is the entry point for the chamicore-power service.
package main

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"

	"git.cscs.ch/openchami/chamicore-lib/dbutil"
	"git.cscs.ch/openchami/chamicore-lib/events/nats"
	"git.cscs.ch/openchami/chamicore-lib/events/outbox"
	baseclient "git.cscs.ch/openchami/chamicore-lib/httputil/client"
	"git.cscs.ch/openchami/chamicore-lib/otel"
	"git.cscs.ch/openchami/chamicore-power/api"
	"git.cscs.ch/openchami/chamicore-power/internal/config"
	"git.cscs.ch/openchami/chamicore-power/internal/engine"
	"git.cscs.ch/openchami/chamicore-power/internal/server"
	powersmd "git.cscs.ch/openchami/chamicore-power/internal/smd"
	"git.cscs.ch/openchami/chamicore-power/internal/store"
	powersync "git.cscs.ch/openchami/chamicore-power/internal/sync"
	smdclient "git.cscs.ch/openchami/chamicore-smd/pkg/client"
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
	smd := smdclient.New(smdclient.Config{
		BaseURL: cfg.SMDURL,
		Token:   cfg.InternalToken,
	})

	if strings.TrimSpace(cfg.NATSURL) != "" {
		publisher, publisherErr := nats.NewPublisher(nats.Config{
			URL:  cfg.NATSURL,
			Name: "chamicore-power-outbox-relay",
			Stream: nats.StreamConfig{
				Name:     cfg.NATSStream,
				Subjects: []string{"chamicore.power.>"},
			},
		})
		if publisherErr != nil {
			logger.Warn().Err(publisherErr).Str("nats_url", cfg.NATSURL).Msg("outbox relay disabled: failed to initialize NATS publisher")
		} else {
			defer func() {
				if closeErr := publisher.Close(); closeErr != nil {
					logger.Error().Err(closeErr).Msg("failed to close NATS publisher")
				}
			}()

			relay, relayErr := outbox.NewRelay(db, publisher, outbox.Config{})
			if relayErr != nil {
				logger.Warn().Err(relayErr).Msg("outbox relay disabled: failed to initialize relay")
			} else {
				go func() {
					if runErr := relay.Run(ctx); runErr != nil {
						logger.Error().Err(runErr).Msg("outbox relay stopped with error")
					}
				}()
				logger.Info().Str("nats_url", cfg.NATSURL).Str("stream", cfg.NATSStream).Msg("outbox relay started")
			}
		}
	}

	mappingSync := powersync.New(st, smd, powersync.Config{
		Interval:            cfg.MappingSyncInterval,
		SyncOnStartup:       cfg.MappingSyncOnStartup,
		DefaultCredentialID: cfg.DefaultCredentialID,
	}, logger.With().Str("component", "mapping-sync").Logger())
	go mappingSync.Run(ctx)

	stateUpdater := powersmd.NewUpdater(smd)
	runner := engine.New(st, engine.NoopExecutor{}, engine.ExpectedStateReader{}, engine.Config{
		GlobalConcurrency:  cfg.GlobalConcurrency,
		PerBMCConcurrency:  cfg.PerBMCConcurrency,
		RetryAttempts:      cfg.RetryAttempts,
		RetryBackoffBase:   cfg.RetryBackoffBase,
		RetryBackoffMax:    cfg.RetryBackoffMax,
		TransitionDeadline: cfg.TransitionDeadline,
		VerificationWindow: cfg.VerificationWindow,
		VerificationPoll:   cfg.VerificationPoll,
	}, engine.WithNodeStateUpdater(stateUpdater))
	runner.Start(ctx)

	resolveGroupMembers := func(ctx context.Context, group string) ([]string, error) {
		groupName := strings.TrimSpace(group)
		if groupName == "" {
			return nil, fmt.Errorf("%w: group name is required", server.ErrGroupNotFound)
		}

		resource, err := smd.GetGroup(ctx, groupName)
		if err != nil {
			var apiErr *baseclient.APIError
			if errors.As(err, &apiErr) && apiErr.StatusCode == http.StatusNotFound {
				return nil, fmt.Errorf("%w: %s", server.ErrGroupNotFound, groupName)
			}
			return nil, err
		}

		return append([]string(nil), resource.Spec.Members...), nil
	}

	srv := server.New(
		st,
		cfg,
		version,
		commit,
		buildDate,
		server.WithOpenAPISpec(api.OpenAPISpec),
		server.WithMappingSyncer(mappingSync),
		server.WithTransitionRunner(runner),
		server.WithGroupMemberResolver(resolveGroupMembers),
	)

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

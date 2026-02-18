// TEMPLATE: Entry point for __SERVICE_FULL__
// Copy this file and replace all __PLACEHOLDER__ markers with your service values.
package main

import (
	"context"
	"database/sql"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/golang-migrate/migrate/v4"
	"github.com/golang-migrate/migrate/v4/database/postgres"
	_ "github.com/golang-migrate/migrate/v4/source/file"
	_ "github.com/lib/pq"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/exporters/prometheus"
	"go.opentelemetry.io/otel/propagation"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.24.0"

	// TEMPLATE: Update these imports to match your service module path.
	"git.cscs.ch/openchami/__SERVICE_FULL__/internal/config"
	"git.cscs.ch/openchami/__SERVICE_FULL__/internal/server"
	"git.cscs.ch/openchami/__SERVICE_FULL__/internal/store"
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

	// ---------------------------------------------------------------
	// 3. Initialise OpenTelemetry (traces + metrics).
	// ---------------------------------------------------------------
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	shutdownOTel, err := initOTel(ctx, cfg)
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
	db, err := sql.Open("postgres", cfg.DBDSN)
	if err != nil {
		logger.Fatal().Err(err).Msg("failed to open database connection")
	}
	defer db.Close()

	db.SetMaxOpenConns(25)
	db.SetMaxIdleConns(5)
	db.SetConnMaxLifetime(5 * time.Minute)

	pingCtx, pingCancel := context.WithTimeout(ctx, 5*time.Second)
	defer pingCancel()
	if err := db.PingContext(pingCtx); err != nil {
		logger.Fatal().Err(err).Msg("failed to ping database")
	}
	logger.Info().Msg("connected to PostgreSQL")

	// ---------------------------------------------------------------
	// 5. Run database migrations.
	// ---------------------------------------------------------------
	if err := runMigrations(db); err != nil {
		logger.Fatal().Err(err).Msg("failed to run database migrations")
	}

	// ---------------------------------------------------------------
	// 6. Create store.
	// ---------------------------------------------------------------
	st := store.NewPostgresStore(db)

	// ---------------------------------------------------------------
	// 7. Create server (chi router + middleware).
	// ---------------------------------------------------------------
	srv := server.New(st, cfg, version)
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

// ---------------------------------------------------------------------------
// initOTel sets up the OpenTelemetry trace and metric providers.
// ---------------------------------------------------------------------------
func initOTel(ctx context.Context, cfg config.Config) (func(context.Context) error, error) {
	res, err := resource.New(ctx,
		resource.WithAttributes(
			semconv.ServiceNameKey.String("__SERVICE_FULL__"),
			semconv.ServiceVersionKey.String(version),
		),
	)
	if err != nil {
		return nil, fmt.Errorf("creating OTel resource: %w", err)
	}

	var shutdownFuncs []func(context.Context) error

	// -- Traces ----------------------------------------------------------
	if cfg.TracesEnabled {
		traceExporter, err := otlptracegrpc.New(ctx)
		if err != nil {
			return nil, fmt.Errorf("creating trace exporter: %w", err)
		}
		tp := sdktrace.NewTracerProvider(
			sdktrace.WithBatcher(traceExporter),
			sdktrace.WithResource(res),
		)
		otel.SetTracerProvider(tp)
		shutdownFuncs = append(shutdownFuncs, tp.Shutdown)
	}

	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(
		propagation.TraceContext{},
		propagation.Baggage{},
	))

	// -- Metrics ---------------------------------------------------------
	if cfg.MetricsEnabled {
		promExporter, err := prometheus.New()
		if err != nil {
			return nil, fmt.Errorf("creating prometheus exporter: %w", err)
		}
		mp := sdkmetric.NewMeterProvider(
			sdkmetric.WithReader(promExporter),
			sdkmetric.WithResource(res),
		)
		otel.SetMeterProvider(mp)
		shutdownFuncs = append(shutdownFuncs, mp.Shutdown)
	}

	shutdown := func(ctx context.Context) error {
		var firstErr error
		for _, fn := range shutdownFuncs {
			if err := fn(ctx); err != nil && firstErr == nil {
				firstErr = err
			}
		}
		return firstErr
	}
	return shutdown, nil
}

// ---------------------------------------------------------------------------
// runMigrations applies any pending database migrations from the embedded
// migrations directory.
// ---------------------------------------------------------------------------
func runMigrations(db *sql.DB) error {
	driver, err := postgres.WithInstance(db, &postgres.Config{})
	if err != nil {
		return fmt.Errorf("creating migrate driver: %w", err)
	}

	// TEMPLATE: Adjust the migration source path as needed.
	m, err := migrate.NewWithDatabaseInstance(
		"file://migrations/postgres",
		"postgres",
		driver,
	)
	if err != nil {
		return fmt.Errorf("creating migrate instance: %w", err)
	}

	if err := m.Up(); err != nil && err != migrate.ErrNoChange {
		return fmt.Errorf("running migrations: %w", err)
	}

	ver, dirty, _ := m.Version()
	log.Info().Uint("version", ver).Bool("dirty", dirty).Msg("database migration complete")
	return nil
}

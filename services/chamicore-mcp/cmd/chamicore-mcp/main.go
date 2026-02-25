// Package main is the entry point for the chamicore-mcp service.
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

	"git.cscs.ch/openchami/chamicore-lib/otel"
	"git.cscs.ch/openchami/chamicore-mcp/api"
	mcpauth "git.cscs.ch/openchami/chamicore-mcp/internal/auth"
	"git.cscs.ch/openchami/chamicore-mcp/internal/config"
	"git.cscs.ch/openchami/chamicore-mcp/internal/policy"
	"git.cscs.ch/openchami/chamicore-mcp/internal/server"
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
	zerolog.TimeFieldFormat = zerolog.TimeFormatUnixMs
	log.Logger = zerolog.New(os.Stderr).With().Timestamp().Str("service", "mcp").Str("version", version).Logger()

	logger := log.With().Str("component", "main").Logger()
	logger.Info().Str("transport", cfg.Transport).Msg("starting chamicore-mcp")

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	shutdownOTel, err := otel.Init(ctx, otel.Config{
		ServiceName:    "chamicore-mcp",
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

	registry, err := server.NewToolRegistry(api.ToolsContract)
	if err != nil {
		logger.Fatal().Err(err).Msg("failed to parse MCP tool contract")
	}
	modeGuard, err := policy.NewGuard(cfg.Mode, cfg.EnableWrite)
	if err != nil {
		logger.Fatal().Err(err).Msg("invalid mode configuration")
	}
	resolvedToken, err := mcpauth.ResolveToken(mcpauth.TokenSourceOptions{
		AllowCLIConfigToken: cfg.AllowCLIConfigToken,
		CLIConfigPath:       cfg.CLIConfigPath,
	})
	if err != nil {
		logger.Fatal().Err(err).Msg("failed to resolve token source")
	}
	if resolvedToken.Token == "" {
		logger.Warn().Msg("no upstream token resolved from CHAMICORE_MCP_TOKEN, CHAMICORE_TOKEN, or CLI config")
	} else {
		logger.Info().Str("token_source", string(resolvedToken.Source)).Msg("resolved upstream token source")
	}
	logger.Info().Str("mode", modeGuard.Mode()).Bool("write_enabled", cfg.EnableWrite).Msg("execution policy initialized")

	switch cfg.Transport {
	case config.TransportStdio:
		if runErr := server.RunStdio(ctx, os.Stdin, os.Stdout, registry, modeGuard, version, logger); runErr != nil {
			logger.Error().Err(runErr).Msg("stdio runtime stopped with error")
			os.Exit(1)
		}
		logger.Info().Msg("stdio runtime stopped")

	case config.TransportHTTP:
		httpServer := server.NewHTTPServer(cfg, version, commit, buildDate, api.ToolsContract, registry, modeGuard, logger)
		srv := &http.Server{
			Addr:              cfg.ListenAddr,
			Handler:           httpServer.Router(),
			ReadHeaderTimeout: 10 * time.Second,
			ReadTimeout:       30 * time.Second,
			WriteTimeout:      0, // allow SSE streaming without forcing writer timeout.
			IdleTimeout:       120 * time.Second,
		}

		errCh := make(chan error, 1)
		go func() {
			logger.Info().Str("addr", cfg.ListenAddr).Msg("HTTP server listening")
			if serveErr := srv.ListenAndServe(); serveErr != nil && serveErr != http.ErrServerClosed {
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
		if shutdownErr := srv.Shutdown(shutdownCtx); shutdownErr != nil {
			logger.Error().Err(shutdownErr).Msg("HTTP server shutdown error")
			os.Exit(1)
		}
		logger.Info().Msg("server stopped gracefully")

	default:
		logger.Fatal().Str("transport", cfg.Transport).Msg("unsupported transport")
	}
}

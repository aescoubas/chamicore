// Package server provides the power HTTP server.
package server

import (
	"context"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/rs/zerolog/log"

	"git.cscs.ch/openchami/chamicore-lib/auth"
	"git.cscs.ch/openchami/chamicore-lib/httputil"
	"git.cscs.ch/openchami/chamicore-lib/otel"
	"git.cscs.ch/openchami/chamicore-power/internal/config"
	"git.cscs.ch/openchami/chamicore-power/internal/store"
)

// Server wraps HTTP routes and dependencies.
type Server struct {
	store       store.Store
	cfg         config.Config
	version     string
	commit      string
	buildDate   string
	openapiSpec []byte
	router      chi.Router
}

// Option configures server construction.
type Option func(*Server)

// WithOpenAPISpec sets the embedded OpenAPI bytes.
func WithOpenAPISpec(spec []byte) Option {
	return func(s *Server) {
		s.openapiSpec = spec
	}
}

// New constructs a power API server.
func New(st store.Store, cfg config.Config, version, commit, buildDate string, opts ...Option) *Server {
	s := &Server{
		store:     st,
		cfg:       cfg,
		version:   version,
		commit:    commit,
		buildDate: buildDate,
	}
	for _, opt := range opts {
		opt(s)
	}
	s.router = s.buildRouter()
	return s
}

// Router returns the configured router.
func (s *Server) Router() chi.Router {
	return s.router
}

func (s *Server) buildRouter() chi.Router {
	r := chi.NewRouter()

	r.Use(otel.HTTPTracing())
	r.Use(otel.HTTPMetrics("chamicore-power"))
	r.Use(httputil.RequestID)
	r.Use(httputil.RequestLogger(log.Logger))
	r.Use(httputil.Recoverer)
	r.Use(httputil.SecureHeaders)
	r.Use(httputil.BodyLimit(1 << 20))
	r.Use(httputil.ContentType)
	r.Use(httputil.APIVersion("power/v1"))
	r.Use(httputil.CacheControl)
	r.Use(httputil.ETag)

	r.Group(func(r chi.Router) {
		r.Method(http.MethodGet, "/health", httputil.HealthHandler())
		r.Method(http.MethodGet, "/readiness", httputil.ReadinessHandler(func() error {
			return s.store.Ping(context.Background())
		}))
		r.Method(http.MethodGet, "/version", httputil.VersionHandler(s.version, s.commit, s.buildDate))
		if s.cfg.MetricsEnabled {
			r.Method(http.MethodGet, "/metrics", otel.PrometheusHandler())
		}
		r.Method(http.MethodGet, "/api/docs", httputil.SwaggerHandler())
		r.Method(http.MethodGet, "/api/openapi.yaml", httputil.OpenAPIHandler(s.openapiSpec))
	})

	r.Group(func(r chi.Router) {
		r.Use(auth.JWTMiddleware(auth.MiddlewareConfig{
			JWKSUrl:       s.cfg.JWKSURL,
			InternalToken: s.cfg.InternalToken,
			DevMode:       s.cfg.DevMode,
		}))

		r.Route("/power/v1", func(r chi.Router) {
			r.With(requireAnyScope("read:power", "admin")).Get("/transitions", s.handleListTransitions)
			r.With(requireAnyScope("write:power", "admin")).Post("/transitions", s.handleCreateTransition)
			r.With(requireAnyScope("read:power", "admin")).Get("/transitions/{id}", s.handleGetTransition)
			r.With(requireAnyScope("write:power", "admin")).Delete("/transitions/{id}", s.handleDeleteTransition)

			r.With(requireAnyScope("read:power", "admin")).Get("/power-status", s.handleGetPowerStatus)

			r.With(requireAnyScope("write:power", "admin")).Post("/actions/on", s.handleActionOn)
			r.With(requireAnyScope("write:power", "admin")).Post("/actions/off", s.handleActionOff)
			r.With(requireAnyScope("write:power", "admin")).Post("/actions/reboot", s.handleActionReboot)
			r.With(requireAnyScope("write:power", "admin")).Post("/actions/reset", s.handleActionReset)

			r.With(requireAnyScope("admin:power", "admin")).Post("/admin/mappings/sync", s.handleAdminSyncMappings)
		})
	})

	return r
}

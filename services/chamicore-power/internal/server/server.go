// Package server provides the power HTTP server.
package server

import (
	"context"
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/rs/zerolog/log"

	"git.cscs.ch/openchami/chamicore-lib/auth"
	"git.cscs.ch/openchami/chamicore-lib/httputil"
	"git.cscs.ch/openchami/chamicore-lib/otel"
	"git.cscs.ch/openchami/chamicore-power/internal/config"
	"git.cscs.ch/openchami/chamicore-power/internal/engine"
	"git.cscs.ch/openchami/chamicore-power/internal/store"
)

var errInitialMappingSyncPending = errors.New("initial mapping sync has not completed")

type mappingSyncer interface {
	Trigger(ctx context.Context) error
	IsReady() bool
}

type transitionRunner interface {
	StartTransition(ctx context.Context, req engine.StartRequest) (engine.Transition, error)
	AbortTransition(ctx context.Context, transitionID string) error
}

type transitionStore interface {
	ListTransitions(ctx context.Context, limit, offset int) ([]engine.Transition, int, error)
	GetTransition(ctx context.Context, id string) (engine.Transition, error)
	ListTransitionTasks(ctx context.Context, transitionID string) ([]engine.Task, error)
	ListLatestTransitionTasksByNode(ctx context.Context, nodeIDs []string) ([]engine.Task, error)
}

// Server wraps HTTP routes and dependencies.
type Server struct {
	store               store.Store
	transitionStore     transitionStore
	transitionRunner    transitionRunner
	resolveGroupMembers func(ctx context.Context, group string) ([]string, error)
	mappingSync         mappingSyncer
	cfg                 config.Config
	version             string
	commit              string
	buildDate           string
	openapiSpec         []byte
	router              chi.Router
}

// Option configures server construction.
type Option func(*Server)

// WithOpenAPISpec sets the embedded OpenAPI bytes.
func WithOpenAPISpec(spec []byte) Option {
	return func(s *Server) {
		s.openapiSpec = spec
	}
}

// WithMappingSyncer sets the background mapping sync controller.
func WithMappingSyncer(syncer mappingSyncer) Option {
	return func(s *Server) {
		s.mappingSync = syncer
	}
}

// WithTransitionRunner sets the async transition execution runner.
func WithTransitionRunner(runner transitionRunner) Option {
	return func(s *Server) {
		s.transitionRunner = runner
	}
}

// WithGroupMemberResolver configures a resolver for SMD group expansion.
func WithGroupMemberResolver(fn func(ctx context.Context, group string) ([]string, error)) Option {
	return func(s *Server) {
		s.resolveGroupMembers = fn
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
	if ts, ok := any(st).(transitionStore); ok {
		s.transitionStore = ts
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
			if err := s.store.Ping(context.Background()); err != nil {
				return err
			}
			if s.mappingSync != nil && !s.mappingSync.IsReady() {
				return errInitialMappingSyncPending
			}
			return nil
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

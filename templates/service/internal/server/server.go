// TEMPLATE: Chi router and middleware stack for __SERVICE_FULL__
// Copy this file and replace all __PLACEHOLDER__ markers with your service values.
package server

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/rs/zerolog/log"

	// TEMPLATE: Update these imports to match your service module path.
	"git.cscs.ch/openchami/__SERVICE_FULL__/internal/config"
	"git.cscs.ch/openchami/__SERVICE_FULL__/internal/store"

	// Shared library packages.
	"git.cscs.ch/openchami/chamicore-lib/auth"
	"git.cscs.ch/openchami/chamicore-lib/httputil"
	"git.cscs.ch/openchami/chamicore-lib/otel"
)

// Server holds the HTTP handler, the backing store, and service configuration.
type Server struct {
	store     store.Store
	cfg       config.Config
	version   string
	commit    string
	buildDate string
	router    chi.Router
}

// New constructs a fully-configured Server with the middleware stack and all
// route groups mounted.
func New(st store.Store, cfg config.Config, version, commit, buildDate string) *Server {
	s := &Server{
		store:     st,
		cfg:       cfg,
		version:   version,
		commit:    commit,
		buildDate: buildDate,
	}
	s.router = s.buildRouter()
	return s
}

// Router returns the underlying chi.Router so it can be used by http.Server.
func (s *Server) Router() chi.Router {
	return s.router
}

// buildRouter assembles the chi router with the full middleware stack and
// mounts all route groups.
func (s *Server) buildRouter() chi.Router {
	r := chi.NewRouter()

	// -------------------------------------------------------------------
	// Standard middleware stack (order matters — outermost first).
	// See AGENTS.md "Middleware Stack" section for rationale.
	// -------------------------------------------------------------------

	// 1. OpenTelemetry tracing — wraps every request in a span.
	r.Use(otel.HTTPTracing())

	// 2. OpenTelemetry metrics — records request count, latency, size.
	r.Use(otel.HTTPMetrics("__SERVICE_FULL__"))

	// 3. Request ID — generates or propagates X-Request-ID.
	r.Use(httputil.RequestID)

	// 4. Structured request logger — logs every request with zerolog.
	r.Use(httputil.RequestLogger(log.Logger))

	// 5. Recoverer — catches panics and returns 500 with RFC 9457 body.
	r.Use(httputil.Recoverer)

	// 6. Secure headers — sets X-Content-Type-Options, X-Frame-Options, etc.
	r.Use(httputil.SecureHeaders)

	// 7. Request body limit — 1 MB default; bulk endpoints override per-route.
	r.Use(httputil.BodyLimit(1 << 20))

	// 8. Content-Type enforcement — requires application/json on mutations.
	r.Use(httputil.ContentType)

	// 9. API version header — sets API-Version on every response.
	r.Use(httputil.APIVersion("__API_VERSION__"))

	// 10. Cache-Control — no-cache for GET/HEAD, no-store for mutations.
	r.Use(httputil.CacheControl)

	// 11. ETag — computes weak ETags for GET responses.
	r.Use(httputil.ETag)

	// -------------------------------------------------------------------
	// Public routes (no authentication required).
	// -------------------------------------------------------------------
	r.Group(func(r chi.Router) {
		// Health / readiness probes for Kubernetes.
		r.Method(http.MethodGet, "/health", httputil.HealthHandler())
		r.Method(http.MethodGet, "/readiness", httputil.ReadinessHandler(func() error {
			return s.store.Ping(nil) //nolint:staticcheck // nil context is acceptable for probes
		}))

		// Build version info.
		r.Method(http.MethodGet, "/version", httputil.VersionHandler(s.version, s.commit, s.buildDate))

		// Prometheus metrics endpoint.
		if s.cfg.MetricsEnabled {
			r.Method(http.MethodGet, "/metrics", otel.PrometheusHandler())
		}

		// OpenAPI / Swagger UI.
		// TEMPLATE: Replace openapiSpec with your embedded spec bytes.
		// Example: //go:embed api/openapi.yaml
		//          var openapiSpec []byte
		r.Method(http.MethodGet, "/api/docs", httputil.SwaggerHandler())
		r.Method(http.MethodGet, "/api/openapi.yaml", httputil.OpenAPIHandler(nil)) // TEMPLATE: pass openapiSpec
	})

	// -------------------------------------------------------------------
	// Authenticated API routes.
	// -------------------------------------------------------------------
	r.Group(func(r chi.Router) {
		// JWT validation — verifies the Bearer token using JWKS.
		r.Use(auth.JWTMiddleware(auth.MiddlewareConfig{
			JWKSUrl:       s.cfg.JWKSUrl,
			InternalToken: s.cfg.InternalToken,
			DevMode:       s.cfg.DevMode,
		}))

		// TEMPLATE: Mount your service's API routes under the API prefix.
		// Replace __API_PREFIX__ with your service's path, e.g. "/hsm/v2".
		r.Route("__API_PREFIX__", func(r chi.Router) {
			// TEMPLATE: Mount resource routes. Add RequireScope per-route.
			r.Route("/__RESOURCE_PLURAL__", func(r chi.Router) {
				// List and Create — collection-level.
				r.With(auth.RequireScope("read:__RESOURCE_PLURAL__")).
					Get("/", s.handleList__RESOURCE__s)
				r.With(auth.RequireScope("write:__RESOURCE_PLURAL__")).
					Post("/", s.handleCreate__RESOURCE__)

				// Single-resource operations.
				r.Route("/{id}", func(r chi.Router) {
					r.With(auth.RequireScope("read:__RESOURCE_PLURAL__")).
						Get("/", s.handleGet__RESOURCE__)
					r.With(auth.RequireScope("write:__RESOURCE_PLURAL__")).
						Put("/", s.handleUpdate__RESOURCE__)
					r.With(auth.RequireScope("write:__RESOURCE_PLURAL__")).
						Patch("/", s.handlePatch__RESOURCE__)
					r.With(auth.RequireScope("write:__RESOURCE_PLURAL__")).
						Delete("/", s.handleDelete__RESOURCE__)
				})
			})

			// TEMPLATE: Add additional resource routes here.
		})
	})

	return r
}

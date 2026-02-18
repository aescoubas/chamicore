// TEMPLATE: Chi router and middleware stack for __SERVICE_FULL__
// Copy this file and replace all __PLACEHOLDER__ markers with your service values.
package server

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/rs/zerolog/log"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"

	// TEMPLATE: Update these imports to match your service module path.
	"git.cscs.ch/openchami/__SERVICE_FULL__/internal/config"
	"git.cscs.ch/openchami/__SERVICE_FULL__/internal/store"

	// TEMPLATE: chamicore-lib provides shared middleware.
	chamihttp "git.cscs.ch/openchami/chamicore-lib/pkg/http"
	chamimw "git.cscs.ch/openchami/chamicore-lib/pkg/middleware"
)

// Server holds the HTTP handler, the backing store, and service configuration.
type Server struct {
	store   store.Store
	cfg     config.Config
	version string
	router  chi.Router
}

// New constructs a fully-configured Server with the 12-layer middleware stack
// and all route groups mounted.
func New(st store.Store, cfg config.Config, version string) *Server {
	s := &Server{
		store:   st,
		cfg:     cfg,
		version: version,
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
	// Standard 12-layer middleware stack (order matters).
	// -------------------------------------------------------------------

	// Layer 1: OpenTelemetry tracing — wraps every request in a span.
	r.Use(func(next http.Handler) http.Handler {
		return otelhttp.NewHandler(next, "__SERVICE_FULL__",
			otelhttp.WithMessageEvents(otelhttp.ReadEvents, otelhttp.WriteEvents),
		)
	})

	// Layer 2: OpenTelemetry metrics — records request count, latency, size.
	// TEMPLATE: chamimw.OTelMetrics is a chamicore-lib middleware that records
	// standard HTTP server metrics using the OTel meter provider.
	r.Use(chamimw.OTelMetrics("__SERVICE_FULL__"))

	// Layer 3: Request ID — generates or propagates X-Request-ID.
	r.Use(middleware.RequestID)

	// Layer 4: Structured request logger — logs every request with zerolog.
	r.Use(chamimw.RequestLogger(&log.Logger))

	// Layer 5: Recoverer — catches panics and returns 500 with RFC 9457 body.
	r.Use(chamimw.Recoverer())

	// Layer 6: Secure headers — sets standard security headers:
	//   X-Content-Type-Options: nosniff
	//   X-Frame-Options: DENY
	//   Strict-Transport-Security: max-age=63072000; includeSubDomains
	r.Use(chamimw.SecureHeaders())

	// Layer 7: Content-Type enforcement — only accepts application/json on
	// POST/PUT/PATCH; always responds with application/json.
	r.Use(chamimw.ContentType("application/json"))

	// Layer 8: API version header — sets X-API-Version on every response.
	r.Use(chamimw.APIVersion("__API_VERSION__"))

	// Layer 9: Cache-Control — sets Cache-Control: no-store for mutating
	// methods, configurable max-age for GET.
	r.Use(chamimw.CacheControl(0)) // 0 = no-store for all

	// Layer 10: ETag — computes weak ETags for GET responses so clients can
	// use If-None-Match for conditional requests (304 Not Modified).
	r.Use(chamimw.ETag())

	// -------------------------------------------------------------------
	// Public routes (no authentication required).
	// -------------------------------------------------------------------
	r.Group(func(r chi.Router) {
		// Health / readiness probes for Kubernetes.
		r.Get("/health", s.handleHealth)
		r.Get("/ready", s.handleReady)

		// Build version info.
		r.Get("/version", s.handleVersion)

		// Prometheus metrics endpoint.
		// TEMPLATE: chamimw.PrometheusHandler returns a standard promhttp.Handler.
		if s.cfg.MetricsEnabled {
			r.Handle("/metrics", chamimw.PrometheusHandler())
		}

		// OpenAPI / Swagger UI.
		// TEMPLATE: chamihttp.SwaggerHandler serves the embedded OpenAPI spec.
		r.Get("/swagger/*", chamihttp.SwaggerHandler("api/openapi.yaml"))
	})

	// -------------------------------------------------------------------
	// Authenticated API routes.
	// -------------------------------------------------------------------
	r.Group(func(r chi.Router) {
		// Layer 11: JWT validation — verifies the Bearer token using JWKS.
		if s.cfg.JWKSUrl != "" {
			r.Use(chamimw.JWTMiddleware(s.cfg.JWKSUrl))
		}

		// TEMPLATE: Mount your service's API routes under the API prefix.
		// Replace __API_PREFIX__ with your service's path, e.g. "/hsm/v2".
		r.Route("__API_PREFIX__", func(r chi.Router) {
			// TEMPLATE: Mount resource routes. Add Layer 12 (RequireScope)
			// per-route group as needed.
			r.Route("/__RESOURCE_PLURAL__", func(r chi.Router) {
				// List and Create — collection-level.
				r.With(chamimw.RequireScope("__SERVICE__:__RESOURCE_PLURAL__:read")).
					Get("/", s.handleList__RESOURCE__s)
				r.With(chamimw.RequireScope("__SERVICE__:__RESOURCE_PLURAL__:write")).
					Post("/", s.handleCreate__RESOURCE__)

				// Single-resource operations.
				r.Route("/{id}", func(r chi.Router) {
					r.With(chamimw.RequireScope("__SERVICE__:__RESOURCE_PLURAL__:read")).
						Get("/", s.handleGet__RESOURCE__)
					r.With(chamimw.RequireScope("__SERVICE__:__RESOURCE_PLURAL__:write")).
						Put("/", s.handleUpdate__RESOURCE__)
					r.With(chamimw.RequireScope("__SERVICE__:__RESOURCE_PLURAL__:write")).
						Patch("/", s.handlePatch__RESOURCE__)
					r.With(chamimw.RequireScope("__SERVICE__:__RESOURCE_PLURAL__:write")).
						Delete("/", s.handleDelete__RESOURCE__)
				})
			})

			// TEMPLATE: Add additional resource routes here.
		})
	})

	return r
}

// ---------------------------------------------------------------------------
// Infrastructure handlers
// ---------------------------------------------------------------------------

// handleHealth is the liveness probe — returns 200 if the process is running.
func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	chamihttp.RespondJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// handleReady is the readiness probe — returns 200 if the database is reachable.
func (s *Server) handleReady(w http.ResponseWriter, r *http.Request) {
	if err := s.store.Ping(r.Context()); err != nil {
		chamihttp.RespondProblem(w, http.StatusServiceUnavailable, "Service Unavailable",
			"database is not reachable", r.URL.Path)
		return
	}
	chamihttp.RespondJSON(w, http.StatusOK, map[string]string{"status": "ready"})
}

// handleVersion returns build version information.
func (s *Server) handleVersion(w http.ResponseWriter, r *http.Request) {
	chamihttp.RespondJSON(w, http.StatusOK, map[string]string{
		"service": "__SERVICE_FULL__",
		"version": s.version,
	})
}

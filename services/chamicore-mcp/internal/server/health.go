package server

import (
	"net/http"

	"github.com/go-chi/chi/v5"

	"git.cscs.ch/openchami/chamicore-lib/httputil"
	"git.cscs.ch/openchami/chamicore-lib/otel"
)

func registerHealthRoutes(r chi.Router, version, commit, buildDate string, metricsEnabled bool) {
	r.Method(http.MethodGet, "/health", httputil.HealthHandler())
	r.Method(http.MethodGet, "/readiness", httputil.ReadinessHandler(func() error {
		return nil
	}))
	r.Method(http.MethodGet, "/version", httputil.VersionHandler(version, commit, buildDate))
	if metricsEnabled {
		r.Method(http.MethodGet, "/metrics", otel.PrometheusHandler())
	}
}

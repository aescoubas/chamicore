package server

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/rs/zerolog"

	"git.cscs.ch/openchami/chamicore-lib/httputil"
	"git.cscs.ch/openchami/chamicore-lib/otel"
	"git.cscs.ch/openchami/chamicore-mcp/internal/config"
)

// HTTPServer wraps MCP HTTP routing state.
type HTTPServer struct {
	cfg      config.Config
	version  string
	commit   string
	build    string
	contract []byte
	registry *ToolRegistry
	policy   ToolAuthorizer
	authn    SessionAuthenticator
	caller   ToolCaller
	logger   zerolog.Logger
}

// NewHTTPServer creates an HTTP transport server with health and MCP routes.
func NewHTTPServer(
	cfg config.Config,
	version, commit, buildDate string,
	contract []byte,
	registry *ToolRegistry,
	policy ToolAuthorizer,
	authn SessionAuthenticator,
	caller ToolCaller,
	logger zerolog.Logger,
) *HTTPServer {
	return &HTTPServer{
		cfg:      cfg,
		version:  version,
		commit:   commit,
		build:    buildDate,
		contract: contract,
		registry: registry,
		policy:   policy,
		authn:    authn,
		caller:   caller,
		logger:   logger,
	}
}

// Router builds the MCP HTTP router.
func (s *HTTPServer) Router() chi.Router {
	r := chi.NewRouter()

	r.Use(otel.HTTPTracing())
	r.Use(otel.HTTPMetrics("chamicore-mcp"))
	r.Use(httputil.RequestID)
	r.Use(httputil.RequestLogger(s.logger))
	r.Use(httputil.Recoverer)
	r.Use(httputil.SecureHeaders)
	r.Use(httputil.BodyLimit(1 << 20))
	r.Use(httputil.ContentType)
	r.Use(httputil.APIVersion("mcp/v1"))
	r.Use(httputil.CacheControl)

	registerHealthRoutes(r, s.version, s.commit, s.build, s.cfg.MetricsEnabled)
	registerMCPHTTPRoutes(r, s.registry, s.policy, s.authn, s.caller, s.version, s.logger)

	r.Get("/api/tools.yaml", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/yaml")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(s.contract)
	})

	return r
}

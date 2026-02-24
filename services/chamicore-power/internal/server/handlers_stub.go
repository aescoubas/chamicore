package server

import (
	"net/http"

	"git.cscs.ch/openchami/chamicore-lib/auth"
	"git.cscs.ch/openchami/chamicore-lib/httputil"
)

func (s *Server) handleListTransitions(w http.ResponseWriter, r *http.Request) {
	httputil.RespondProblem(w, r, http.StatusNotImplemented, "transition listing is not implemented yet")
}

func (s *Server) handleCreateTransition(w http.ResponseWriter, r *http.Request) {
	httputil.RespondProblem(w, r, http.StatusNotImplemented, "transition creation is not implemented yet")
}

func (s *Server) handleGetTransition(w http.ResponseWriter, r *http.Request) {
	httputil.RespondProblem(w, r, http.StatusNotImplemented, "transition status is not implemented yet")
}

func (s *Server) handleDeleteTransition(w http.ResponseWriter, r *http.Request) {
	httputil.RespondProblem(w, r, http.StatusNotImplemented, "transition abort is not implemented yet")
}

func (s *Server) handleGetPowerStatus(w http.ResponseWriter, r *http.Request) {
	httputil.RespondProblem(w, r, http.StatusNotImplemented, "power status is not implemented yet")
}

func (s *Server) handleActionOn(w http.ResponseWriter, r *http.Request) {
	httputil.RespondProblem(w, r, http.StatusNotImplemented, "power-on action is not implemented yet")
}

func (s *Server) handleActionOff(w http.ResponseWriter, r *http.Request) {
	httputil.RespondProblem(w, r, http.StatusNotImplemented, "power-off action is not implemented yet")
}

func (s *Server) handleActionReboot(w http.ResponseWriter, r *http.Request) {
	httputil.RespondProblem(w, r, http.StatusNotImplemented, "reboot action is not implemented yet")
}

func (s *Server) handleActionReset(w http.ResponseWriter, r *http.Request) {
	httputil.RespondProblem(w, r, http.StatusNotImplemented, "reset action is not implemented yet")
}

func (s *Server) handleAdminSyncMappings(w http.ResponseWriter, r *http.Request) {
	httputil.RespondProblem(w, r, http.StatusNotImplemented, "mapping sync is not implemented yet")
}

func requireAnyScope(scopes ...string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			claims, ok := auth.ClaimsFromContext(r.Context())
			if !ok {
				httputil.RespondProblem(w, r, http.StatusForbidden, "no claims in context")
				return
			}

			for _, scope := range scopes {
				if claims.HasScope(scope) {
					next.ServeHTTP(w, r)
					return
				}
			}
			httputil.RespondProblemf(w, r, http.StatusForbidden, "missing required scope: %s", scopes[0])
		})
	}
}

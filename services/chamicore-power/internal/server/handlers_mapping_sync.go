package server

import (
	"context"
	"net/http"
	"time"

	"github.com/rs/zerolog/log"

	"git.cscs.ch/openchami/chamicore-lib/httputil"
)

type mappingSyncTriggerResponse struct {
	Status string `json:"status"`
}

func (s *Server) handleAdminSyncMappings(w http.ResponseWriter, r *http.Request) {
	if s.mappingSync == nil {
		httputil.RespondProblem(w, r, http.StatusServiceUnavailable, "mapping sync subsystem is not configured")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 60*time.Second)
	defer cancel()

	if err := s.mappingSync.Trigger(ctx); err != nil {
		log.Ctx(r.Context()).Error().Err(err).Msg("failed to trigger mapping sync")
		httputil.RespondProblem(w, r, http.StatusInternalServerError, "failed to trigger mapping sync")
		return
	}

	httputil.RespondJSON(w, http.StatusAccepted, httputil.Resource[mappingSyncTriggerResponse]{
		Kind:       "MappingSyncTrigger",
		APIVersion: "power/v1",
		Metadata: httputil.Metadata{
			ID: "power-mapping-sync",
		},
		Spec: mappingSyncTriggerResponse{
			Status: "accepted",
		},
	})
}

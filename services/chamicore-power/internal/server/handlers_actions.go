package server

import (
	"net/http"
	"strings"

	"git.cscs.ch/openchami/chamicore-lib/httputil"
)

type actionRequest struct {
	RequestID string   `json:"requestID,omitempty"`
	Nodes     []string `json:"nodes,omitempty"`
	Groups    []string `json:"groups,omitempty"`
	DryRun    bool     `json:"dryRun,omitempty"`
}

type resetActionRequest struct {
	RequestID string   `json:"requestID,omitempty"`
	Operation string   `json:"operation"`
	Nodes     []string `json:"nodes,omitempty"`
	Groups    []string `json:"groups,omitempty"`
	DryRun    bool     `json:"dryRun,omitempty"`
}

func (s *Server) handleActionOn(w http.ResponseWriter, r *http.Request) {
	var req actionRequest
	if err := httputil.DecodeJSON(r, &req); err != nil {
		httputil.RespondProblemf(w, r, http.StatusBadRequest, "invalid request body: %v", err)
		return
	}

	s.startTransition(w, r, transitionRequest{
		RequestID: strings.TrimSpace(req.RequestID),
		Operation: "On",
		Nodes:     req.Nodes,
		Groups:    req.Groups,
		DryRun:    req.DryRun,
	})
}

func (s *Server) handleActionOff(w http.ResponseWriter, r *http.Request) {
	var req actionRequest
	if err := httputil.DecodeJSON(r, &req); err != nil {
		httputil.RespondProblemf(w, r, http.StatusBadRequest, "invalid request body: %v", err)
		return
	}

	s.startTransition(w, r, transitionRequest{
		RequestID: strings.TrimSpace(req.RequestID),
		Operation: "ForceOff",
		Nodes:     req.Nodes,
		Groups:    req.Groups,
		DryRun:    req.DryRun,
	})
}

func (s *Server) handleActionReboot(w http.ResponseWriter, r *http.Request) {
	var req actionRequest
	if err := httputil.DecodeJSON(r, &req); err != nil {
		httputil.RespondProblemf(w, r, http.StatusBadRequest, "invalid request body: %v", err)
		return
	}

	s.startTransition(w, r, transitionRequest{
		RequestID: strings.TrimSpace(req.RequestID),
		Operation: "ForceRestart",
		Nodes:     req.Nodes,
		Groups:    req.Groups,
		DryRun:    req.DryRun,
	})
}

func (s *Server) handleActionReset(w http.ResponseWriter, r *http.Request) {
	var req resetActionRequest
	if err := httputil.DecodeJSON(r, &req); err != nil {
		httputil.RespondProblemf(w, r, http.StatusBadRequest, "invalid request body: %v", err)
		return
	}

	s.startTransition(w, r, transitionRequest{
		RequestID: strings.TrimSpace(req.RequestID),
		Operation: strings.TrimSpace(req.Operation),
		Nodes:     req.Nodes,
		Groups:    req.Groups,
		DryRun:    req.DryRun,
	})
}

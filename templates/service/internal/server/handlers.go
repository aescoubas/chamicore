// TEMPLATE: CRUD handlers for __RESOURCE__ in __SERVICE_FULL__
// Copy this file and replace all __PLACEHOLDER__ markers with your service values.
//
// Every handler follows these conventions:
//   - Uses RFC 9457 Problem Details for errors (httputil.RespondProblem).
//   - Returns resources in the envelope pattern (kind, apiVersion, metadata, spec).
//   - Passes context to the store for tracing and cancellation.
//   - Logs structured fields with zerolog.
package server

import (
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/rs/zerolog/log"

	// TEMPLATE: Update these imports to match your service module path.
	"git.cscs.ch/openchami/__SERVICE_FULL__/internal/model"
	"git.cscs.ch/openchami/__SERVICE_FULL__/internal/store"
	"git.cscs.ch/openchami/__SERVICE_FULL__/pkg/types"

	// Shared library packages.
	"git.cscs.ch/openchami/chamicore-lib/httputil"
)

// ---------------------------------------------------------------------------
// Constants
// ---------------------------------------------------------------------------

const (
	defaultPageLimit = 50
	maxPageLimit     = 200

	// TEMPLATE: Adjust these to match your resource and API version.
	resourceKind       = "__RESOURCE__"
	resourceListKind   = "__RESOURCE__List"
	resourceAPIVersion = "__API_VERSION__"
)

// ---------------------------------------------------------------------------
// List — GET __API_PREFIX__/__RESOURCE_PLURAL__
// ---------------------------------------------------------------------------

// handleList__RESOURCE__s returns a paginated list of __RESOURCE_LOWER__ resources.
//
// Query parameters:
//
//	limit  — max items to return (default 50, max 200)
//	offset — number of items to skip (default 0)
//
// Response: 200 with ResourceList envelope.
func (s *Server) handleList__RESOURCE__s(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	logger := log.Ctx(ctx).With().Str("handler", "List__RESOURCE__s").Logger()

	// Parse pagination parameters.
	limit, offset, err := parsePagination(r)
	if err != nil {
		httputil.RespondProblem(w, r, http.StatusBadRequest, err.Error())
		return
	}

	// TEMPLATE: Parse any additional filter query parameters here.
	// Example:
	// typeFilter := r.URL.Query().Get("type")

	items, total, err := s.store.List__RESOURCE__s(ctx, store.ListOptions{
		Limit:  limit,
		Offset: offset,
		// TEMPLATE: Pass filter parameters to the store.
	})
	if err != nil {
		logger.Error().Err(err).Msg("failed to list __RESOURCE_LOWER__s")
		httputil.RespondProblem(w, r, http.StatusInternalServerError, "an unexpected error occurred")
		return
	}

	// Map internal models to envelope items.
	envelopeItems := make([]httputil.Resource[types.__RESOURCE__], len(items))
	for i, item := range items {
		envelopeItems[i] = httputil.Resource[types.__RESOURCE__]{
			Kind:       resourceKind,
			APIVersion: resourceAPIVersion,
			Metadata: httputil.Metadata{
				ID:        item.ID,
				CreatedAt: item.CreatedAt,
				UpdatedAt: item.UpdatedAt,
			},
			Spec: toPublic__RESOURCE__(item),
		}
	}

	resp := httputil.ResourceList[types.__RESOURCE__]{
		Kind:       resourceListKind,
		APIVersion: resourceAPIVersion,
		Metadata: httputil.ListMetadata{
			Total:  total,
			Limit:  limit,
			Offset: offset,
		},
		Items: envelopeItems,
	}

	httputil.RespondJSON(w, http.StatusOK, resp)
}

// ---------------------------------------------------------------------------
// Get — GET __API_PREFIX__/__RESOURCE_PLURAL__/{id}
// ---------------------------------------------------------------------------

// handleGet__RESOURCE__ returns a single __RESOURCE_LOWER__ by ID with ETag support.
//
// Response: 200 with Resource envelope, or 304 if ETag matches.
// Errors:  404 if not found.
func (s *Server) handleGet__RESOURCE__(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	logger := log.Ctx(ctx).With().Str("handler", "Get__RESOURCE__").Logger()

	id := chi.URLParam(r, "id")
	if id == "" {
		httputil.RespondProblem(w, r, http.StatusBadRequest, "missing resource id in URL path")
		return
	}

	item, err := s.store.Get__RESOURCE__(ctx, id)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			httputil.RespondProblemf(w, r, http.StatusNotFound, "__RESOURCE_LOWER__ %q not found", id)
			return
		}
		logger.Error().Err(err).Str("id", id).Msg("failed to get __RESOURCE_LOWER__")
		httputil.RespondProblem(w, r, http.StatusInternalServerError, "an unexpected error occurred")
		return
	}

	resp := httputil.Resource[types.__RESOURCE__]{
		Kind:       resourceKind,
		APIVersion: resourceAPIVersion,
		Metadata: httputil.Metadata{
			ID:        item.ID,
			ETag:      computeETag(item.ID, item.UpdatedAt),
			CreatedAt: item.CreatedAt,
			UpdatedAt: item.UpdatedAt,
		},
		Spec: toPublic__RESOURCE__(item),
	}

	// Set ETag header for conditional request support.
	etag := computeETag(item.ID, item.UpdatedAt)
	w.Header().Set("ETag", etag)

	// If the client sent If-None-Match and it matches, return 304.
	if match := r.Header.Get("If-None-Match"); match != "" {
		if strings.TrimSpace(match) == etag {
			w.WriteHeader(http.StatusNotModified)
			return
		}
	}

	httputil.RespondJSON(w, http.StatusOK, resp)
}

// ---------------------------------------------------------------------------
// Create — POST __API_PREFIX__/__RESOURCE_PLURAL__
// ---------------------------------------------------------------------------

// handleCreate__RESOURCE__ creates a new __RESOURCE_LOWER__.
//
// Request body: JSON-encoded Create__RESOURCE__Request (unknown fields rejected).
// Response: 201 with Resource envelope + Location header.
// Errors:  400 for validation errors, 409 for conflicts.
func (s *Server) handleCreate__RESOURCE__(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	logger := log.Ctx(ctx).With().Str("handler", "Create__RESOURCE__").Logger()

	// Decode request body with strict unknown field checking.
	var req types.Create__RESOURCE__Request
	if err := httputil.DecodeJSON(r, &req); err != nil {
		httputil.RespondProblemf(w, r, http.StatusBadRequest, "invalid request body: %v", err)
		return
	}

	// Validate the request.
	if errs := validateCreate__RESOURCE__Request(req); len(errs) > 0 {
		httputil.RespondValidationProblem(w, r, errs)
		return
	}

	// Map public request to internal model.
	m := model.__RESOURCE__{
		// TEMPLATE: Map fields from the request to your internal model.
		Name:        req.Name,
		Description: req.Description,
	}

	created, err := s.store.Create__RESOURCE__(ctx, m)
	if err != nil {
		if errors.Is(err, store.ErrConflict) {
			httputil.RespondProblem(w, r, http.StatusConflict,
				"a __RESOURCE_LOWER__ with this identifier already exists")
			return
		}
		logger.Error().Err(err).Msg("failed to create __RESOURCE_LOWER__")
		httputil.RespondProblem(w, r, http.StatusInternalServerError, "an unexpected error occurred")
		return
	}

	resp := httputil.Resource[types.__RESOURCE__]{
		Kind:       resourceKind,
		APIVersion: resourceAPIVersion,
		Metadata: httputil.Metadata{
			ID:        created.ID,
			ETag:      computeETag(created.ID, created.UpdatedAt),
			CreatedAt: created.CreatedAt,
			UpdatedAt: created.UpdatedAt,
		},
		Spec: toPublic__RESOURCE__(created),
	}

	location := fmt.Sprintf("__API_PREFIX__/__RESOURCE_PLURAL__/%s", created.ID)
	w.Header().Set("Location", location)
	w.Header().Set("ETag", computeETag(created.ID, created.UpdatedAt))

	httputil.RespondJSON(w, http.StatusCreated, resp)
}

// ---------------------------------------------------------------------------
// Update — PUT __API_PREFIX__/__RESOURCE_PLURAL__/{id}
// ---------------------------------------------------------------------------

// handleUpdate__RESOURCE__ performs a full replacement of a __RESOURCE_LOWER__.
//
// Requires If-Match header with the current ETag for optimistic concurrency.
// Response: 200 with updated Resource envelope.
// Errors:  400, 404, 412 Precondition Failed if ETag mismatch.
func (s *Server) handleUpdate__RESOURCE__(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	logger := log.Ctx(ctx).With().Str("handler", "Update__RESOURCE__").Logger()

	id := chi.URLParam(r, "id")
	if id == "" {
		httputil.RespondProblem(w, r, http.StatusBadRequest, "missing resource id in URL path")
		return
	}

	// Require If-Match for optimistic concurrency control.
	ifMatch := r.Header.Get("If-Match")
	if ifMatch == "" {
		httputil.RespondProblem(w, r, http.StatusPreconditionRequired,
			"If-Match header with ETag is required for PUT operations")
		return
	}

	// Fetch current resource to verify ETag.
	current, err := s.store.Get__RESOURCE__(ctx, id)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			httputil.RespondProblemf(w, r, http.StatusNotFound, "__RESOURCE_LOWER__ %q not found", id)
			return
		}
		logger.Error().Err(err).Str("id", id).Msg("failed to get __RESOURCE_LOWER__ for update")
		httputil.RespondProblem(w, r, http.StatusInternalServerError, "an unexpected error occurred")
		return
	}

	// Check ETag.
	currentETag := computeETag(current.ID, current.UpdatedAt)
	if strings.TrimSpace(ifMatch) != currentETag {
		httputil.RespondProblem(w, r, http.StatusPreconditionFailed,
			"the resource has been modified since you last retrieved it; re-fetch and retry")
		return
	}

	// Decode request body.
	var req types.Update__RESOURCE__Request
	if err := httputil.DecodeJSON(r, &req); err != nil {
		httputil.RespondProblemf(w, r, http.StatusBadRequest, "invalid request body: %v", err)
		return
	}

	// Validate.
	if errs := validateUpdate__RESOURCE__Request(req); len(errs) > 0 {
		httputil.RespondValidationProblem(w, r, errs)
		return
	}

	// Map to internal model.
	m := model.__RESOURCE__{
		ID: id,
		// TEMPLATE: Map all fields from the update request.
		Name:        req.Name,
		Description: req.Description,
	}

	updated, err := s.store.Update__RESOURCE__(ctx, m)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			httputil.RespondProblemf(w, r, http.StatusNotFound, "__RESOURCE_LOWER__ %q not found", id)
			return
		}
		logger.Error().Err(err).Str("id", id).Msg("failed to update __RESOURCE_LOWER__")
		httputil.RespondProblem(w, r, http.StatusInternalServerError, "an unexpected error occurred")
		return
	}

	resp := httputil.Resource[types.__RESOURCE__]{
		Kind:       resourceKind,
		APIVersion: resourceAPIVersion,
		Metadata: httputil.Metadata{
			ID:        updated.ID,
			ETag:      computeETag(updated.ID, updated.UpdatedAt),
			CreatedAt: updated.CreatedAt,
			UpdatedAt: updated.UpdatedAt,
		},
		Spec: toPublic__RESOURCE__(updated),
	}

	w.Header().Set("ETag", computeETag(updated.ID, updated.UpdatedAt))
	httputil.RespondJSON(w, http.StatusOK, resp)
}

// ---------------------------------------------------------------------------
// Patch — PATCH __API_PREFIX__/__RESOURCE_PLURAL__/{id}
// ---------------------------------------------------------------------------

// handlePatch__RESOURCE__ performs a partial update of a __RESOURCE_LOWER__.
//
// Only fields present in the request body are updated; omitted fields retain
// their current values. Uses pointer fields in the request type to distinguish
// "not provided" (nil) from "set to zero value".
//
// Response: 200 with updated Resource envelope.
// Errors:  400, 404.
func (s *Server) handlePatch__RESOURCE__(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	logger := log.Ctx(ctx).With().Str("handler", "Patch__RESOURCE__").Logger()

	id := chi.URLParam(r, "id")
	if id == "" {
		httputil.RespondProblem(w, r, http.StatusBadRequest, "missing resource id in URL path")
		return
	}

	// Decode patch request (uses pointer fields for optionality).
	var req types.Patch__RESOURCE__Request
	if err := httputil.DecodeJSON(r, &req); err != nil {
		httputil.RespondProblemf(w, r, http.StatusBadRequest, "invalid request body: %v", err)
		return
	}

	// Fetch existing record.
	existing, err := s.store.Get__RESOURCE__(ctx, id)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			httputil.RespondProblemf(w, r, http.StatusNotFound, "__RESOURCE_LOWER__ %q not found", id)
			return
		}
		logger.Error().Err(err).Str("id", id).Msg("failed to get __RESOURCE_LOWER__ for patch")
		httputil.RespondProblem(w, r, http.StatusInternalServerError, "an unexpected error occurred")
		return
	}

	// TEMPLATE: Apply patch — only overwrite fields that were provided.
	if req.Name != nil {
		existing.Name = *req.Name
	}
	if req.Description != nil {
		existing.Description = *req.Description
	}
	// TEMPLATE: Add more patchable fields as needed.

	updated, err := s.store.Update__RESOURCE__(ctx, existing)
	if err != nil {
		logger.Error().Err(err).Str("id", id).Msg("failed to patch __RESOURCE_LOWER__")
		httputil.RespondProblem(w, r, http.StatusInternalServerError, "an unexpected error occurred")
		return
	}

	resp := httputil.Resource[types.__RESOURCE__]{
		Kind:       resourceKind,
		APIVersion: resourceAPIVersion,
		Metadata: httputil.Metadata{
			ID:        updated.ID,
			ETag:      computeETag(updated.ID, updated.UpdatedAt),
			CreatedAt: updated.CreatedAt,
			UpdatedAt: updated.UpdatedAt,
		},
		Spec: toPublic__RESOURCE__(updated),
	}

	w.Header().Set("ETag", computeETag(updated.ID, updated.UpdatedAt))
	httputil.RespondJSON(w, http.StatusOK, resp)
}

// ---------------------------------------------------------------------------
// Delete — DELETE __API_PREFIX__/__RESOURCE_PLURAL__/{id}
// ---------------------------------------------------------------------------

// handleDelete__RESOURCE__ deletes a __RESOURCE_LOWER__ by ID.
//
// Response: 204 No Content on success.
// Errors:  404 if not found.
func (s *Server) handleDelete__RESOURCE__(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	logger := log.Ctx(ctx).With().Str("handler", "Delete__RESOURCE__").Logger()

	id := chi.URLParam(r, "id")
	if id == "" {
		httputil.RespondProblem(w, r, http.StatusBadRequest, "missing resource id in URL path")
		return
	}

	if err := s.store.Delete__RESOURCE__(ctx, id); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			httputil.RespondProblemf(w, r, http.StatusNotFound, "__RESOURCE_LOWER__ %q not found", id)
			return
		}
		logger.Error().Err(err).Str("id", id).Msg("failed to delete __RESOURCE_LOWER__")
		httputil.RespondProblem(w, r, http.StatusInternalServerError, "an unexpected error occurred")
		return
	}

	httputil.RespondNoContent(w)
}

// ---------------------------------------------------------------------------
// Validation helpers
// ---------------------------------------------------------------------------

// TEMPLATE: validateCreate__RESOURCE__Request validates the creation request
// and returns a list of field-level errors. Return nil if valid.
func validateCreate__RESOURCE__Request(req types.Create__RESOURCE__Request) []httputil.ValidationError {
	var errs []httputil.ValidationError

	if strings.TrimSpace(req.Name) == "" {
		errs = append(errs, httputil.ValidationError{
			Field:   "name",
			Message: "name is required and must not be blank",
		})
	}

	// TEMPLATE: Add additional field validations.

	return errs
}

// TEMPLATE: validateUpdate__RESOURCE__Request validates the full update request.
func validateUpdate__RESOURCE__Request(req types.Update__RESOURCE__Request) []httputil.ValidationError {
	var errs []httputil.ValidationError

	if strings.TrimSpace(req.Name) == "" {
		errs = append(errs, httputil.ValidationError{
			Field:   "name",
			Message: "name is required and must not be blank",
		})
	}

	// TEMPLATE: Add additional field validations.

	return errs
}

// ---------------------------------------------------------------------------
// Mapping helpers
// ---------------------------------------------------------------------------

// toPublic__RESOURCE__ converts the internal model to the public API type.
func toPublic__RESOURCE__(m model.__RESOURCE__) types.__RESOURCE__ {
	return types.__RESOURCE__{
		// TEMPLATE: Map all fields from internal model to public type.
		Name:        m.Name,
		Description: m.Description,
	}
}

// ---------------------------------------------------------------------------
// Utility helpers
// ---------------------------------------------------------------------------

// parsePagination extracts limit and offset from query parameters with
// defaults and bounds checking.
func parsePagination(r *http.Request) (limit, offset int, err error) {
	limit = defaultPageLimit
	offset = 0

	if v := r.URL.Query().Get("limit"); v != "" {
		limit, err = strconv.Atoi(v)
		if err != nil || limit < 1 {
			return 0, 0, fmt.Errorf("limit must be a positive integer")
		}
		if limit > maxPageLimit {
			limit = maxPageLimit
		}
	}

	if v := r.URL.Query().Get("offset"); v != "" {
		offset, err = strconv.Atoi(v)
		if err != nil || offset < 0 {
			return 0, 0, fmt.Errorf("offset must be a non-negative integer")
		}
	}

	return limit, offset, nil
}

// computeETag produces a weak ETag from the resource ID and its last
// modification timestamp.
func computeETag(id string, updatedAt time.Time) string {
	return fmt.Sprintf(`W/"%s-%d"`, id, updatedAt.UnixNano())
}

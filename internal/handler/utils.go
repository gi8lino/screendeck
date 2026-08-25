package handler

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"

	"github.com/gi8lino/screendeck/internal/jellyfin"
	"github.com/gi8lino/screendeck/internal/media"
	"github.com/gi8lino/screendeck/internal/plex"
	"github.com/gi8lino/screendeck/internal/requestid"
	"github.com/gi8lino/screendeck/internal/store"
)

// validRequest validates a decoded request and returns problems keyed by JSON field path.
type validRequest interface {
	Valid(context.Context) map[string]string
}

// validationError contains all field-level problems found in a request.
type validationError struct {
	Problems map[string]string
}

// Error implements error.
func (e validationError) Error() string {
	return "request validation failed"
}

// decode reads and validates a JSON request body.
func decode(r *http.Request, target any) error {
	r.Body = http.MaxBytesReader(nil, r.Body, 1<<20)

	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()

	if err := decoder.Decode(target); err != nil {
		return fmt.Errorf("invalid request: %w", err)
	}

	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		if err == nil {
			return errors.New("invalid request: body must contain exactly one JSON value")
		}
		return fmt.Errorf("invalid request: %w", err)
	}

	return nil
}

// decodeValid decodes a request and reports all field-level validation problems.
func decodeValid(r *http.Request, target validRequest) error {
	if err := decode(r, target); err != nil {
		return err
	}
	if problems := target.Valid(r.Context()); len(problems) > 0 {
		return validationError{Problems: problems}
	}
	return nil
}

// statusForError maps application errors to their public HTTP status.
func statusForError(err error) int {
	switch {
	case errors.Is(err, errBrowserIdentityUnavailable):
		return http.StatusInternalServerError
	case errors.Is(err, store.ErrMembershipConflict), errors.Is(err, media.ErrProviderConflict):
		return http.StatusConflict
	case errors.Is(err, store.ErrForbidden):
		return http.StatusForbidden
	case errors.Is(err, store.ErrNotFound):
		return http.StatusNotFound
	case errors.Is(err, media.ErrNotConfigured), errors.Is(err, plex.ErrNotConfigured):
		return http.StatusServiceUnavailable
	case isMediaUpstreamError(err):
		return http.StatusBadGateway
	default:
		return http.StatusBadRequest
	}
}

// isMediaUpstreamError reports whether an error represents a media-provider upstream failure.
func isMediaUpstreamError(err error) bool {
	return errors.Is(err, plex.ErrServerContact) ||
		errors.Is(err, plex.ErrServerResponse) ||
		errors.Is(err, plex.ErrServerDecode) ||
		errors.Is(err, plex.ErrCloudUnavailable) ||
		errors.Is(err, plex.ErrCloudResponse) ||
		errors.Is(err, plex.ErrCloudDecode) ||
		errors.Is(err, plex.ErrServerVerification) ||
		errors.Is(err, plex.ErrAuthenticationRefresh) ||
		errors.Is(err, jellyfin.ErrServerContact) ||
		errors.Is(err, jellyfin.ErrServerResponse) ||
		errors.Is(err, jellyfin.ErrServerDecode)
}

// publicErrorMessage returns a safe, actionable message for an API error.
func publicErrorMessage(err error) string {
	if isMediaUpstreamError(err) {
		return "media server unavailable; check that Plex or Jellyfin is running and reachable"
	}
	return err.Error()
}

// fail logs and writes an API error response.
func (a *API) fail(r *http.Request, w http.ResponseWriter, err error) {
	status := statusForError(err)
	requestid.WithLogger(a.Logger, r.Context()).Error("API request failed",
		"event", "api_request_failed",
		"method", r.Method,
		"path", r.URL.Path,
		"status", status,
		"error", err,
	)
	payload := map[string]any{"error": publicErrorMessage(err)}
	var validation validationError
	if errors.As(err, &validation) {
		payload["problems"] = validation.Problems
	}
	a.respond(w, status, payload)
}

// respond writes a JSON API response.
func (a *API) respond(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(value); err != nil {
		requestid.WithLogger(a.Logger, nil).Error("encode response",
			"event", "encode_response_failed",
			"error", err,
		)
	}
}

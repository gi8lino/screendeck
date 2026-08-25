package handler

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/gi8lino/screendeck/internal/jellyfin"
	"github.com/gi8lino/screendeck/internal/media"
	"github.com/gi8lino/screendeck/internal/plex"
	"github.com/gi8lino/screendeck/internal/requestid"
	"github.com/gi8lino/screendeck/internal/store"
)

// participantToken extracts a participant token from a request.
func participantToken(r *http.Request) string {
	return strings.TrimSpace(r.Header.Get("X-Participant-Token"))
}

// setupToken extracts a Plex setup token from a request.
func setupToken(r *http.Request) string {
	return strings.TrimSpace(r.Header.Get("X-Setup-Token"))
}

// decode reads and validates a JSON request body.
func decode(r *http.Request, target any) error {
	r.Body = http.MaxBytesReader(nil, r.Body, 1<<20)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return fmt.Errorf("invalid request: %w", err)
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
	a.respond(w, status, map[string]string{"error": err.Error()})
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

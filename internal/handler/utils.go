package handler

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/gi8lino/screendeck/internal/logging"
	"github.com/gi8lino/screendeck/internal/plex"
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
	case errors.Is(err, store.ErrNotFound):
		return http.StatusNotFound
	case errors.Is(err, plex.ErrNotConfigured):
		return http.StatusServiceUnavailable
	case isPlexUpstreamError(err):
		return http.StatusBadGateway
	default:
		return http.StatusBadRequest
	}
}

// isPlexUpstreamError reports whether an error represents a Plex upstream failure.
func isPlexUpstreamError(err error) bool {
	return errors.Is(err, plex.ErrServerContact) ||
		errors.Is(err, plex.ErrServerResponse) ||
		errors.Is(err, plex.ErrServerDecode) ||
		errors.Is(err, plex.ErrCloudUnavailable) ||
		errors.Is(err, plex.ErrCloudResponse) ||
		errors.Is(err, plex.ErrCloudDecode) ||
		errors.Is(err, plex.ErrServerVerification) ||
		errors.Is(err, plex.ErrAuthenticationRefresh)
}

// fail logs and writes an API error response.
func (a *API) fail(r *http.Request, w http.ResponseWriter, err error) {
	status := statusForError(err)
	logging.WithRequestIDLogger(a.Logger, r.Context()).Error("API request failed",
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
		logging.WithRequestIDLogger(a.Logger, nil).Error("encode response",
			"event", "encode_response_failed",
			"error", err,
		)
	}
}

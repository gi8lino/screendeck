package handler

import (
	"errors"
	"log/slog"
	"net/http"

	"github.com/gi8lino/screendeck/internal/jellyfin"
	"github.com/gi8lino/screendeck/internal/media"
	"github.com/gi8lino/screendeck/internal/plex"
	"github.com/gi8lino/screendeck/internal/requestid"
	"github.com/gi8lino/screendeck/internal/store"
)

// statusForError maps application errors to their public HTTP status.
func statusForError(err error) int {
	switch {
	case errors.Is(err, errBrowserIdentityUnavailable):
		return http.StatusInternalServerError
	case errors.Is(err, store.ErrMembershipConflict),
		errors.Is(err, store.ErrRoomLocked),
		errors.Is(err, media.ErrProviderConflict):
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
func fail(logger *slog.Logger, r *http.Request, w http.ResponseWriter, err error) {
	status := statusForError(err)
	requestid.WithLogger(logger, r.Context()).Error("API request failed",
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
	respond(logger, w, status, payload)
}

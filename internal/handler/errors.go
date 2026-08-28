package handler

import (
	"errors"
	"log/slog"
	"net/http"

	"github.com/gi8lino/screendeck/internal/jellyfin"
	"github.com/gi8lino/screendeck/internal/media"
	"github.com/gi8lino/screendeck/internal/plex"
	"github.com/gi8lino/screendeck/internal/requestid"
	"github.com/gi8lino/screendeck/internal/room"
)

// statusForError maps application errors to their public HTTP status.
func statusForError(err error) int {
	switch {
	case isValidationError(err):
		return http.StatusBadRequest
	case errors.Is(err, errBrowserIdentityUnavailable):
		return http.StatusInternalServerError
	case errors.Is(err, room.ErrMembershipConflict),
		errors.Is(err, room.ErrLocked),
		errors.Is(err, media.ErrProviderConflict):
		return http.StatusConflict
	case errors.Is(err, room.ErrForbidden):
		return http.StatusForbidden
	case errors.Is(err, room.ErrNotFound):
		return http.StatusNotFound
	case errors.Is(err, room.ErrInvalidInput), isClientMediaError(err):
		return http.StatusBadRequest
	case errors.Is(err, media.ErrNotConfigured), errors.Is(err, plex.ErrNotConfigured):
		return http.StatusServiceUnavailable
	case isMediaUpstreamError(err):
		return http.StatusBadGateway
	default:
		return http.StatusInternalServerError
	}
}

func isValidationError(err error) bool {
	var validation validationError
	return errors.As(err, &validation)
}

// isClientMediaError reports whether media setup rejected client-supplied state.
func isClientMediaError(err error) bool {
	return errors.Is(err, jellyfin.ErrInvalidServerURL) ||
		errors.Is(err, jellyfin.ErrInvalidClientConfig) ||
		errors.Is(err, jellyfin.ErrInvalidLibrary) ||
		errors.Is(err, jellyfin.ErrInvalidPosterReference) ||
		errors.Is(err, jellyfin.ErrAuthenticationFailed) ||
		errors.Is(err, plex.ErrAlreadyConfigured) ||
		errors.Is(err, plex.ErrInvalidAuthMethod) ||
		errors.Is(err, plex.ErrExperimentalAuthDisabled) ||
		errors.Is(err, plex.ErrAuthorizationExpired) ||
		errors.Is(err, plex.ErrAuthorizationIncomplete) ||
		errors.Is(err, plex.ErrServerUnavailable) ||
		errors.Is(err, plex.ErrNoUsableConnection) ||
		errors.Is(err, plex.ErrInvalidClientConfig) ||
		errors.Is(err, plex.ErrInvalidClientID) ||
		errors.Is(err, plex.ErrInvalidServerURL) ||
		errors.Is(err, plex.ErrInvalidLibrary) ||
		errors.Is(err, plex.ErrInvalidPosterPath)
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
	if statusForError(err) == http.StatusInternalServerError {
		return "internal server error"
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

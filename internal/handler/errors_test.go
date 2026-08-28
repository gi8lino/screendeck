package handler

import (
	"errors"
	"fmt"
	"net/http"
	"testing"

	"github.com/gi8lino/screendeck/internal/jellyfin"
	"github.com/gi8lino/screendeck/internal/media"
	"github.com/gi8lino/screendeck/internal/plex"
	"github.com/gi8lino/screendeck/internal/room"
	"github.com/stretchr/testify/assert"
)

// TestStatusForError verifies application errors map to stable public HTTP statuses.
func TestStatusForError(t *testing.T) {
	t.Run("bad request", func(t *testing.T) {
		assert.Equal(t, http.StatusBadRequest, statusForError(room.InvalidInput("invalid input")))
		assert.Equal(t, http.StatusBadRequest, statusForError(jellyfin.ErrAuthenticationFailed))
	})

	t.Run("unexpected server error", func(t *testing.T) {
		assert.Equal(t, http.StatusInternalServerError, statusForError(errors.New("database unavailable")))
	})

	t.Run("identity unavailable", func(t *testing.T) {
		assert.Equal(t, http.StatusInternalServerError, statusForError(errBrowserIdentityUnavailable))
	})

	t.Run("conflict", func(t *testing.T) {
		assert.Equal(t, http.StatusConflict, statusForError(room.ErrMembershipConflict))
		assert.Equal(t, http.StatusConflict, statusForError(room.ErrLocked))
	})

	t.Run("forbidden", func(t *testing.T) {
		assert.Equal(t, http.StatusForbidden, statusForError(room.ErrForbidden))
	})

	t.Run("not found", func(t *testing.T) {
		assert.Equal(t, http.StatusNotFound, statusForError(room.ErrNotFound))
	})

	t.Run("not configured", func(t *testing.T) {
		assert.Equal(t, http.StatusServiceUnavailable, statusForError(media.ErrNotConfigured))
	})

	t.Run("provider conflict", func(t *testing.T) {
		assert.Equal(t, http.StatusConflict, statusForError(media.ErrProviderConflict))
	})

	t.Run("upstream", func(t *testing.T) {
		assert.Equal(t, http.StatusBadGateway, statusForError(plex.ErrServerResponse))
	})
}

// TestIsMediaUpstreamError verifies provider transport and response error classification.
func TestIsMediaUpstreamError(t *testing.T) {
	t.Run("upstream error", func(t *testing.T) {
		assert.True(t, isMediaUpstreamError(plex.ErrCloudUnavailable))
	})

	t.Run("wrapped Plex upstream error", func(t *testing.T) {
		assert.True(t, isMediaUpstreamError(errors.Join(errors.New("context"), plex.ErrAuthenticationRefresh)))
	})

	t.Run("Jellyfin upstream error", func(t *testing.T) {
		assert.True(t, isMediaUpstreamError(jellyfin.ErrServerResponse))
	})

	t.Run("application error", func(t *testing.T) {
		assert.False(t, isMediaUpstreamError(room.ErrNotFound))
	})
}

// TestPublicErrorMessage verifies upstream details are replaced with actionable public copy.
func TestPublicErrorMessage(t *testing.T) {
	t.Run("upstream error", func(t *testing.T) {
		err := fmt.Errorf("GET http://plex:32400: %w", plex.ErrServerContact)
		assert.Equal(
			t,
			"media server unavailable; check that Plex or Jellyfin is running and reachable",
			publicErrorMessage(err),
		)
	})

	t.Run("public application error", func(t *testing.T) {
		err := room.InvalidInput("room is full")
		assert.Equal(t, "room is full", publicErrorMessage(err))
	})

	t.Run("internal error", func(t *testing.T) {
		err := errors.New("database path /secret failed")
		assert.Equal(t, "internal server error", publicErrorMessage(err))
	})
}

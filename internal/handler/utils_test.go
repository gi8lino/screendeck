package handler

import (
	"errors"
	"net/http"
	"testing"

	"github.com/gi8lino/screendeck/internal/plex"
	"github.com/gi8lino/screendeck/internal/store"
	"github.com/stretchr/testify/assert"
)

// TestStatusForError verifies application errors map to stable public HTTP statuses.
func TestStatusForError(t *testing.T) {
	t.Run("bad request", func(t *testing.T) {
		assert.Equal(t, http.StatusBadRequest, statusForError(errors.New("invalid input")))
	})

	t.Run("not found", func(t *testing.T) {
		assert.Equal(t, http.StatusNotFound, statusForError(store.ErrNotFound))
	})

	t.Run("not configured", func(t *testing.T) {
		assert.Equal(t, http.StatusServiceUnavailable, statusForError(plex.ErrNotConfigured))
	})

	t.Run("upstream", func(t *testing.T) {
		assert.Equal(t, http.StatusBadGateway, statusForError(plex.ErrServerResponse))
	})
}

// TestIsPlexUpstreamError verifies only Plex transport and response failures are treated as upstream errors.
func TestIsPlexUpstreamError(t *testing.T) {
	t.Run("upstream error", func(t *testing.T) {
		assert.True(t, isPlexUpstreamError(plex.ErrCloudUnavailable))
	})

	t.Run("wrapped upstream error", func(t *testing.T) {
		assert.True(t, isPlexUpstreamError(errors.Join(errors.New("context"), plex.ErrAuthenticationRefresh)))
	})

	t.Run("application error", func(t *testing.T) {
		assert.False(t, isPlexUpstreamError(store.ErrNotFound))
	})
}

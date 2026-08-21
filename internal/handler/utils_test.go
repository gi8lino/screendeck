package handler

import (
	"bytes"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gi8lino/screendeck/internal/plex"
	"github.com/gi8lino/screendeck/internal/store"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// decodeTestRequest is the request shape used to verify strict JSON decoding.
type decodeTestRequest struct {
	// ItemID identifies the decoded media item.
	ItemID string `json:"itemId"`
}

// TestDecode verifies strict JSON request decoding.
func TestDecode(t *testing.T) {
	t.Run("accepts valid request", func(t *testing.T) {
		request := httptest.NewRequest(
			http.MethodPost,
			"/",
			bytes.NewBufferString(`{"itemId":"42"}`),
		)
		var input decodeTestRequest

		err := decode(request, &input)

		require.NoError(t, err)
		assert.Equal(t, "42", input.ItemID)
	})

	t.Run("rejects unknown field", func(t *testing.T) {
		request := httptest.NewRequest(
			http.MethodPost,
			"/",
			bytes.NewBufferString(`{"unexpected":"42"}`),
		)
		var input decodeTestRequest

		err := decode(request, &input)

		require.Error(t, err)
		assert.Contains(t, err.Error(), `unknown field "unexpected"`)
	})
}

// TestStatusForError verifies application errors map to stable public HTTP statuses.
func TestStatusForError(t *testing.T) {
	t.Run("bad request", func(t *testing.T) {
		assert.Equal(t, http.StatusBadRequest, statusForError(errors.New("invalid input")))
	})

	t.Run("identity unavailable", func(t *testing.T) {
		assert.Equal(t, http.StatusInternalServerError, statusForError(errBrowserIdentityUnavailable))
	})

	t.Run("conflict", func(t *testing.T) {
		assert.Equal(t, http.StatusConflict, statusForError(store.ErrMembershipConflict))
	})

	t.Run("forbidden", func(t *testing.T) {
		assert.Equal(t, http.StatusForbidden, statusForError(store.ErrForbidden))
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

// TestIsPlexUpstreamError verifies Plex transport and response error classification.
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

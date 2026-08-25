package handler

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gi8lino/screendeck/internal/jellyfin"
	"github.com/gi8lino/screendeck/internal/media"
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

type decodeValidTestRequest struct {
	Name string `json:"name"`
	Code string `json:"code"`
}

func (input *decodeValidTestRequest) Valid(context.Context) map[string]string {
	problems := make(map[string]string)
	if input.Name == "" {
		problems["name"] = "Enter your name."
	}
	if input.Code == "" {
		problems["code"] = "Enter a room code."
	}
	return problems
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

	t.Run("rejects multiple JSON values", func(t *testing.T) {
		request := httptest.NewRequest(
			http.MethodPost,
			"/",
			bytes.NewBufferString(`{"itemId":"42"} {"itemId":"43"}`),
		)
		var input decodeTestRequest

		err := decode(request, &input)

		require.Error(t, err)
		assert.Contains(t, err.Error(), "exactly one JSON value")
	})
}

func TestDecodeValidCollectsFieldProblems(t *testing.T) {
	request := httptest.NewRequest(
		http.MethodPost,
		"/",
		bytes.NewBufferString(`{}`),
	)
	var input decodeValidTestRequest

	err := decodeValid(request, &input)

	var validation validationError
	require.ErrorAs(t, err, &validation)
	assert.Equal(t, map[string]string{
		"code": "Enter a room code.",
		"name": "Enter your name.",
	}, validation.Problems)
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
		assert.Equal(t, http.StatusConflict, statusForError(store.ErrRoomLocked))
	})

	t.Run("forbidden", func(t *testing.T) {
		assert.Equal(t, http.StatusForbidden, statusForError(store.ErrForbidden))
	})

	t.Run("not found", func(t *testing.T) {
		assert.Equal(t, http.StatusNotFound, statusForError(store.ErrNotFound))
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
		assert.False(t, isMediaUpstreamError(store.ErrNotFound))
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

	t.Run("application error", func(t *testing.T) {
		err := errors.New("room is full")
		assert.Equal(t, "room is full", publicErrorMessage(err))
	})
}

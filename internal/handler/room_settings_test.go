package handler

import (
	"bytes"
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gi8lino/screendeck/internal/room"
	"github.com/stretchr/testify/assert"
)

type fakeRoomSettingsUpdater struct {
	code   string
	token  string
	locked bool
	err    error
}

func (f *fakeRoomSettingsUpdater) SetRoomLocked(_ context.Context, code, token string, locked bool) error {
	f.code = code
	f.token = token
	f.locked = locked
	return f.err
}

// TestRoomSettingsRequestValidation verifies the locked field is required.
func TestRoomSettingsRequestValidation(t *testing.T) {
	t.Parallel()
	t.Run("requires locked", func(t *testing.T) {
		t.Parallel()
		problems := (roomSettingsRequest{}).Valid(t.Context())

		assert.Equal(t, "Choose whether the room accepts new participants.", problems["locked"])
	})

	t.Run("accepts false", func(t *testing.T) {
		locked := false
		problems := (roomSettingsRequest{Locked: &locked}).Valid(t.Context())

		assert.Empty(t, problems)
	})
}

// TestUpdateRoomSettings verifies validated room settings are forwarded to the service.
func TestUpdateRoomSettings(t *testing.T) {
	t.Parallel()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	t.Run("updates lock state", func(t *testing.T) {
		t.Parallel()
		rooms := &fakeRoomSettingsUpdater{}
		request := httptest.NewRequest(
			http.MethodPatch,
			"/api/rooms/ABC234/settings",
			bytes.NewBufferString(`{"locked":true}`),
		)
		request.SetPathValue("code", "ABC234")
		request.Header.Set("X-Participant-Token", " participant-token ")
		response := httptest.NewRecorder()

		UpdateRoomSettings(rooms, logger).ServeHTTP(response, request)

		assert.Equal(t, http.StatusOK, response.Code)
		assert.Equal(t, "ABC234", rooms.code)
		assert.Equal(t, "participant-token", rooms.token)
		assert.True(t, rooms.locked)
		assert.JSONEq(t, `{"locked":true}`, response.Body.String())
	})

	t.Run("rejects missing lock state", func(t *testing.T) {
		t.Parallel()
		rooms := &fakeRoomSettingsUpdater{}
		request := httptest.NewRequest(http.MethodPatch, "/", bytes.NewBufferString(`{}`))
		response := httptest.NewRecorder()

		UpdateRoomSettings(rooms, logger).ServeHTTP(response, request)

		assert.Equal(t, http.StatusBadRequest, response.Code)
		assert.Contains(t, response.Body.String(), `"locked"`)
	})

	t.Run("maps service error", func(t *testing.T) {
		t.Parallel()
		rooms := &fakeRoomSettingsUpdater{err: room.ErrForbidden}
		request := httptest.NewRequest(http.MethodPatch, "/", bytes.NewBufferString(`{"locked":true}`))
		response := httptest.NewRecorder()

		UpdateRoomSettings(rooms, logger).ServeHTTP(response, request)

		assert.Equal(t, http.StatusForbidden, response.Code)
	})
}

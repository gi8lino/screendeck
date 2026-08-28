package handler

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gi8lino/screendeck/internal/room"
	"github.com/stretchr/testify/assert"
)

type fakeRoomLeaver struct {
	code  string
	token string
	err   error
}

func (f *fakeRoomLeaver) Leave(_ context.Context, code, token string) error {
	f.code = code
	f.token = token
	return f.err
}

type fakeParticipantRemover struct {
	code          string
	token         string
	participantID string
	err           error
}

func (f *fakeParticipantRemover) RemoveParticipant(_ context.Context, code, token, participantID string) error {
	f.code = code
	f.token = token
	f.participantID = participantID
	return f.err
}

// TestLeaveRoom verifies participant credentials are forwarded to the room service.
func TestLeaveRoom(t *testing.T) {
	t.Parallel()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	t.Run("leaves room", func(t *testing.T) {
		t.Parallel()
		rooms := &fakeRoomLeaver{}
		request := httptest.NewRequest(http.MethodPost, "/api/rooms/ABC234/leave", nil)
		request.SetPathValue("code", "ABC234")
		request.Header.Set("X-Participant-Token", " participant-token ")
		response := httptest.NewRecorder()

		LeaveRoom(rooms, logger).ServeHTTP(response, request)

		assert.Equal(t, http.StatusOK, response.Code)
		assert.Equal(t, "ABC234", rooms.code)
		assert.Equal(t, "participant-token", rooms.token)
		assert.JSONEq(t, `{"status":"left"}`, response.Body.String())
	})

	t.Run("maps service error", func(t *testing.T) {
		t.Parallel()
		rooms := &fakeRoomLeaver{err: room.ErrForbidden}
		request := httptest.NewRequest(http.MethodPost, "/api/rooms/ABC234/leave", nil)
		request.SetPathValue("code", "ABC234")
		response := httptest.NewRecorder()

		LeaveRoom(rooms, logger).ServeHTTP(response, request)

		assert.Equal(t, http.StatusForbidden, response.Code)
	})
}

// TestRemoveParticipant verifies host credentials and participant ID are forwarded.
func TestRemoveParticipant(t *testing.T) {
	t.Parallel()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	t.Run("removes participant", func(t *testing.T) {
		t.Parallel()
		rooms := &fakeParticipantRemover{}
		request := httptest.NewRequest(http.MethodDelete, "/api/rooms/ABC234/participants/guest", nil)
		request.SetPathValue("code", "ABC234")
		request.SetPathValue("participantID", "guest")
		request.Header.Set("X-Participant-Token", "host-token")
		response := httptest.NewRecorder()

		RemoveParticipant(rooms, logger).ServeHTTP(response, request)

		assert.Equal(t, http.StatusOK, response.Code)
		assert.Equal(t, "ABC234", rooms.code)
		assert.Equal(t, "host-token", rooms.token)
		assert.Equal(t, "guest", rooms.participantID)
		assert.JSONEq(t, `{"status":"removed"}`, response.Body.String())
	})

	t.Run("maps service error", func(t *testing.T) {
		t.Parallel()
		rooms := &fakeParticipantRemover{err: room.ErrForbidden}
		request := httptest.NewRequest(http.MethodDelete, "/api/rooms/ABC234/participants/guest", nil)
		request.SetPathValue("code", "ABC234")
		request.SetPathValue("participantID", "guest")
		response := httptest.NewRecorder()

		RemoveParticipant(rooms, logger).ServeHTTP(response, request)

		assert.Equal(t, http.StatusForbidden, response.Code)
	})
}

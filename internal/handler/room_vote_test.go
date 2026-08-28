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

type fakeRoomVoter struct {
	code   string
	token  string
	itemID string
	liked  bool
	match  bool
	err    error
}

func (f *fakeRoomVoter) Vote(_ context.Context, code, token, itemID string, liked bool) (bool, error) {
	f.code = code
	f.token = token
	f.itemID = itemID
	f.liked = liked
	return f.match, f.err
}

// TestVote verifies vote validation, forwarding, and match responses.
func TestVote(t *testing.T) {
	t.Parallel()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	t.Run("records vote", func(t *testing.T) {
		t.Parallel()
		rooms := &fakeRoomVoter{match: true}
		request := httptest.NewRequest(
			http.MethodPost,
			"/api/rooms/ABC234/votes",
			bytes.NewBufferString(`{"itemId":"item-1","liked":true}`),
		)
		request.SetPathValue("code", "ABC234")
		request.Header.Set("X-Participant-Token", " participant-token ")
		response := httptest.NewRecorder()

		Vote(rooms, logger).ServeHTTP(response, request)

		assert.Equal(t, http.StatusOK, response.Code)
		assert.Equal(t, "ABC234", rooms.code)
		assert.Equal(t, "participant-token", rooms.token)
		assert.Equal(t, "item-1", rooms.itemID)
		assert.True(t, rooms.liked)
		assert.JSONEq(t, `{"matched":true}`, response.Body.String())
	})

	t.Run("requires item ID", func(t *testing.T) {
		t.Parallel()
		rooms := &fakeRoomVoter{}
		request := httptest.NewRequest(http.MethodPost, "/", bytes.NewBufferString(`{"liked":true}`))
		response := httptest.NewRecorder()

		Vote(rooms, logger).ServeHTTP(response, request)

		assert.Equal(t, http.StatusBadRequest, response.Code)
		assert.JSONEq(t, `{"error":"itemId is required"}`, response.Body.String())
		assert.Empty(t, rooms.itemID)
	})

	t.Run("rejects malformed JSON", func(t *testing.T) {
		t.Parallel()
		rooms := &fakeRoomVoter{}
		request := httptest.NewRequest(http.MethodPost, "/", bytes.NewBufferString(`{"itemId":`))
		response := httptest.NewRecorder()

		Vote(rooms, logger).ServeHTTP(response, request)

		assert.Equal(t, http.StatusBadRequest, response.Code)
		assert.Empty(t, rooms.itemID)
	})

	t.Run("maps service error", func(t *testing.T) {
		t.Parallel()
		rooms := &fakeRoomVoter{err: room.ErrNotFound}
		request := httptest.NewRequest(
			http.MethodPost,
			"/",
			bytes.NewBufferString(`{"itemId":"item-1","liked":false}`),
		)
		response := httptest.NewRecorder()

		Vote(rooms, logger).ServeHTTP(response, request)

		assert.Equal(t, http.StatusNotFound, response.Code)
	})
}

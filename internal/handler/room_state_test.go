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

type fakeRoomGenreReader struct {
	code   string
	genres []string
	err    error
}

func (f *fakeRoomGenreReader) Genres(_ context.Context, code string) ([]string, error) {
	f.code = code
	return f.genres, f.err
}

type fakeRoomStateReader struct {
	code  string
	token string
	state room.State
	err   error
}

func (f *fakeRoomStateReader) State(_ context.Context, code, token string) (room.State, error) {
	f.code = code
	f.token = token
	return f.state, f.err
}

// TestRoomGenres verifies room code is forwarded and genres are returned.
func TestRoomGenres(t *testing.T) {
	t.Parallel()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	t.Run("returns genres", func(t *testing.T) {
		t.Parallel()
		rooms := &fakeRoomGenreReader{genres: []string{"Comedy", "Drama"}}
		request := httptest.NewRequest(http.MethodGet, "/api/rooms/ABC234/genres", nil)
		request.SetPathValue("code", "ABC234")
		response := httptest.NewRecorder()

		RoomGenres(rooms, logger).ServeHTTP(response, request)

		assert.Equal(t, http.StatusOK, response.Code)
		assert.Equal(t, "ABC234", rooms.code)
		assert.JSONEq(t, `{"genres":["Comedy","Drama"]}`, response.Body.String())
	})

	t.Run("maps service error", func(t *testing.T) {
		t.Parallel()
		rooms := &fakeRoomGenreReader{err: room.ErrNotFound}
		request := httptest.NewRequest(http.MethodGet, "/", nil)
		response := httptest.NewRecorder()

		RoomGenres(rooms, logger).ServeHTTP(response, request)

		assert.Equal(t, http.StatusNotFound, response.Code)
	})
}

// TestRoomState verifies participant credentials are forwarded for participant-specific state.
func TestRoomState(t *testing.T) {
	t.Parallel()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	t.Run("returns room state", func(t *testing.T) {
		t.Parallel()
		rooms := &fakeRoomStateReader{state: room.State{
			Room: room.Room{Code: "ABC234", Round: 2, Phase: room.PhaseSwiping},
			Me:   room.Participant{ID: "alice", Name: "Alice"},
		}}
		request := httptest.NewRequest(http.MethodGet, "/api/rooms/ABC234", nil)
		request.SetPathValue("code", "ABC234")
		request.Header.Set("X-Participant-Token", " participant-token ")
		response := httptest.NewRecorder()

		RoomState(rooms, logger).ServeHTTP(response, request)

		assert.Equal(t, http.StatusOK, response.Code)
		assert.Equal(t, "ABC234", rooms.code)
		assert.Equal(t, "participant-token", rooms.token)
		assert.Contains(t, response.Body.String(), `"code":"ABC234"`)
		assert.Contains(t, response.Body.String(), `"name":"Alice"`)
	})

	t.Run("maps service error", func(t *testing.T) {
		t.Parallel()
		rooms := &fakeRoomStateReader{err: room.ErrForbidden}
		request := httptest.NewRequest(http.MethodGet, "/", nil)
		response := httptest.NewRecorder()

		RoomState(rooms, logger).ServeHTTP(response, request)

		assert.Equal(t, http.StatusForbidden, response.Code)
	})
}

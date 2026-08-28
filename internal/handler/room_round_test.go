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

type fakeRoomExpander struct {
	code   string
	token  string
	count  int
	result room.MoreTitlesResult
	err    error
}

func (f *fakeRoomExpander) AddMoreTitles(_ context.Context, code, token string, count int) (room.MoreTitlesResult, error) {
	f.code = code
	f.token = token
	f.count = count
	return f.result, f.err
}

type fakeRoundReadinessUpdater struct {
	code          string
	token         string
	expectedRound int
	ready         bool
	result        room.RoundResult
	err           error
}

func (f *fakeRoundReadinessUpdater) SetNextRoundReady(
	_ context.Context,
	code,
	token string,
	expectedRound int,
	ready bool,
) (room.RoundResult, error) {
	f.code = code
	f.token = token
	f.expectedRound = expectedRound
	f.ready = ready
	return f.result, f.err
}

// TestAddMoreTitles verifies first-round expansion input and result handling.
func TestAddMoreTitles(t *testing.T) {
	t.Parallel()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	t.Run("returns expansion result", func(t *testing.T) {
		t.Parallel()
		rooms := &fakeRoomExpander{result: room.MoreTitlesResult{Added: 12, Remaining: 34}}
		request := httptest.NewRequest(
			http.MethodPost,
			"/api/rooms/ABC234/more",
			bytes.NewBufferString(`{"count":12}`),
		)
		request.SetPathValue("code", "ABC234")
		request.Header.Set("X-Participant-Token", " participant-token ")
		response := httptest.NewRecorder()

		AddMoreTitles(rooms, logger).ServeHTTP(response, request)

		assert.Equal(t, http.StatusOK, response.Code)
		assert.Equal(t, "ABC234", rooms.code)
		assert.Equal(t, "participant-token", rooms.token)
		assert.Equal(t, 12, rooms.count)
		assert.JSONEq(t, `{"added":12,"remaining":34}`, response.Body.String())
	})

	t.Run("rejects malformed JSON", func(t *testing.T) {
		t.Parallel()
		rooms := &fakeRoomExpander{}
		request := httptest.NewRequest(http.MethodPost, "/", bytes.NewBufferString(`{"count":`))
		response := httptest.NewRecorder()

		AddMoreTitles(rooms, logger).ServeHTTP(response, request)

		assert.Equal(t, http.StatusBadRequest, response.Code)
		assert.Zero(t, rooms.count)
	})

	t.Run("maps service error", func(t *testing.T) {
		t.Parallel()
		rooms := &fakeRoomExpander{err: room.ErrForbidden}
		request := httptest.NewRequest(http.MethodPost, "/", bytes.NewBufferString(`{"count":5}`))
		response := httptest.NewRecorder()

		AddMoreTitles(rooms, logger).ServeHTTP(response, request)

		assert.Equal(t, http.StatusForbidden, response.Code)
	})
}

// TestNextRoundReady verifies readiness updates are decoded and forwarded.
func TestNextRoundReady(t *testing.T) {
	t.Parallel()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	t.Run("returns readiness result", func(t *testing.T) {
		t.Parallel()
		rooms := &fakeRoundReadinessUpdater{result: room.RoundResult{
			Round:    2,
			Titles:   3,
			Ready:    2,
			Required: 2,
			Advanced: true,
		}}
		request := httptest.NewRequest(
			http.MethodPost,
			"/api/rooms/ABC234/next-round",
			bytes.NewBufferString(`{"round":1,"ready":true}`),
		)
		request.SetPathValue("code", "ABC234")
		request.Header.Set("X-Participant-Token", "participant-token")
		response := httptest.NewRecorder()

		NextRoundReady(rooms, logger).ServeHTTP(response, request)

		assert.Equal(t, http.StatusOK, response.Code)
		assert.Equal(t, "ABC234", rooms.code)
		assert.Equal(t, "participant-token", rooms.token)
		assert.Equal(t, 1, rooms.expectedRound)
		assert.True(t, rooms.ready)
		assert.JSONEq(t, `{"round":2,"titles":3,"ready":2,"required":2,"advanced":true}`, response.Body.String())
	})

	t.Run("maps service error", func(t *testing.T) {
		t.Parallel()
		rooms := &fakeRoundReadinessUpdater{err: room.ErrNotFound}
		request := httptest.NewRequest(
			http.MethodPost,
			"/",
			bytes.NewBufferString(`{"round":1,"ready":true}`),
		)
		response := httptest.NewRecorder()

		NextRoundReady(rooms, logger).ServeHTTP(response, request)

		assert.Equal(t, http.StatusNotFound, response.Code)
	})
}

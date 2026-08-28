package handler

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gi8lino/screendeck/internal/room"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// deadlineResponseWriter records write-deadline changes made through a response controller.
type deadlineResponseWriter struct {
	*httptest.ResponseRecorder
	deadline time.Time
	called   bool
}

// SetWriteDeadline records the requested response write deadline.
func (w *deadlineResponseWriter) SetWriteDeadline(deadline time.Time) error {
	w.deadline = deadline
	w.called = true
	return nil
}

type fakeRoomEventSource struct {
	code         string
	token        string
	stateErr     error
	subscribed   bool
	unsubscribed bool
	events       chan struct{}
}

func (f *fakeRoomEventSource) State(_ context.Context, code, token string) (room.State, error) {
	f.code = code
	f.token = token
	return room.State{}, f.stateErr
}

func (f *fakeRoomEventSource) Subscribe(string) (<-chan struct{}, func()) {
	f.subscribed = true
	if f.events == nil {
		f.events = make(chan struct{})
	}
	return f.events, func() {
		f.unsubscribed = true
	}
}

// TestEvents verifies event streams authenticate participants and release subscriptions.
func TestEvents(t *testing.T) {
	t.Parallel()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	t.Run("rejects unauthorized participant before subscribing", func(t *testing.T) {
		t.Parallel()
		rooms := &fakeRoomEventSource{stateErr: room.ErrForbidden}
		request := httptest.NewRequest(http.MethodGet, "/api/rooms/ABC234/events", nil)
		request.SetPathValue("code", "ABC234")
		request.Header.Set("X-Participant-Token", " bad-token ")
		response := httptest.NewRecorder()

		Events(rooms, logger).ServeHTTP(response, request)

		assert.Equal(t, http.StatusForbidden, response.Code)
		assert.Equal(t, "ABC234", rooms.code)
		assert.Equal(t, "bad-token", rooms.token)
		assert.False(t, rooms.subscribed)
	})

	t.Run("opens stream and unsubscribes when request is canceled", func(t *testing.T) {
		t.Parallel()
		rooms := &fakeRoomEventSource{}
		ctx, cancel := context.WithCancel(t.Context())
		cancel()

		request := httptest.NewRequest(http.MethodGet, "/api/rooms/ABC234/events", nil).WithContext(ctx)
		request.SetPathValue("code", "ABC234")
		request.Header.Set("X-Participant-Token", " participant-token ")
		response := httptest.NewRecorder()

		Events(rooms, logger).ServeHTTP(response, request)

		assert.Equal(t, "text/event-stream", response.Header().Get("Content-Type"))
		assert.Equal(t, "no-cache", response.Header().Get("Cache-Control"))
		assert.Equal(t, "no", response.Header().Get("X-Accel-Buffering"))
		assert.Contains(t, response.Body.String(), "event: update\ndata: connected\n\n")
		assert.Equal(t, "ABC234", rooms.code)
		assert.Equal(t, "participant-token", rooms.token)
		assert.True(t, rooms.subscribed)
		assert.True(t, rooms.unsubscribed)
	})
}

// TestDisableWriteDeadline verifies that event streams remove inherited server deadlines.
func TestDisableWriteDeadline(t *testing.T) {
	t.Parallel()

	w := &deadlineResponseWriter{ResponseRecorder: httptest.NewRecorder()}
	require.NoError(t, disableWriteDeadline(w))
	require.True(t, w.called)
	require.True(t, w.deadline.IsZero())
}

// TestDisableWriteDeadlineAllowsUnsupportedWriters verifies writers without deadlines remain usable.
func TestDisableWriteDeadlineAllowsUnsupportedWriters(t *testing.T) {
	t.Parallel()
	require.NoError(t, disableWriteDeadline(httptest.NewRecorder()))
}

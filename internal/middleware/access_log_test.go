package middleware

import (
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// deadlineResponseWriter records write-deadline changes made through middleware.
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

// TestAccessLogPreservesResponseControl verifies wrapped streaming handlers can control deadlines.
func TestAccessLogPreservesResponseControl(t *testing.T) {
	t.Parallel()

	w := &deadlineResponseWriter{ResponseRecorder: httptest.NewRecorder()}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	handler := AccessLog(logger)(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		require.NoError(t, http.NewResponseController(w).SetWriteDeadline(time.Time{}))
	}))

	handler.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/events", nil))
	require.True(t, w.called)
	require.True(t, w.deadline.IsZero())
}

package handler

import (
	"net/http/httptest"
	"testing"
	"time"

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

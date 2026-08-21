package maintenance

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// cleanupTestStore records room-cleanup calls and can return a configured error.
type cleanupTestStore struct {
	// calls receives a signal whenever DeleteExpired is invoked.
	calls chan struct{}
	// err is returned by DeleteExpired.
	err error
}

// DeleteExpired records a cleanup invocation and returns the configured error.
func (s *cleanupTestStore) DeleteExpired(context.Context) error {
	select {
	case s.calls <- struct{}{}:
	default:
	}
	return s.err
}

// TestRunRoomCleanup verifies periodic cleanup, cancellation, and error logging.
func TestRunRoomCleanup(t *testing.T) {
	t.Run("runs and stops", func(t *testing.T) {
		ctx, cancel := context.WithCancel(t.Context())
		t.Cleanup(cancel)

		store := &cleanupTestStore{calls: make(chan struct{}, 1)}
		done := make(chan struct{})

		go func() {
			RunRoomCleanup(
				ctx,
				store,
				time.Millisecond,
				slog.New(slog.NewTextHandler(&bytes.Buffer{}, nil)),
			)
			close(done)
		}()

		select {
		case <-store.calls:
			cancel()
		case <-time.After(time.Second):
			require.FailNow(t, "room cleanup was not invoked")
		}

		select {
		case <-done:
		case <-time.After(time.Second):
			require.FailNow(t, "room cleanup did not stop after cancellation")
		}
	})

	t.Run("logs failures", func(t *testing.T) {
		ctx, cancel := context.WithCancel(t.Context())
		t.Cleanup(cancel)

		store := &cleanupTestStore{
			calls: make(chan struct{}, 1),
			err:   errors.New("database unavailable"),
		}
		var output bytes.Buffer
		logger := slog.New(slog.NewJSONHandler(&output, nil))
		done := make(chan struct{})

		go func() {
			RunRoomCleanup(ctx, store, time.Millisecond, logger)
			close(done)
		}()

		select {
		case <-store.calls:
			cancel()
		case <-time.After(time.Second):
			require.FailNow(t, "room cleanup was not invoked")
		}

		select {
		case <-done:
		case <-time.After(time.Second):
			require.FailNow(t, "room cleanup did not stop after cancellation")
		}

		assert.Contains(t, output.String(), `"event":"delete_expired_rooms_failed"`)
		assert.Contains(t, output.String(), "database unavailable")
	})
}

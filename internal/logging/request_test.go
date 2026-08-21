package logging

import (
	"bytes"
	"encoding/hex"
	"log/slog"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewRequestID(t *testing.T) {
	t.Parallel()

	t.Run("contains sixteen random bytes encoded as hexadecimal", func(t *testing.T) {
		t.Parallel()

		requestID := NewRequestID()
		require.Len(t, requestID, 32)

		decoded, err := hex.DecodeString(requestID)
		require.NoError(t, err)
		assert.Len(t, decoded, 16)
	})
}

func TestWithRequestID(t *testing.T) {
	t.Parallel()

	t.Run("stores identifier", func(t *testing.T) {
		t.Parallel()

		ctx := WithRequestID(t.Context(), "request-123")
		assert.Equal(t, "request-123", RequestID(ctx))
	})
}

func TestRequestID(t *testing.T) {
	t.Parallel()

	t.Run("returns stored identifier", func(t *testing.T) {
		t.Parallel()

		ctx := WithRequestID(t.Context(), "request-123")
		assert.Equal(t, "request-123", RequestID(ctx))
	})

	t.Run("returns empty for nil context", func(t *testing.T) {
		t.Parallel()
		//lint:ignore SA1012 RequestID intentionally receives nil to verify defensive behavior.
		assert.Empty(t, RequestID(nil))
	})
}

// TestWithRequestIDLogger verifies request-aware loggers include request identifiers.
func TestWithRequestIDLogger(t *testing.T) {
	t.Parallel()

	t.Run("adds context request identifier", func(t *testing.T) {
		t.Parallel()

		var output bytes.Buffer
		logger := slog.New(slog.NewJSONHandler(&output, nil))
		ctx := WithRequestID(t.Context(), "request-123")

		WithRequestIDLogger(logger, ctx).Info("handled")

		assert.Contains(t, output.String(), `"request_id":"request-123"`)
	})
}

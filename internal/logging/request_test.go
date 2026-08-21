package logging

import (
	"bytes"
	"encoding/hex"
	"log/slog"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestNewRequestID verifies generated request identifiers.
func TestNewRequestID(t *testing.T) {
	t.Run("contains sixteen random bytes encoded as hexadecimal", func(t *testing.T) {
		requestID := NewRequestID()
		require.Len(t, requestID, 32)

		decoded, err := hex.DecodeString(requestID)
		require.NoError(t, err)
		assert.Len(t, decoded, 16)
	})
}

// TestWithRequestID verifies request identifiers are stored in contexts.
func TestWithRequestID(t *testing.T) {
	t.Run("stores identifier", func(t *testing.T) {
		ctx := WithRequestID(t.Context(), "request-123")
		assert.Equal(t, "request-123", RequestID(ctx))
	})
}

// TestRequestID verifies request identifiers can be read from contexts.
func TestRequestID(t *testing.T) {
	t.Run("returns stored identifier", func(t *testing.T) {
		ctx := WithRequestID(t.Context(), "request-123")
		assert.Equal(t, "request-123", RequestID(ctx))
	})

	t.Run("returns empty for nil context", func(t *testing.T) {
		assert.Empty(t, RequestID(nil))
	})
}

// TestWithRequestIDLogger verifies request-aware loggers include request identifiers.
func TestWithRequestIDLogger(t *testing.T) {
	t.Run("adds context request identifier", func(t *testing.T) {
		var output bytes.Buffer
		logger := slog.New(slog.NewJSONHandler(&output, nil))
		ctx := WithRequestID(t.Context(), "request-123")

		WithRequestIDLogger(logger, ctx).Info("handled")

		assert.Contains(t, output.String(), `"request_id":"request-123"`)
	})
}

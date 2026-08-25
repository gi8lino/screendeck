package requestid

import (
	"bytes"
	"encoding/hex"
	"log/slog"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNew(t *testing.T) {
	t.Parallel()

	t.Run("contains sixteen random bytes encoded as hexadecimal", func(t *testing.T) {
		t.Parallel()

		requestID := New()
		require.Len(t, requestID, 32)

		decoded, err := hex.DecodeString(requestID)
		require.NoError(t, err)
		assert.Len(t, decoded, 16)
	})
}

func TestWithContext(t *testing.T) {
	t.Parallel()

	t.Run("stores identifier", func(t *testing.T) {
		t.Parallel()

		ctx := WithContext(t.Context(), "request-123")
		assert.Equal(t, "request-123", FromContext(ctx))
	})
}

func TestFromContext(t *testing.T) {
	t.Parallel()

	t.Run("returns stored identifier", func(t *testing.T) {
		t.Parallel()

		ctx := WithContext(t.Context(), "request-123")
		assert.Equal(t, "request-123", FromContext(ctx))
	})

	t.Run("returns empty for nil context", func(t *testing.T) {
		t.Parallel()
		//lint:ignore SA1012 FromContext intentionally receives nil to verify defensive behavior.
		assert.Empty(t, FromContext(nil))
	})
}

// TestWithLogger verifies request-aware loggers include request identifiers.
func TestWithLogger(t *testing.T) {
	t.Parallel()

	t.Run("adds context request identifier", func(t *testing.T) {
		t.Parallel()

		var output bytes.Buffer
		logger := slog.New(slog.NewJSONHandler(&output, nil))
		ctx := WithContext(t.Context(), "request-123")

		WithLogger(logger, ctx).Info("handled")

		assert.Contains(t, output.String(), `"request_id":"request-123"`)
	})
}

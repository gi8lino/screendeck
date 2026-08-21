package logging

import (
	"bytes"
	"context"
	"encoding/hex"
	"log/slog"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestNewRequestID verifies generated request identifiers contain 16 random bytes encoded as hexadecimal.
func TestNewRequestID(t *testing.T) {
	requestID := NewRequestID()
	require.Len(t, requestID, 32)

	decoded, err := hex.DecodeString(requestID)
	require.NoError(t, err)
	assert.Len(t, decoded, 16)
}

// TestRequestID verifies request identifiers round-trip through context storage.
func TestRequestID(t *testing.T) {
	ctx := WithRequestID(context.Background(), "request-123")
	assert.Equal(t, "request-123", RequestID(ctx))
	assert.Empty(t, RequestID(nil))
}

// TestWithRequestIDLogger verifies request-aware loggers include the context request identifier.
func TestWithRequestIDLogger(t *testing.T) {
	var output bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&output, nil))
	ctx := WithRequestID(context.Background(), "request-123")

	WithRequestIDLogger(logger, ctx).Info("handled")

	assert.True(t, strings.Contains(output.String(), `"request_id":"request-123"`))
}

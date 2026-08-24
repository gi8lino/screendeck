package logging

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"io"
	"log/slog"
)

const RequestIDHeader = "X-Request-Id"

// requestIDKey is the private context key used for request identifiers.
type requestIDKey struct{}

// NewRequestID creates a random request identifier.
func NewRequestID() string {
	var value [16]byte
	if _, err := io.ReadFull(rand.Reader, value[:]); err != nil {
		return ""
	}
	return hex.EncodeToString(value[:])
}

// WithRequestID stores a request identifier in a context.
func WithRequestID(ctx context.Context, requestID string) context.Context {
	return context.WithValue(ctx, requestIDKey{}, requestID)
}

// RequestID returns the request identifier stored in a context.
func RequestID(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	requestID, _ := ctx.Value(requestIDKey{}).(string)
	return requestID
}

// WithRequestIDLogger enriches a logger with a context request identifier.
func WithRequestIDLogger(logger *slog.Logger, ctx context.Context) *slog.Logger {
	if logger == nil {
		logger = slog.Default()
	}
	if requestID := RequestID(ctx); requestID != "" {
		return logger.With("request_id", requestID)
	}
	return logger
}

package requestid

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"log/slog"
)

const Header = "X-Request-Id"

// requestIDKey is the private context key used for request identifiers.
type requestIDKey struct{}

// New creates a random request identifier.
func New() string {
	var value [16]byte
	if _, err := rand.Read(value[:]); err != nil {
		return ""
	}
	return hex.EncodeToString(value[:])
}

// WithContext stores a request identifier in a context.
func WithContext(ctx context.Context, requestID string) context.Context {
	return context.WithValue(ctx, requestIDKey{}, requestID)
}

// FromContext returns the request identifier stored in a context.
func FromContext(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	requestID, _ := ctx.Value(requestIDKey{}).(string)
	return requestID
}

// WithLogger enriches a logger with a context request identifier.
func WithLogger(logger *slog.Logger, ctx context.Context) *slog.Logger {
	if logger == nil {
		logger = slog.Default()
	}
	if requestID := FromContext(ctx); requestID != "" {
		return logger.With("request_id", requestID)
	}
	return logger
}

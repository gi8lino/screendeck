package middleware

import (
	"log/slog"
	"net/http"
	"time"

	"github.com/gi8lino/screendeck/internal/logging"
)

type responseRecorder struct {
	http.ResponseWriter
	status int
}

// WriteHeader records the response status before writing it.
func (r *responseRecorder) WriteHeader(status int) {
	r.status = status
	r.ResponseWriter.WriteHeader(status)
}

// Flush forwards buffered response data when supported.
func (r *responseRecorder) Flush() {
	if flusher, ok := r.ResponseWriter.(http.Flusher); ok {
		flusher.Flush()
	}
}

// RequestLogging logs the method, path, status, client, and duration of requests.
func RequestLogging(logger *slog.Logger) Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			started := time.Now()
			recorder := &responseRecorder{ResponseWriter: w, status: http.StatusOK}
			next.ServeHTTP(recorder, r)
			logging.WithRequestIDLogger(logger, r.Context()).Info(
				"HTTP request",
				"event", "http_request",
				"method", r.Method,
				"path", r.URL.Path,
				"status", recorder.status,
				"ip", r.RemoteAddr,
				"user_agent", r.UserAgent(),
				"duration_ms", time.Since(started).Milliseconds(),
			)
		})
	}
}

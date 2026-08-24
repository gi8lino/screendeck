package middleware

import (
	"net/http"

	"github.com/gi8lino/screendeck/internal/logging"
)

// RequestID ensures every request has an identifier in its context and response.
func RequestID() Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			requestID := r.Header.Get(logging.RequestIDHeader)
			if requestID == "" {
				requestID = logging.NewRequestID()
			}
			if requestID != "" {
				w.Header().Set(logging.RequestIDHeader, requestID)
				r = r.WithContext(logging.WithRequestID(r.Context(), requestID))
			}
			next.ServeHTTP(w, r)
		})
	}
}

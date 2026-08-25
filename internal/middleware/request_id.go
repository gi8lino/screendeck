package middleware

import (
	"net/http"

	"github.com/gi8lino/screendeck/internal/requestid"
)

// RequestID ensures every request has an identifier in its context and response.
func RequestID() Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			requestID := r.Header.Get(requestid.Header)
			if requestID == "" {
				requestID = requestid.New()
			}
			if requestID != "" {
				w.Header().Set(requestid.Header, requestID)
				r = r.WithContext(requestid.WithContext(r.Context(), requestID))
			}
			next.ServeHTTP(w, r)
		})
	}
}

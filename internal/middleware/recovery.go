package middleware

import (
	"log/slog"
	"net/http"
)

// RecoverPanics converts handler panics into internal server errors.
func RecoverPanics(logger *slog.Logger) Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			defer func() {
				if value := recover(); value != nil {
					logger.Error("request panic",
						"event", "request_panic",
						"value", value,
					)
					http.Error(w, "internal server error", http.StatusInternalServerError)
				}
			}()
			next.ServeHTTP(w, r)
		})
	}
}

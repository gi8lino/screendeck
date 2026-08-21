package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestSecurityHeaders(t *testing.T) {
	t.Parallel()

	handler := SecurityHeaders(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	response := httptest.NewRecorder()

	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/", nil))

	assert.Equal(t, http.StatusNoContent, response.Code)
	assert.Equal(t, "nosniff", response.Header().Get("X-Content-Type-Options"))
	assert.Equal(t, "no-referrer", response.Header().Get("Referrer-Policy"))
	assert.Equal(t, "DENY", response.Header().Get("X-Frame-Options"))
	assert.Contains(t, response.Header().Get("Content-Security-Policy"), "default-src 'self'")
}

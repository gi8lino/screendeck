package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gi8lino/screendeck/internal/logging"
	"github.com/stretchr/testify/assert"
)

// TestRequestID verifies request identifiers are propagated through headers and context.
func TestRequestID(t *testing.T) {
	handler := RequestID()(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "known-request", logging.RequestID(r.Context()))
		w.WriteHeader(http.StatusNoContent)
	}))
	request := httptest.NewRequest(http.MethodGet, "/", nil)
	request.Header.Set(logging.RequestIDHeader, "known-request")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	assert.Equal(t, "known-request", response.Header().Get(logging.RequestIDHeader))
}

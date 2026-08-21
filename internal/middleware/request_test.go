package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gi8lino/screendeck/internal/logging"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestRequestID verifies request identifiers are propagated or generated as needed.
func TestRequestID(t *testing.T) {
	t.Run("propagates provided identifier", func(t *testing.T) {
		handler := RequestID()(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			assert.Equal(t, "known-request", logging.RequestID(r.Context()))
			w.WriteHeader(http.StatusNoContent)
		}))
		request := httptest.NewRequest(http.MethodGet, "/", nil)
		request.Header.Set(logging.RequestIDHeader, "known-request")
		response := httptest.NewRecorder()

		handler.ServeHTTP(response, request)

		assert.Equal(t, "known-request", response.Header().Get(logging.RequestIDHeader))
	})

	t.Run("generates missing identifier", func(t *testing.T) {
		var contextID string
		handler := RequestID()(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			contextID = logging.RequestID(r.Context())
			w.WriteHeader(http.StatusNoContent)
		}))
		request := httptest.NewRequest(http.MethodGet, "/", nil)
		response := httptest.NewRecorder()

		handler.ServeHTTP(response, request)

		responseID := response.Header().Get(logging.RequestIDHeader)
		require.NotEmpty(t, responseID)
		assert.Len(t, responseID, 32)
		assert.Equal(t, responseID, contextID)
	})
}

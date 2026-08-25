package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gi8lino/screendeck/internal/requestid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRequestID(t *testing.T) {
	t.Parallel()

	t.Run("propagates provided identifier", func(t *testing.T) {
		t.Parallel()

		handler := RequestID()(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			assert.Equal(t, "known-request", requestid.FromContext(r.Context()))
			w.WriteHeader(http.StatusNoContent)
		}))
		request := httptest.NewRequest(http.MethodGet, "/", nil)
		request.Header.Set(requestid.Header, "known-request")
		response := httptest.NewRecorder()

		handler.ServeHTTP(response, request)

		assert.Equal(t, "known-request", response.Header().Get(requestid.Header))
	})

	t.Run("generates missing identifier", func(t *testing.T) {
		t.Parallel()

		var contextID string
		handler := RequestID()(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			contextID = requestid.FromContext(r.Context())
			w.WriteHeader(http.StatusNoContent)
		}))
		request := httptest.NewRequest(http.MethodGet, "/", nil)
		response := httptest.NewRecorder()

		handler.ServeHTTP(response, request)

		responseID := response.Header().Get(requestid.Header)
		require.NotEmpty(t, responseID)
		assert.Len(t, responseID, 32)
		assert.Equal(t, responseID, contextID)
	})
}

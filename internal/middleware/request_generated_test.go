package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gi8lino/screendeck/internal/logging"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestRequestIDGeneratesMissingID verifies requests without an identifier receive a generated one.
func TestRequestIDGeneratesMissingID(t *testing.T) {
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
}

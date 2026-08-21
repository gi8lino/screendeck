package middleware

import (
	"bytes"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestRecoverPanics verifies router recovery middleware converts panics into HTTP 500 responses.
func TestRecoverPanics(t *testing.T) {
	var output bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&output, nil))
	handler := RecoverPanics(logger)(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		panic("boom")
	}))
	response := httptest.NewRecorder()

	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/", nil))

	assert.Equal(t, http.StatusInternalServerError, response.Code)
	assert.True(t, strings.Contains(output.String(), `"event":"request_panic"`))
	assert.True(t, strings.Contains(output.String(), "boom"))
}

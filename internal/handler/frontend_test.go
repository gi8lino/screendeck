package handler

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"testing/fstest"

	"github.com/stretchr/testify/assert"
)

func TestFrontendRejectsUnsupportedMethods(t *testing.T) {
	t.Parallel()

	handler := Frontend(fstest.MapFS{"index.html": {Data: []byte("ScreenDeck")}})
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/", nil))

	assert.Equal(t, http.StatusMethodNotAllowed, response.Code)
	assert.Equal(t, "GET, HEAD", response.Header().Get("Allow"))
}

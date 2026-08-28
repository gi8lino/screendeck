package handler

import (
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gi8lino/screendeck/internal/media"
	"github.com/stretchr/testify/assert"
)

type fakeMediaStatusReader struct {
	status media.Status
}

func (f fakeMediaStatusReader) Status() media.Status {
	return f.status
}

// TestConfig verifies public runtime configuration is exposed to the browser.
func TestConfig(t *testing.T) {
	t.Parallel()

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	request := httptest.NewRequest(http.MethodGet, "/api/config", nil)
	response := httptest.NewRecorder()

	Config(
		"v1.2.3",
		"abc123",
		"https://movies.test",
		true,
		true,
		fakeMediaStatusReader{status: media.Status{
			Configured: true,
			Provider:   media.ProviderJellyfin,
			ServerName: "Home Jellyfin",
		}},
		logger,
	).ServeHTTP(response, request)

	assert.Equal(t, http.StatusOK, response.Code)
	assert.JSONEq(t, `{
		"version":"v1.2.3",
		"commit":"abc123",
		"baseUrl":"https://movies.test",
		"experimental":true,
		"networkWarning":true,
		"mediaConfigured":true,
		"mediaProvider":"jellyfin",
		"mediaServerName":"Home Jellyfin"
	}`, response.Body.String())
}

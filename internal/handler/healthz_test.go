package handler

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
)

// fakeHealthProber implements the healthProber dependency used by health handler tests.
type fakeHealthProber struct {
	err error
}

// Ping returns the configured health check result.
func (d fakeHealthProber) Ping(context.Context) error {
	return d.err
}

// TestHealth verifies healthProber health determines the HTTP health status.
func TestHealth(t *testing.T) {
	t.Run("healthy healthProber", func(t *testing.T) {
		api := &API{
			healthProber: fakeHealthProber{},
		}
		response := httptest.NewRecorder()
		request := httptest.NewRequest(http.MethodGet, "/healthz", nil)

		api.Health().ServeHTTP(response, request)

		assert.Equal(t, http.StatusOK, response.Code)
		assert.JSONEq(t, `{"status":"ok"}`, response.Body.String())
	})

	t.Run("unhealthy healthProber", func(t *testing.T) {
		api := &API{
			healthProber: fakeHealthProber{
				err: errors.New("healthProber unavailable"),
			},
		}
		response := httptest.NewRecorder()
		request := httptest.NewRequest(http.MethodGet, "/healthz", nil)

		api.Health().ServeHTTP(response, request)

		assert.Equal(t, http.StatusServiceUnavailable, response.Code)
		assert.JSONEq(t, `{"status":"unhealthy"}`, response.Body.String())
	})
}

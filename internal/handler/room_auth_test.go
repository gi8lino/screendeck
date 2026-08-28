package handler

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestParticipantToken verifies participant credentials are read only from the expected header.
func TestParticipantToken(t *testing.T) {
	t.Parallel()
	t.Run("trims header value", func(t *testing.T) {
		t.Parallel()
		request := httptest.NewRequest(http.MethodGet, "/", nil)
		request.Header.Set("X-Participant-Token", "  participant-token  ")

		assert.Equal(t, "participant-token", participantToken(request))
	})

	t.Run("missing header", func(t *testing.T) {
		t.Parallel()
		request := httptest.NewRequest(http.MethodGet, "/", nil)

		assert.Empty(t, participantToken(request))
	})
}

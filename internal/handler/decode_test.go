package handler

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// decodeTestRequest is the request shape used to verify strict JSON decoding.
type decodeTestRequest struct {
	// ItemID identifies the decoded media item.
	ItemID string `json:"itemId"`
}

// TestDecode verifies valid JSON is accepted and unknown compatibility fields are rejected.
func TestDecode(t *testing.T) {
	t.Run("valid request", func(t *testing.T) {
		request := httptest.NewRequest(
			http.MethodPost,
			"/",
			bytes.NewBufferString(`{"itemId":"42"}`),
		)
		var input decodeTestRequest

		err := decode(request, &input)

		require.NoError(t, err)
		assert.Equal(t, "42", input.ItemID)
	})

	t.Run("unknown field", func(t *testing.T) {
		request := httptest.NewRequest(
			http.MethodPost,
			"/",
			bytes.NewBufferString(`{"unexpected":"42"}`),
		)
		var input decodeTestRequest

		err := decode(request, &input)

		require.Error(t, err)
		assert.Contains(t, err.Error(), `unknown field "unexpected"`)
	})
}

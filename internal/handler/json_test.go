package handler

import (
	"bytes"
	"context"
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

// decodeValidTestRequest is the request shape used to verify field-level validation.
type decodeValidTestRequest struct {
	Name string `json:"name"`
	Code string `json:"code"`
}

// Valid returns all missing fields in the validation test request.
func (input decodeValidTestRequest) Valid(context.Context) map[string]string {
	problems := make(map[string]string)
	if input.Name == "" {
		problems["name"] = "Enter your name."
	}
	if input.Code == "" {
		problems["code"] = "Enter a room code."
	}
	return problems
}

// TestDecode verifies strict JSON request decoding.
func TestDecode(t *testing.T) {
	t.Run("accepts valid request", func(t *testing.T) {
		request := httptest.NewRequest(
			http.MethodPost,
			"/",
			bytes.NewBufferString(`{"itemId":"42"}`),
		)
		input, err := decode[decodeTestRequest](request)

		require.NoError(t, err)
		assert.Equal(t, "42", input.ItemID)
	})

	t.Run("rejects unknown field", func(t *testing.T) {
		request := httptest.NewRequest(
			http.MethodPost,
			"/",
			bytes.NewBufferString(`{"unexpected":"42"}`),
		)
		_, err := decode[decodeTestRequest](request)

		require.Error(t, err)
		assert.Equal(t, http.StatusBadRequest, statusForError(err))
		assert.Contains(t, err.Error(), "unexpected")
	})

	t.Run("rejects multiple JSON values", func(t *testing.T) {
		request := httptest.NewRequest(
			http.MethodPost,
			"/",
			bytes.NewBufferString(`{"itemId":"42"} {"itemId":"43"}`),
		)
		_, err := decode[decodeTestRequest](request)

		require.Error(t, err)
		assert.Equal(t, http.StatusBadRequest, statusForError(err))
		assert.Contains(t, err.Error(), "invalid request")
	})

	t.Run("rejects duplicate fields", func(t *testing.T) {
		request := httptest.NewRequest(
			http.MethodPost,
			"/",
			bytes.NewBufferString(`{"itemId":"42","itemId":"43"}`),
		)
		_, err := decode[decodeTestRequest](request)

		require.Error(t, err)
		assert.Equal(t, http.StatusBadRequest, statusForError(err))
		assert.Contains(t, err.Error(), "itemId")
	})

	t.Run("rejects invalid UTF-8", func(t *testing.T) {
		body := []byte{'{', '"', 'i', 't', 'e', 'm', 'I', 'd', '"', ':', '"', 0xff, '"', '}'}
		request := httptest.NewRequest(http.MethodPost, "/", bytes.NewReader(body))
		_, err := decode[decodeTestRequest](request)

		require.Error(t, err)
		assert.Equal(t, http.StatusBadRequest, statusForError(err))
		assert.Contains(t, err.Error(), "invalid request")
	})
}

// TestDecodeValidCollectsFieldProblems verifies validation returns every field problem.
func TestDecodeValidCollectsFieldProblems(t *testing.T) {
	request := httptest.NewRequest(
		http.MethodPost,
		"/",
		bytes.NewBufferString(`{}`),
	)
	_, err := decodeValid[decodeValidTestRequest](request)

	var validation validationError
	require.ErrorAs(t, err, &validation)
	assert.Equal(t, map[string]string{
		"code": "Enter a room code.",
		"name": "Enter your name.",
	}, validation.Problems)
}

// TestEncode verifies typed values are written as JSON responses.
func TestEncode(t *testing.T) {
	response := httptest.NewRecorder()

	err := encode(response, http.StatusCreated, struct {
		Status string `json:"status"`
	}{Status: "created"})

	require.NoError(t, err)
	assert.Equal(t, http.StatusCreated, response.Code)
	assert.Equal(t, "application/json", response.Header().Get("Content-Type"))
	assert.JSONEq(t, `{"status":"created"}`, response.Body.String())
}

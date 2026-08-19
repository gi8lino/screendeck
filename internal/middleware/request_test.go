package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gi8lino/screendeck/internal/logging"
)

// TestRequestID verifies request identifiers are propagated through headers and context.
func TestRequestID(t *testing.T) {
	handler := RequestID()(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if logging.RequestID(r.Context()) != "known-request" {
			t.Error("request ID was not added to context")
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	request := httptest.NewRequest(http.MethodGet, "/", nil)
	request.Header.Set(logging.RequestIDHeader, "known-request")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Header().Get(logging.RequestIDHeader) != "known-request" {
		t.Fatal("request ID was not added to response")
	}
}

package plex

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestVerifyServerFallsBackToResourceToken verifies compatibility with servers that reject the account JWT.
func TestVerifyServerFallsBackToResourceToken(t *testing.T) {
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		if r.Header.Get("X-Plex-Token") != "resource-token" {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"MediaContainer":{"Directory":[]}}`) // nolint:errcheck
	}))
	defer server.Close()

	manager := &AuthManager{logger: slog.New(slog.NewTextHandler(io.Discard, nil))}
	token, err := manager.verifyServer(context.Background(), server.URL, "client",
		tokenCandidate{kind: "account_jwt", value: "account-jwt"},
		tokenCandidate{kind: "resource_token", value: "resource-token"},
	)
	require.NoError(t, err)
	assert.Equal(t, "resource-token", token)
	assert.Equal(t, int32(2), requests.Load())
}

// TestSafeURLRedactsSensitiveParts verifies diagnostic URLs cannot expose credentials or queries.
func TestSafeURLRedactsSensitiveParts(t *testing.T) {
	redacted := safeURL("https://user:secret@plex.example:32400/library?X-Plex-Token=secret#fragment")
	assert.Equal(t, "https://user:xxxxx@plex.example:32400/library", redacted)
}

// TestVerifyServerReturnsSentinelError verifies connection errors retain their stable category.
func TestVerifyServerReturnsSentinelError(t *testing.T) {
	manager := &AuthManager{logger: slog.New(slog.NewTextHandler(io.Discard, nil))}
	_, err := manager.verifyServer(context.Background(), "http://127.0.0.1:1", "client", tokenCandidate{kind: "account_jwt", value: "token"})
	require.ErrorIs(t, err, ErrServerContact)
}

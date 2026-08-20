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
	"time"

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

// TestStatusReturnsAuthorizedPendingSessionWithoutPolling verifies completed setup sessions return immediately.
func TestStatusReturnsAuthorizedPendingSessionWithoutPolling(t *testing.T) {
	now := time.Now().UTC()
	manager := &AuthManager{
		logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
		pending: map[string]*pendingAuth{
			"setup": {
				method:    AuthMethodLegacy,
				expiresAt: now.Add(time.Minute),
				userToken: "account-token",
				resources: map[string]resource{
					"server": {
						Name:             "Home Plex",
						ClientIdentifier: "server",
						Provides:         "server",
						Connections: []connection{
							{URI: "http://plex.test:32400", Local: true},
						},
					},
				},
			},
		},
		now: func() time.Time { return now },
	}

	status, err := manager.Status(context.Background(), "setup")
	require.NoError(t, err)
	assert.Equal(t, "authorized", status.Status)
	require.Len(t, status.Servers, 1)
	assert.Equal(t, "server", status.Servers[0].ID)
	assert.True(t, status.Servers[0].Local)
}

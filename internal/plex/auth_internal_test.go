package plex

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
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
		fmt.Fprint(w, `{"MediaContainer":{"Directory":[]}}`)
	}))
	defer server.Close()

	manager := &AuthManager{logger: slog.New(slog.NewTextHandler(io.Discard, nil))}
	token, err := manager.verifyServer(context.Background(), server.URL, "client",
		tokenCandidate{kind: "account_jwt", value: "account-jwt"},
		tokenCandidate{kind: "resource_token", value: "resource-token"},
	)
	if err != nil {
		t.Fatal(err)
	}
	if token != "resource-token" || requests.Load() != 2 {
		t.Fatalf("token=%q requests=%d", token, requests.Load())
	}
}

// TestSafeURLRedactsSensitiveParts verifies diagnostic URLs cannot expose credentials or queries.
func TestSafeURLRedactsSensitiveParts(t *testing.T) {
	redacted := safeURL("https://user:secret@plex.example:32400/library?X-Plex-Token=secret#fragment")
	if redacted != "https://user:xxxxx@plex.example:32400/library" {
		t.Fatalf("unexpected redacted URL: %q", redacted)
	}
}

// TestVerifyServerReturnsSentinelError verifies connection errors retain their stable category.
func TestVerifyServerReturnsSentinelError(t *testing.T) {
	manager := &AuthManager{logger: slog.New(slog.NewTextHandler(io.Discard, nil))}
	_, err := manager.verifyServer(context.Background(), "http://127.0.0.1:1", "client", tokenCandidate{kind: "account_jwt", value: "token"})
	if !errors.Is(err, ErrServerContact) {
		t.Fatalf("expected ErrServerContact, got %v", err)
	}
}

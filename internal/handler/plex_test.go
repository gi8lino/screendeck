package handler

import (
	"bytes"
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gi8lino/screendeck/internal/media"
	"github.com/gi8lino/screendeck/internal/plex"
	"github.com/stretchr/testify/assert"
)

type fakePlexProviderSelector struct {
	checkedProvider media.ProviderID
	activeProvider  media.ProviderID
	checkErr        error
	setActiveErr    error
}

func (f *fakePlexProviderSelector) CheckProvider(provider media.ProviderID) error {
	f.checkedProvider = provider
	return f.checkErr
}

func (f *fakePlexProviderSelector) SetActive(_ context.Context, provider media.ProviderID) error {
	f.activeProvider = provider
	return f.setActiveErr
}

type fakePlexAuthStarter struct {
	method  plex.AuthMethod
	started plex.AuthStart
	called  bool
	err     error
}

func (f *fakePlexAuthStarter) Start(_ context.Context, method plex.AuthMethod) (plex.AuthStart, error) {
	f.called = true
	f.method = method
	return f.started, f.err
}

type fakePlexAuthStatusReader struct {
	setupToken string
	status     plex.AuthStatus
	err        error
}

func (f *fakePlexAuthStatusReader) Status(_ context.Context, setupToken string) (plex.AuthStatus, error) {
	f.setupToken = setupToken
	return f.status, f.err
}

type fakePlexServerSelector struct {
	setupToken string
	serverID   string
	called     bool
	err        error
}

func (f *fakePlexServerSelector) SelectServer(_ context.Context, setupToken, serverID string) error {
	f.called = true
	f.setupToken = setupToken
	f.serverID = serverID
	return f.err
}

// TestPlexAuthRequestValidation verifies an unsupported Plex authentication method is reported.
func TestPlexAuthRequestValidation(t *testing.T) {
	t.Parallel()
	input := plexAuthRequest{Method: plex.AuthMethod("invalid")}

	problems := input.Valid(t.Context())

	assert.Contains(t, problems, "method")
}

// TestSelectPlexServerRequestValidation verifies an empty Plex server selection is reported.
func TestSelectPlexServerRequestValidation(t *testing.T) {
	t.Parallel()
	input := selectPlexServerRequest{ServerID: "  "}

	problems := input.Valid(t.Context())

	assert.Contains(t, problems, "serverId")
}

// TestStartPlexAuth verifies provider checks and Plex authorization startup.
func TestStartPlexAuth(t *testing.T) {
	t.Parallel()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	t.Run("defaults to standard authentication", func(t *testing.T) {
		t.Parallel()
		expiresAt := time.Date(2026, time.August, 28, 12, 0, 0, 0, time.UTC)
		mediaManager := &fakePlexProviderSelector{}
		auth := &fakePlexAuthStarter{started: plex.AuthStart{
			AuthURL:    "https://app.plex.tv/auth",
			SetupToken: "setup-token",
			ExpiresAt:  expiresAt,
		}}
		request := httptest.NewRequest(http.MethodPost, "/api/plex/auth", bytes.NewBufferString(`{}`))
		response := httptest.NewRecorder()

		StartPlexAuth(mediaManager, auth, logger).ServeHTTP(response, request)

		assert.Equal(t, http.StatusCreated, response.Code)
		assert.Equal(t, media.ProviderPlex, mediaManager.checkedProvider)
		assert.True(t, auth.called)
		assert.Equal(t, plex.AuthMethodStandard, auth.method)
		assert.Contains(t, response.Body.String(), `"setupToken":"setup-token"`)
	})

	t.Run("provider conflict prevents authorization", func(t *testing.T) {
		t.Parallel()
		mediaManager := &fakePlexProviderSelector{checkErr: media.ErrProviderConflict}
		auth := &fakePlexAuthStarter{}
		request := httptest.NewRequest(http.MethodPost, "/api/plex/auth", bytes.NewBufferString(`{}`))
		response := httptest.NewRecorder()

		StartPlexAuth(mediaManager, auth, logger).ServeHTTP(response, request)

		assert.Equal(t, http.StatusConflict, response.Code)
		assert.False(t, auth.called)
	})
}

// TestPlexAuthStatus verifies setup credentials are forwarded when polling authorization.
func TestPlexAuthStatus(t *testing.T) {
	t.Parallel()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	t.Run("returns authorization status", func(t *testing.T) {
		t.Parallel()
		auth := &fakePlexAuthStatusReader{status: plex.AuthStatus{
			Status:  "authorized",
			Servers: []plex.ServerInfo{{ID: "server-1", Name: "Home Plex", Owned: true}},
		}}
		request := httptest.NewRequest(http.MethodGet, "/api/plex/auth/status", nil)
		request.Header.Set("X-Setup-Token", " setup-token ")
		response := httptest.NewRecorder()

		PlexAuthStatus(auth, logger).ServeHTTP(response, request)

		assert.Equal(t, http.StatusOK, response.Code)
		assert.Equal(t, "setup-token", auth.setupToken)
		assert.Contains(t, response.Body.String(), `"status":"authorized"`)
		assert.Contains(t, response.Body.String(), `"name":"Home Plex"`)
	})

	t.Run("maps authorization error", func(t *testing.T) {
		t.Parallel()
		auth := &fakePlexAuthStatusReader{err: plex.ErrAuthorizationExpired}
		request := httptest.NewRequest(http.MethodGet, "/api/plex/auth/status", nil)
		response := httptest.NewRecorder()

		PlexAuthStatus(auth, logger).ServeHTTP(response, request)

		assert.Equal(t, http.StatusBadRequest, response.Code)
	})
}

// TestSelectPlexServer verifies authorized server selection activates Plex only after success.
func TestSelectPlexServer(t *testing.T) {
	t.Parallel()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	t.Run("selects server and activates Plex", func(t *testing.T) {
		t.Parallel()
		mediaManager := &fakePlexProviderSelector{}
		auth := &fakePlexServerSelector{}
		request := httptest.NewRequest(
			http.MethodPost,
			"/api/plex/server",
			bytes.NewBufferString(`{"serverId":" server-1 "}`),
		)
		request.Header.Set("X-Setup-Token", " setup-token ")
		response := httptest.NewRecorder()

		SelectPlexServer(mediaManager, auth, logger).ServeHTTP(response, request)

		assert.Equal(t, http.StatusOK, response.Code)
		assert.Equal(t, media.ProviderPlex, mediaManager.checkedProvider)
		assert.Equal(t, media.ProviderPlex, mediaManager.activeProvider)
		assert.True(t, auth.called)
		assert.Equal(t, "setup-token", auth.setupToken)
		assert.Equal(t, "server-1", auth.serverID)
		assert.JSONEq(t, `{"status":"connected"}`, response.Body.String())
	})

	t.Run("selection failure prevents activation", func(t *testing.T) {
		t.Parallel()
		mediaManager := &fakePlexProviderSelector{}
		auth := &fakePlexServerSelector{err: plex.ErrServerUnavailable}
		request := httptest.NewRequest(
			http.MethodPost,
			"/api/plex/server",
			bytes.NewBufferString(`{"serverId":"server-1"}`),
		)
		response := httptest.NewRecorder()

		SelectPlexServer(mediaManager, auth, logger).ServeHTTP(response, request)

		assert.Equal(t, http.StatusBadRequest, response.Code)
		assert.True(t, auth.called)
		assert.Empty(t, mediaManager.activeProvider)
	})
}

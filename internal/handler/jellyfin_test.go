package handler

import (
	"bytes"
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gi8lino/screendeck/internal/jellyfin"
	"github.com/gi8lino/screendeck/internal/media"
	"github.com/stretchr/testify/assert"
)

type fakeJellyfinProviderSelector struct {
	checkedProvider media.ProviderID
	activeProvider  media.ProviderID
	checkErr        error
	setActiveErr    error
}

func (f *fakeJellyfinProviderSelector) CheckProvider(provider media.ProviderID) error {
	f.checkedProvider = provider
	return f.checkErr
}

func (f *fakeJellyfinProviderSelector) SetActive(_ context.Context, provider media.ProviderID) error {
	f.activeProvider = provider
	return f.setActiveErr
}

type fakeJellyfinConnector struct {
	serverURL string
	username  string
	password  string
	called    bool
	err       error
}

func (f *fakeJellyfinConnector) Connect(_ context.Context, serverURL, username, password string) error {
	f.called = true
	f.serverURL = serverURL
	f.username = username
	f.password = password
	return f.err
}

// TestJellyfinConnectRequestValidation verifies invalid Jellyfin connection fields are reported.
func TestJellyfinConnectRequestValidation(t *testing.T) {
	t.Parallel()
	input := jellyfinConnectRequest{ServerURL: "jellyfin.local"}

	problems := input.Valid(t.Context())

	assert.Contains(t, problems, "serverUrl")
	assert.Contains(t, problems, "username")
}

// TestConnectJellyfin verifies provider checks, authentication, and activation ordering.
func TestConnectJellyfin(t *testing.T) {
	t.Parallel()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	t.Run("connects and activates Jellyfin", func(t *testing.T) {
		t.Parallel()
		mediaManager := &fakeJellyfinProviderSelector{}
		connector := &fakeJellyfinConnector{}
		request := httptest.NewRequest(
			http.MethodPost,
			"/api/jellyfin/connect",
			bytes.NewBufferString(`{"serverUrl":" http://jellyfin.test:8096/ ","username":" Alice ","password":"secret"}`),
		)
		response := httptest.NewRecorder()

		ConnectJellyfin(mediaManager, connector, logger).ServeHTTP(response, request)

		assert.Equal(t, http.StatusOK, response.Code)
		assert.Equal(t, media.ProviderJellyfin, mediaManager.checkedProvider)
		assert.Equal(t, media.ProviderJellyfin, mediaManager.activeProvider)
		assert.True(t, connector.called)
		assert.Equal(t, "http://jellyfin.test:8096/", connector.serverURL)
		assert.Equal(t, "Alice", connector.username)
		assert.Equal(t, "secret", connector.password)
		assert.JSONEq(t, `{"status":"connected"}`, response.Body.String())
	})

	t.Run("provider conflict prevents authentication", func(t *testing.T) {
		t.Parallel()
		mediaManager := &fakeJellyfinProviderSelector{checkErr: media.ErrProviderConflict}
		connector := &fakeJellyfinConnector{}
		request := httptest.NewRequest(
			http.MethodPost,
			"/api/jellyfin/connect",
			bytes.NewBufferString(`{"serverUrl":"http://jellyfin.test:8096","username":"Alice","password":"secret"}`),
		)
		response := httptest.NewRecorder()

		ConnectJellyfin(mediaManager, connector, logger).ServeHTTP(response, request)

		assert.Equal(t, http.StatusConflict, response.Code)
		assert.False(t, connector.called)
		assert.Empty(t, mediaManager.activeProvider)
	})

	t.Run("authentication failure prevents activation", func(t *testing.T) {
		t.Parallel()
		mediaManager := &fakeJellyfinProviderSelector{}
		connector := &fakeJellyfinConnector{err: jellyfin.ErrAuthenticationFailed}
		request := httptest.NewRequest(
			http.MethodPost,
			"/api/jellyfin/connect",
			bytes.NewBufferString(`{"serverUrl":"http://jellyfin.test:8096","username":"Alice","password":"wrong"}`),
		)
		response := httptest.NewRecorder()

		ConnectJellyfin(mediaManager, connector, logger).ServeHTTP(response, request)

		assert.Equal(t, http.StatusBadRequest, response.Code)
		assert.True(t, connector.called)
		assert.Empty(t, mediaManager.activeProvider)
	})
}

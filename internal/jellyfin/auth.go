package jellyfin

import (
	"cmp"
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"sync"

	"github.com/gi8lino/screendeck/internal/media"
	"github.com/gi8lino/screendeck/internal/requestid"
)

// AuthState contains persisted Jellyfin server and user authorization state.
type AuthState struct {
	// ServerID is Jellyfin's stable server identifier.
	ServerID string
	// ServerName is the friendly Jellyfin server name.
	ServerName string
	// ServerURL is the configured Jellyfin base URL.
	ServerURL string
	// UserID is Jellyfin's stable identifier for the configured user.
	UserID string
	// Username is the configured user's display/login name.
	Username string
	// AccessToken is the Jellyfin token returned after authentication.
	AccessToken string
	// DeviceID identifies this ScreenDeck installation to Jellyfin.
	DeviceID string
}

// AuthStore persists and restores Jellyfin authorization state.
type AuthStore interface {
	LoadJellyfinAuth(context.Context) (AuthState, error)
	SaveJellyfinAuth(context.Context, AuthState) error
}

// AuthManager coordinates Jellyfin login and authenticated catalog access.
type AuthManager struct {
	// store persists Jellyfin authorization state.
	store AuthStore
	// logger records Jellyfin setup diagnostics.
	logger *slog.Logger
	// version identifies the ScreenDeck version sent to Jellyfin.
	version string

	// mu protects mutable authorization state.
	mu sync.RWMutex
	// state contains the active persisted Jellyfin authorization.
	state AuthState
}

// newAuthManager creates a Jellyfin authentication manager and restores saved state.
func newAuthManager(
	ctx context.Context,
	store AuthStore,
	logger *slog.Logger,
	version string,
) (*AuthManager, error) {
	if logger == nil {
		logger = slog.Default()
	}
	version = cmp.Or(version, "dev")
	manager := &AuthManager{
		store:   store,
		logger:  logger,
		version: version,
	}
	state, err := store.LoadJellyfinAuth(ctx)
	if err != nil && !errors.Is(err, ErrAuthNotFound) {
		return nil, fmt.Errorf("load Jellyfin authentication: %w", err)
	}
	if err == nil {
		manager.state = state
	}
	manager.requestLogger(ctx).Info("Jellyfin authentication manager ready",
		"event", "jellyfin_auth_ready",
		"configured", authStateConfigured(state),
		"server_name", state.ServerName,
		"server_url", safeURL(state.ServerURL),
		"username", state.Username,
	)
	return manager, nil
}

// Configured reports whether a Jellyfin server and user token have been configured.
func (m *AuthManager) Configured() (bool, string) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return authStateConfigured(m.state), m.state.ServerName
}

// Connect authenticates to a Jellyfin server and persists the resulting access token.
func (m *AuthManager) Connect(ctx context.Context, serverURL, username, password string) error {
	serverURL = strings.TrimRight(strings.TrimSpace(serverURL), "/")
	username = strings.TrimSpace(username)
	if serverURL == "" || username == "" {
		return errors.New("Jellyfin server URL and username are required")
	}

	deviceID, err := m.deviceID()
	if err != nil {
		return err
	}
	serverName, publicServerID, err := PublicSystemInfo(ctx, serverURL, deviceID, m.version)
	if err != nil {
		return err
	}
	accessToken, userID, userName, authenticatedServerID, err := Authenticate(
		ctx,
		serverURL,
		username,
		password,
		deviceID,
		m.version,
	)
	if err != nil {
		return err
	}
	serverID := cmp.Or(authenticatedServerID, publicServerID)
	userName = cmp.Or(userName, username)

	state := AuthState{
		ServerID:    serverID,
		ServerName:  serverName,
		ServerURL:   serverURL,
		UserID:      userID,
		Username:    userName,
		AccessToken: accessToken,
		DeviceID:    deviceID,
	}
	if err := m.store.SaveJellyfinAuth(ctx, state); err != nil {
		return fmt.Errorf("save Jellyfin authentication: %w", err)
	}
	m.mu.Lock()
	m.state = state
	m.mu.Unlock()
	m.requestLogger(ctx).Info("Jellyfin server connected",
		"event", "jellyfin_connected",
		"server_id", state.ServerID,
		"server_name", state.ServerName,
		"server_url", safeURL(state.ServerURL),
		"username", state.Username,
	)
	return nil
}

// Libraries returns libraries from the configured Jellyfin server.
func (m *AuthManager) Libraries(ctx context.Context) ([]media.Library, error) {
	client, err := m.client()
	if err != nil {
		return nil, err
	}
	return client.Libraries(ctx)
}

// Items returns media items from the configured Jellyfin server.
func (m *AuthManager) Items(ctx context.Context, library media.Library) ([]media.Item, error) {
	client, err := m.client()
	if err != nil {
		return nil, err
	}
	return client.Items(ctx, library)
}

// Poster retrieves a poster from the configured Jellyfin server.
func (m *AuthManager) Poster(ctx context.Context, reference string) (*http.Response, error) {
	client, err := m.client()
	if err != nil {
		return nil, err
	}
	return client.Poster(ctx, reference)
}

// client creates an authenticated catalog client from the current state.
func (m *AuthManager) client() (*Client, error) {
	m.mu.RLock()
	state := m.state
	m.mu.RUnlock()
	if !authStateConfigured(state) {
		return nil, media.ErrNotConfigured
	}
	return NewClient(state.ServerURL, state.AccessToken, state.UserID, state.DeviceID, m.version)
}

// deviceID returns the persisted device identifier or creates a new one for initial setup.
func (m *AuthManager) deviceID() (string, error) {
	m.mu.RLock()
	deviceID := m.state.DeviceID
	m.mu.RUnlock()
	if deviceID != "" {
		return deviceID, nil
	}
	raw := make([]byte, 18)
	if _, err := rand.Read(raw); err != nil {
		return "", fmt.Errorf("generate Jellyfin device identifier: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(raw), nil
}

// authStateConfigured reports whether saved Jellyfin state can create an authenticated client.
func authStateConfigured(state AuthState) bool {
	return state.ServerURL != "" && state.UserID != "" && state.AccessToken != "" && state.DeviceID != ""
}

// safeURL strips query parameters and fragments before logging a Jellyfin URL.
func safeURL(rawURL string) string {
	parsed, err := parseHTTPURL(rawURL)
	if err != nil {
		if rawURL == "" {
			return ""
		}
		return "<invalid>"
	}
	parsed.RawQuery = ""
	parsed.Fragment = ""
	return parsed.Redacted()
}

// requestLogger returns a logger enriched with the current request identifier.
func (m *AuthManager) requestLogger(ctx context.Context) *slog.Logger {
	return requestid.WithLogger(m.logger, ctx)
}

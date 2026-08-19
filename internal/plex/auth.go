package plex

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/gi8lino/screendeck/internal/logging"
)

type AuthMethod string

const (
	AuthMethodLegacy AuthMethod = "legacy"
	AuthMethodJWT    AuthMethod = "jwt"
)

type AuthState struct {
	Method         AuthMethod
	ClientID       string
	KeyID          string
	PrivateKey     ed25519.PrivateKey
	UserToken      string
	TokenExpiresAt time.Time
	ServerID       string
	ServerName     string
	ServerURL      string
	ServerToken    string
}

type AuthStore interface {
	LoadPlexAuth(context.Context) (AuthState, error)
	SavePlexAuth(context.Context, AuthState) error
}

type AuthStart struct {
	AuthURL    string    `json:"authUrl"`
	SetupToken string    `json:"setupToken"`
	ExpiresAt  time.Time `json:"expiresAt"`
}

type AuthStatus struct {
	Status  string       `json:"status"`
	Servers []ServerInfo `json:"servers,omitempty"`
}

type ServerInfo struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Owned    bool   `json:"owned"`
	Local    bool   `json:"local"`
	Relay    bool   `json:"relay"`
	Platform string `json:"platform,omitempty"`
}

type resource struct {
	Name             string       `json:"name"`
	ClientIdentifier string       `json:"clientIdentifier"`
	Provides         string       `json:"provides"`
	Owned            bool         `json:"owned"`
	AccessToken      string       `json:"accessToken"`
	Platform         string       `json:"platform"`
	Connections      []connection `json:"connections"`
}

type connection struct {
	URI   string `json:"uri"`
	Local bool   `json:"local"`
	Relay bool   `json:"relay"`
}

type authorizationPINResponse struct {
	ID        int64  `json:"id"`
	Code      string `json:"code"`
	ExpiresIn int    `json:"expiresIn"`
}

type authorizationStatusResponse struct {
	AuthToken string `json:"authToken"`
}

type authNonceResponse struct {
	Nonce string `json:"nonce"`
}

type authTokenResponse struct {
	AuthToken string `json:"auth_token"`
}

type deviceJWK struct {
	KeyType   string `json:"kty"`
	Curve     string `json:"crv"`
	X         string `json:"x"`
	KeyID     string `json:"kid"`
	Use       string `json:"use"`
	Algorithm string `json:"alg"`
}

type authorizationPINRequest struct {
	JWK    *deviceJWK `json:"jwk,omitempty"`
	Strong bool       `json:"strong"`
}

type legacyResourcesResponse struct {
	Devices []legacyResource `xml:"Device"`
}

type legacyResource struct {
	Name             string             `xml:"name,attr"`
	ClientIdentifier string             `xml:"clientIdentifier,attr"`
	Provides         string             `xml:"provides,attr"`
	Owned            int                `xml:"owned,attr"`
	AccessToken      string             `xml:"accessToken,attr"`
	Platform         string             `xml:"platform,attr"`
	Connections      []legacyConnection `xml:"Connection"`
}

type legacyConnection struct {
	URI   string `xml:"uri,attr"`
	Local int    `xml:"local,attr"`
	Relay int    `xml:"relay,attr"`
}

type tokenRefreshRequest struct {
	JWT string `json:"jwt"`
}

type tokenClaims struct {
	ExpiresAt int64 `json:"exp"`
}

type tokenCandidate struct {
	kind  string
	value string
}

type pendingAuth struct {
	method     AuthMethod
	clientID   string
	keyID      string
	privateKey ed25519.PrivateKey
	pinID      int64
	expiresAt  time.Time
	userToken  string
	tokenExp   time.Time
	resources  map[string]resource
}

type AuthManager struct {
	store             AuthStore
	logger            *slog.Logger
	cloudBase         *url.URL
	serverURLOverride string
	experimental      bool
	httpClient        *http.Client

	mu        sync.Mutex
	refreshMu sync.Mutex
	state     AuthState
	pending   map[string]*pendingAuth
	now       func() time.Time
}

// NewAuthManager creates an authentication manager and restores saved state.
func NewAuthManager(ctx context.Context, store AuthStore, logger *slog.Logger, cloudURL, serverURLOverride string, experimental bool) (*AuthManager, error) {
	if logger == nil {
		logger = slog.Default()
	}
	base, err := url.Parse(strings.TrimRight(cloudURL, "/"))
	if err != nil || base.Host == "" || (base.Scheme != "http" && base.Scheme != "https") {
		return nil, ErrInvalidCloudURL
	}
	serverURLOverride = strings.TrimRight(serverURLOverride, "/")
	if serverURLOverride != "" {
		override, parseErr := url.Parse(serverURLOverride)
		if parseErr != nil || override.Host == "" || (override.Scheme != "http" && override.Scheme != "https") {
			return nil, ErrInvalidServerURLOverride
		}
	}
	manager := &AuthManager{
		store:             store,
		logger:            logger,
		cloudBase:         base,
		serverURLOverride: serverURLOverride,
		experimental:      experimental,
		httpClient:        &http.Client{Timeout: 20 * time.Second},
		pending:           make(map[string]*pendingAuth),
		now:               time.Now,
	}
	state, err := store.LoadPlexAuth(ctx)
	if err == nil {
		manager.state = state
	} else if !errors.Is(err, ErrAuthNotFound) {
		return nil, fmt.Errorf("load Plex authentication: %w", err)
	}
	manager.requestLogger(ctx).Info(
		"Plex authentication manager ready",
		"event", "plex_auth_ready",
		"configured", state.ServerURL != "" && state.ServerToken != "",
		"server_name", state.ServerName,
		"discovered_url", safeURL(state.ServerURL),
		"effective_url", safeURL(manager.serverURL(state.ServerURL)),
		"url_override", serverURLOverride != "",
		"experimental", experimental,
		"auth_method", state.Method,
		"token_expires_at", state.TokenExpiresAt,
	)
	return manager, nil
}

// Configured reports whether a Plex server has been configured.
func (m *AuthManager) Configured() (bool, string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.state.ServerURL != "" && m.state.ServerToken != "", m.state.ServerName
}

// Start begins a Plex device authorization flow.
func (m *AuthManager) Start(ctx context.Context, method AuthMethod) (AuthStart, error) {
	if configured, _ := m.Configured(); configured {
		return AuthStart{}, ErrAlreadyConfigured
	}
	if method == "" {
		method = AuthMethodLegacy
	}
	if method != AuthMethodLegacy && method != AuthMethodJWT {
		return AuthStart{}, ErrInvalidAuthMethod
	}
	if method == AuthMethodJWT && !m.experimental {
		return AuthStart{}, ErrExperimentalAuthDisabled
	}
	logger := m.requestLogger(ctx)
	logger.Info("starting Plex authorization",
		"event", "plex_auth_starting",
		"auth_method", method,
	)
	clientID, err := randomToken(18)
	if err != nil {
		return AuthStart{}, fmt.Errorf("generate Plex client identifier: %w", err)
	}
	var keyID string
	var privateKey ed25519.PrivateKey
	var body any
	query := url.Values{}
	if method == AuthMethodJWT {
		publicKey, generatedPrivateKey, keyErr := ed25519.GenerateKey(rand.Reader)
		if keyErr != nil {
			return AuthStart{}, fmt.Errorf("generate Plex device key: %w", keyErr)
		}
		privateKey = generatedPrivateKey
		digest := sha256.Sum256(publicKey)
		keyID = base64.RawURLEncoding.EncodeToString(digest[:12])
		body = authorizationPINRequest{
			JWK: &deviceJWK{
				KeyType: "OKP", Curve: "Ed25519", X: base64.RawURLEncoding.EncodeToString(publicKey),
				KeyID: keyID, Use: "sig", Algorithm: "EdDSA",
			},
			Strong: true,
		}
	} else {
		query.Set("strong", "true")
	}
	var pin authorizationPINResponse
	if err := m.cloudJSON(ctx, http.MethodPost, "/api/v2/pins", clientID, "", query, body, &pin); err != nil {
		return AuthStart{}, err
	}
	if pin.ID == 0 || pin.Code == "" {
		return AuthStart{}, ErrInvalidAuthorizationPIN
	}
	ttl := time.Duration(pin.ExpiresIn) * time.Second
	if ttl <= 0 || ttl > 30*time.Minute {
		ttl = 10 * time.Minute
	}
	setupToken, err := randomToken(32)
	if err != nil {
		return AuthStart{}, fmt.Errorf("generate Plex setup token: %w", err)
	}
	pending := &pendingAuth{method: method, clientID: clientID, keyID: keyID, privateKey: privateKey, pinID: pin.ID, expiresAt: m.now().Add(ttl)}
	m.mu.Lock()
	for token, auth := range m.pending {
		if m.now().After(auth.expiresAt) {
			delete(m.pending, token)
		}
	}
	m.pending[setupToken] = pending
	m.mu.Unlock()
	logger.Info(
		"Plex authorization started",
		"event", "plex_auth_started",
		"pin_id", pin.ID,
		"auth_method", method,
		"expires_at", pending.expiresAt,
	)
	params := url.Values{
		"clientID":                    {clientID},
		"code":                        {pin.Code},
		"context[device][product]":    {"ScreenDeck"},
		"context[device][version]":    {"1.0"},
		"context[device][platform]":   {"Web"},
		"context[device][deviceName]": {"ScreenDeck"},
		"context[device][model]":      {"Plex OAuth"},
		"context[device][layout]":     {"desktop"},
	}
	return AuthStart{AuthURL: "https://app.plex.tv/auth#?" + params.Encode(), SetupToken: setupToken, ExpiresAt: pending.expiresAt}, nil
}

// Status polls and reports a Plex device authorization flow.
func (m *AuthManager) Status(ctx context.Context, setupToken string) (AuthStatus, error) {
	logger := m.requestLogger(ctx)
	m.mu.Lock()
	pending := m.pending[setupToken]
	m.mu.Unlock()
	if pending == nil || m.now().After(pending.expiresAt) {
		logger.Warn("Plex authorization session expired",
			"event", "plex_auth_expired",
		)
		return AuthStatus{}, ErrAuthorizationExpired
	}
	if pending.userToken == "" {
		logger.Debug("polling Plex authorization",
			"event", "plex_auth_polling",
			"pin_id", pending.pinID,
		)
		query := url.Values{}
		if pending.method == AuthMethodJWT {
			deviceJWT, err := signDeviceJWT(pending.privateKey, pending.keyID, map[string]any{
				"aud": "plex.tv", "iss": pending.clientID,
				"iat": m.now().Unix(), "exp": m.now().Add(5 * time.Minute).Unix(),
			})
			if err != nil {
				return AuthStatus{}, fmt.Errorf("sign Plex device JWT: %w", err)
			}
			query.Set("deviceJWT", deviceJWT)
		}
		var pin authorizationStatusResponse
		if err := m.cloudJSON(ctx, http.MethodGet, fmt.Sprintf("/api/v2/pins/%d", pending.pinID), pending.clientID, "", query, nil, &pin); err != nil {
			return AuthStatus{}, err
		}
		if pin.AuthToken == "" {
			logger.Debug("Plex authorization is waiting for the user",
				"event", "plex_auth_waiting",
				"pin_id", pending.pinID,
			)
			return AuthStatus{Status: "waiting"}, nil
		}
		resources, err := m.resources(ctx, pending.method, pending.clientID, pin.AuthToken)
		if err != nil {
			return AuthStatus{}, err
		}
		m.mu.Lock()
		pending.userToken = pin.AuthToken
		if pending.method == AuthMethodJWT {
			pending.tokenExp = tokenExpiry(pin.AuthToken, m.now().Add(7*24*time.Hour))
		}
		pending.resources = resources
		m.mu.Unlock()
		logger.Info(
			"Plex authorization completed",
			"event", "plex_auth_authorized",
			"server_count", len(resources),
			"auth_method", pending.method,
			"token_expires_at", pending.tokenExp,
		)
	}
	return AuthStatus{Status: "authorized", Servers: serverInfos(pending.resources)}, nil
}

// SelectServer persists the selected Plex server connection.
func (m *AuthManager) SelectServer(ctx context.Context, setupToken, serverID string) error {
	logger := m.requestLogger(ctx)
	m.mu.Lock()
	pending := m.pending[setupToken]
	m.mu.Unlock()
	if pending == nil || pending.userToken == "" || m.now().After(pending.expiresAt) {
		return ErrAuthorizationIncomplete
	}
	server, ok := pending.resources[serverID]
	if !ok {
		logger.Warn("selected Plex server is unavailable",
			"event", "plex_server_unavailable",
			"server_id", serverID,
		)
		return ErrServerUnavailable
	}
	logger.Info(
		"selecting Plex server",
		"event", "plex_server_selecting",
		"server_id", server.ClientIdentifier,
		"server_name", server.Name,
		"owned", server.Owned,
		"platform", server.Platform,
		"connection_count", len(server.Connections),
	)
	for _, candidate := range server.Connections {
		logger.Debug(
			"discovered Plex connection",
			"event", "plex_connection_discovered",
			"server_id", server.ClientIdentifier,
			"url", safeURL(candidate.URI),
			"local", candidate.Local,
			"relay", candidate.Relay,
		)
	}
	selected, ok := preferredConnection(server.Connections)
	if !ok {
		return ErrNoUsableConnection
	}
	effectiveURL := m.serverURL(selected.URI)
	logger.Info(
		"selected Plex connection",
		"event", "plex_connection_selected",
		"server_id", server.ClientIdentifier,
		"discovered_url", safeURL(selected.URI),
		"effective_url", safeURL(effectiveURL),
		"local", selected.Local,
		"relay", selected.Relay,
		"url_override", m.serverURLOverride != "",
	)
	candidates := []tokenCandidate{{kind: "resource_token", value: server.AccessToken}}
	if pending.method == AuthMethodJWT {
		candidates = append([]tokenCandidate{{kind: "account_jwt", value: pending.userToken}}, candidates...)
	} else {
		candidates = append(candidates, tokenCandidate{kind: "account_token", value: pending.userToken})
	}
	serverToken, err := m.verifyServer(ctx, effectiveURL, pending.clientID, candidates...)
	if err != nil {
		logger.Error("Plex server verification failed",
			"event", "plex_server_verification_failed",
			"server_id", server.ClientIdentifier,
			"url", safeURL(effectiveURL),
			"error", err,
		)
		return fmt.Errorf("%w: %w", ErrServerVerification, err)
	}
	state := AuthState{
		Method:   pending.method,
		ClientID: pending.clientID, KeyID: pending.keyID, PrivateKey: pending.privateKey,
		UserToken: pending.userToken, TokenExpiresAt: pending.tokenExp,
		ServerID: server.ClientIdentifier, ServerName: server.Name, ServerURL: selected.URI, ServerToken: serverToken,
	}
	if err := m.store.SavePlexAuth(ctx, state); err != nil {
		return fmt.Errorf("save Plex authentication: %w", err)
	}
	m.mu.Lock()
	m.state = state
	delete(m.pending, setupToken)
	m.mu.Unlock()
	logger.Info("Plex server selected",
		"event", "plex_server_selected",
		"server_id", state.ServerID,
		"server_name", state.ServerName,
		"effective_url", safeURL(effectiveURL),
	)
	return nil
}

// Libraries returns libraries from the configured Plex server.
func (m *AuthManager) Libraries(ctx context.Context) ([]Library, error) {
	client, err := m.client(ctx)
	if err != nil {
		return nil, err
	}
	return client.Libraries(ctx)
}

// Items returns media from the configured Plex server.
func (m *AuthManager) Items(ctx context.Context, library Library) ([]Item, error) {
	client, err := m.client(ctx)
	if err != nil {
		return nil, err
	}
	return client.Items(ctx, library)
}

// Poster retrieves a poster from the configured Plex server.
func (m *AuthManager) Poster(ctx context.Context, path string) (*http.Response, error) {
	client, err := m.client(ctx)
	if err != nil {
		return nil, err
	}
	return client.Poster(ctx, path)
}

// Refresh renews Plex authentication when necessary.
func (m *AuthManager) Refresh(ctx context.Context) error {
	return m.refresh(ctx, true)
}

// refresh renews and persists Plex authentication state.
func (m *AuthManager) refresh(ctx context.Context, force bool) error {
	m.refreshMu.Lock()
	defer m.refreshMu.Unlock()
	logger := m.requestLogger(ctx)
	m.mu.Lock()
	state := m.state
	m.mu.Unlock()
	if state.UserToken == "" {
		return ErrAuthNotFound
	}
	if state.Method != AuthMethodJWT {
		logger.Debug("Plex token refresh is not required for legacy authentication",
			"event", "plex_token_refresh_skipped",
			"auth_method", state.Method,
		)
		return nil
	}
	if !force && !state.TokenExpiresAt.IsZero() && state.TokenExpiresAt.Sub(m.now()) >= 12*time.Hour {
		logger.Debug("Plex token refresh not required",
			"event", "plex_token_refresh_skipped",
			"token_expires_at", state.TokenExpiresAt,
		)
		return nil
	}
	logger.Info("refreshing Plex authentication",
		"event", "plex_token_refreshing",
		"server_id", state.ServerID,
		"token_expires_at", state.TokenExpiresAt,
	)
	var nonceResponse authNonceResponse
	if err := m.cloudJSON(ctx, http.MethodGet, "/api/v2/auth/nonce", state.ClientID, "", nil, nil, &nonceResponse); err != nil {
		return err
	}
	deviceJWT, err := signDeviceJWT(state.PrivateKey, state.KeyID, map[string]any{
		"nonce": nonceResponse.Nonce, "scope": "friendly_name", "aud": "plex.tv", "iss": state.ClientID,
		"iat": m.now().Unix(), "exp": m.now().Add(5 * time.Minute).Unix(),
	})
	if err != nil {
		return fmt.Errorf("sign Plex refresh JWT: %w", err)
	}
	var tokenResponse authTokenResponse
	if err := m.cloudJSON(ctx, http.MethodPost, "/api/v2/auth/token", state.ClientID, "", nil, tokenRefreshRequest{JWT: deviceJWT}, &tokenResponse); err != nil {
		return err
	}
	if tokenResponse.AuthToken == "" {
		return ErrEmptyRefreshedToken
	}
	resources, err := m.resources(ctx, AuthMethodJWT, state.ClientID, tokenResponse.AuthToken)
	if err != nil {
		return err
	}
	resourceToken := ""
	if server, ok := resources[state.ServerID]; ok {
		if selected, found := preferredConnection(server.Connections); found {
			state.ServerURL = selected.URI
		}
		resourceToken = server.AccessToken
	}
	effectiveURL := m.serverURL(state.ServerURL)
	serverToken, err := m.verifyServer(ctx, effectiveURL, state.ClientID,
		tokenCandidate{kind: "account_jwt", value: tokenResponse.AuthToken},
		tokenCandidate{kind: "resource_token", value: resourceToken},
	)
	if err != nil {
		logger.Error("refreshed Plex token verification failed",
			"event", "plex_token_refresh_verification_failed",
			"server_id", state.ServerID,
			"url", safeURL(effectiveURL),
			"error", err,
		)
		return fmt.Errorf("%w: %w", ErrAuthenticationRefresh, err)
	}
	state.UserToken = tokenResponse.AuthToken
	state.ServerToken = serverToken
	state.TokenExpiresAt = tokenExpiry(tokenResponse.AuthToken, m.now().Add(7*24*time.Hour))
	if err := m.store.SavePlexAuth(ctx, state); err != nil {
		return fmt.Errorf("save refreshed Plex authentication: %w", err)
	}
	m.mu.Lock()
	m.state = state
	m.mu.Unlock()
	logger.Info("Plex authentication refreshed",
		"event", "plex_token_refreshed",
		"server_id", state.ServerID,
		"effective_url", safeURL(effectiveURL),
		"token_expires_at", state.TokenExpiresAt,
	)
	return nil
}

// client returns an authenticated client for the selected Plex server.
func (m *AuthManager) client(ctx context.Context) (*Client, error) {
	m.mu.Lock()
	state := m.state
	m.mu.Unlock()
	if state.ServerURL == "" || state.ServerToken == "" {
		return nil, ErrNotConfigured
	}
	if state.Method == AuthMethodJWT && (state.TokenExpiresAt.IsZero() || state.TokenExpiresAt.Sub(m.now()) < 12*time.Hour) {
		if err := m.refresh(ctx, false); err != nil {
			return nil, fmt.Errorf("%w: %w", ErrAuthenticationRefresh, err)
		}
		m.mu.Lock()
		state = m.state
		m.mu.Unlock()
	}
	return NewWithClientID(m.serverURL(state.ServerURL), state.ServerToken, state.ClientID)
}

// serverURL returns the runtime override or the discovered Plex URL.
func (m *AuthManager) serverURL(discovered string) string {
	if m.serverURLOverride != "" {
		return m.serverURLOverride
	}
	return discovered
}

// safeURL removes credentials, query parameters, and fragments from a URL used in logs.
func safeURL(rawURL string) string {
	if rawURL == "" {
		return ""
	}
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return "<invalid>"
	}
	parsed.RawQuery = ""
	parsed.Fragment = ""
	return parsed.Redacted()
}

// verifyServer returns the first token accepted by the selected Plex server.
func (m *AuthManager) verifyServer(ctx context.Context, serverURL, clientID string, candidates ...tokenCandidate) (string, error) {
	logger := m.requestLogger(ctx)
	var failures []error
	seen := make(map[string]struct{}, len(candidates))
	for _, candidate := range candidates {
		if candidate.value == "" {
			logger.Debug("skipping empty Plex token candidate",
				"event", "plex_token_candidate_skipped",
				"token_kind", candidate.kind,
			)
			continue
		}
		if _, exists := seen[candidate.value]; exists {
			logger.Debug("skipping duplicate Plex token candidate",
				"event", "plex_token_candidate_skipped",
				"token_kind", candidate.kind,
			)
			continue
		}
		seen[candidate.value] = struct{}{}
		logger.Info("verifying Plex server access",
			"event", "plex_server_verifying",
			"url", safeURL(serverURL),
			"token_kind", candidate.kind,
		)
		started := time.Now()
		client, err := NewWithClientID(serverURL, candidate.value, clientID)
		if err == nil {
			_, err = client.Libraries(ctx)
		}
		if err == nil {
			logger.Info("Plex server access verified",
				"event", "plex_server_verified",
				"url", safeURL(serverURL),
				"token_kind", candidate.kind,
				"duration_ms", time.Since(started).Milliseconds(),
			)
			return candidate.value, nil
		}
		logger.Warn("Plex server rejected token candidate",
			"event", "plex_server_verification_attempt_failed",
			"url", safeURL(serverURL),
			"token_kind", candidate.kind,
			"duration_ms", time.Since(started).Milliseconds(),
			"error", err,
		)
		failures = append(failures, fmt.Errorf("%s: %w", candidate.kind, err))
	}
	if len(failures) == 0 {
		return "", ErrMissingToken
	}
	return "", errors.Join(failures...)
}

// resources retrieves the Plex resources available to an account.
func (m *AuthManager) resources(ctx context.Context, method AuthMethod, clientID, token string) (map[string]resource, error) {
	if method == AuthMethodLegacy {
		return m.legacyResources(ctx, clientID, token)
	}
	logger := m.requestLogger(ctx)
	query := url.Values{"includeHttps": {"1"}, "includeRelay": {"1"}, "includeIPv6": {"1"}}
	var response []resource
	if err := m.cloudJSON(ctx, http.MethodGet, "/api/v2/resources", clientID, token, query, nil, &response); err != nil {
		return nil, err
	}
	resources := make(map[string]resource)
	for _, item := range response {
		if item.ClientIdentifier != "" && containsProvide(item.Provides, "server") && len(item.Connections) > 0 {
			resources[item.ClientIdentifier] = item
			logger.Debug("discovered Plex server",
				"event", "plex_server_discovered",
				"server_id", item.ClientIdentifier,
				"server_name", item.Name,
				"owned", item.Owned,
				"platform", item.Platform,
				"connection_count", len(item.Connections),
				"has_resource_token", item.AccessToken != "",
			)
		}
	}
	if len(resources) == 0 {
		return nil, ErrNoServers
	}
	logger.Info("Plex servers discovered",
		"event", "plex_servers_discovered",
		"server_count", len(resources),
	)
	return resources, nil
}

// legacyResources retrieves Plex resources with server-compatible legacy tokens.
func (m *AuthManager) legacyResources(ctx context.Context, clientID, token string) (map[string]resource, error) {
	logger := m.requestLogger(ctx)
	query := url.Values{"includeHttps": {"1"}, "includeRelay": {"1"}, "includeIPv6": {"1"}}
	var response legacyResourcesResponse
	if err := m.cloudXML(ctx, http.MethodGet, "/api/resources", clientID, token, query, &response); err != nil {
		return nil, err
	}
	resources := make(map[string]resource)
	for _, item := range response.Devices {
		connections := make([]connection, 0, len(item.Connections))
		for _, candidate := range item.Connections {
			connections = append(connections, connection{URI: candidate.URI, Local: candidate.Local == 1, Relay: candidate.Relay == 1})
		}
		converted := resource{
			Name: item.Name, ClientIdentifier: item.ClientIdentifier, Provides: item.Provides,
			Owned: item.Owned == 1, AccessToken: item.AccessToken, Platform: item.Platform, Connections: connections,
		}
		if converted.ClientIdentifier != "" && containsProvide(converted.Provides, "server") && len(converted.Connections) > 0 {
			resources[converted.ClientIdentifier] = converted
			logger.Debug("discovered Plex server",
				"event", "plex_server_discovered",
				"server_id", converted.ClientIdentifier,
				"server_name", converted.Name,
				"owned", converted.Owned,
				"platform", converted.Platform,
				"connection_count", len(converted.Connections),
				"has_resource_token", converted.AccessToken != "",
				"auth_method", AuthMethodLegacy,
			)
		}
	}
	if len(resources) == 0 {
		return nil, ErrNoServers
	}
	logger.Info("Plex servers discovered",
		"event", "plex_servers_discovered",
		"server_count", len(resources),
		"auth_method", AuthMethodLegacy,
	)
	return resources, nil
}

// cloudJSON sends a JSON request to the Plex cloud API.
func (m *AuthManager) cloudJSON(ctx context.Context, method, path, clientID, token string, query url.Values, body, target any) error {
	logger := m.requestLogger(ctx)
	started := time.Now()
	logger.Debug("sending Plex authentication request",
		"event", "plex_cloud_request",
		"method", method,
		"path", path,
		"authenticated", token != "",
	)
	u := *m.cloudBase
	u.Path = strings.TrimRight(u.Path, "/") + path
	u.RawQuery = query.Encode()
	var reader io.Reader
	if body != nil {
		encoded, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("encode Plex authentication request: %w", err)
		}
		reader = bytes.NewReader(encoded)
	}
	req, err := http.NewRequestWithContext(ctx, method, u.String(), reader)
	if err != nil {
		return fmt.Errorf("create Plex authentication request: %w", err)
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Plex-Product", "ScreenDeck")
	req.Header.Set("X-Plex-Version", "1.0")
	req.Header.Set("X-Plex-Client-Identifier", clientID)
	if token != "" {
		req.Header.Set("X-Plex-Token", token)
	}
	response, err := m.httpClient.Do(req)
	if err != nil {
		logger.Error("Plex authentication request failed", "event", "plex_cloud_request_failed", "method", method, "path", path, "duration_ms", time.Since(started).Milliseconds(), "error", err)
		return fmt.Errorf("%w: %s %s: %w", ErrCloudUnavailable, method, path, err)
	}
	defer response.Body.Close()
	logger.Debug("Plex authentication response received",
		"event", "plex_cloud_response",
		"method", method,
		"path", path,
		"status", response.StatusCode,
		"duration_ms", time.Since(started).Milliseconds(),
	)
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		message, _ := io.ReadAll(io.LimitReader(response.Body, 1024))
		return fmt.Errorf("%w: %s %s: %s: %s", ErrCloudResponse, method, path, response.Status, strings.TrimSpace(string(message)))
	}
	if target == nil {
		return nil
	}
	if err := json.NewDecoder(io.LimitReader(response.Body, 8<<20)).Decode(target); err != nil {
		return fmt.Errorf("%w: %s %s: %w", ErrCloudDecode, method, path, err)
	}
	return nil
}

// cloudXML sends an XML request to the legacy Plex cloud API.
func (m *AuthManager) cloudXML(ctx context.Context, method, path, clientID, token string, query url.Values, target any) error {
	logger := m.requestLogger(ctx)
	started := time.Now()
	logger.Debug("sending Plex authentication request",
		"event", "plex_cloud_request",
		"method", method,
		"path", path, "authenticated",
		token != "",
	)
	u := *m.cloudBase
	u.Path = strings.TrimRight(u.Path, "/") + path
	u.RawQuery = query.Encode()
	req, err := http.NewRequestWithContext(ctx, method, u.String(), nil)
	if err != nil {
		return fmt.Errorf("create Plex authentication request: %w", err)
	}
	req.Header.Set("Accept", "application/xml")
	req.Header.Set("X-Plex-Product", "ScreenDeck")
	req.Header.Set("X-Plex-Version", "1.0")
	req.Header.Set("X-Plex-Client-Identifier", clientID)
	if token != "" {
		req.Header.Set("X-Plex-Token", token)
	}
	response, err := m.httpClient.Do(req)
	if err != nil {
		logger.Error("Plex authentication request failed", "event", "plex_cloud_request_failed", "method", method, "path", path, "duration_ms", time.Since(started).Milliseconds(), "error", err)
		return fmt.Errorf("%w: %s %s: %w", ErrCloudUnavailable, method, path, err)
	}
	defer response.Body.Close()
	logger.Debug("Plex authentication response received", "event", "plex_cloud_response", "method", method, "path", path, "status", response.StatusCode, "duration_ms", time.Since(started).Milliseconds())
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		message, _ := io.ReadAll(io.LimitReader(response.Body, 1024))
		return fmt.Errorf("%w: %s %s: %s: %s", ErrCloudResponse, method, path, response.Status, strings.TrimSpace(string(message)))
	}
	if err := xml.NewDecoder(io.LimitReader(response.Body, 8<<20)).Decode(target); err != nil {
		return fmt.Errorf("%w: %s %s: %w", ErrCloudDecode, method, path, err)
	}
	return nil
}

// requestLogger returns a logger enriched with request context.
func (m *AuthManager) requestLogger(ctx context.Context) *slog.Logger {
	return logging.WithRequestIDLogger(m.logger, ctx)
}

// signDeviceJWT signs Plex device claims as an EdDSA JWT.
func signDeviceJWT(privateKey ed25519.PrivateKey, keyID string, claims map[string]any) (string, error) {
	header, err := json.Marshal(map[string]string{"alg": "EdDSA", "kid": keyID, "typ": "JWT"})
	if err != nil {
		return "", err
	}
	payload, err := json.Marshal(claims)
	if err != nil {
		return "", err
	}
	unsigned := base64.RawURLEncoding.EncodeToString(header) + "." + base64.RawURLEncoding.EncodeToString(payload)
	signature := ed25519.Sign(privateKey, []byte(unsigned))
	return unsigned + "." + base64.RawURLEncoding.EncodeToString(signature), nil
}

// tokenExpiry extracts a JWT expiration time or returns a fallback.
func tokenExpiry(token string, fallback time.Time) time.Time {
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return fallback
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return fallback
	}
	var claims tokenClaims
	if json.Unmarshal(payload, &claims) != nil || claims.ExpiresAt == 0 {
		return fallback
	}
	return time.Unix(claims.ExpiresAt, 0).UTC()
}

// serverInfos converts Plex resources into selectable server summaries.
func serverInfos(resources map[string]resource) []ServerInfo {
	servers := make([]ServerInfo, 0, len(resources))
	for _, server := range resources {
		selected, _ := preferredConnection(server.Connections)
		servers = append(servers, ServerInfo{ID: server.ClientIdentifier, Name: server.Name, Owned: server.Owned, Local: selected.Local, Relay: selected.Relay, Platform: server.Platform})
	}
	sort.Slice(servers, func(i, j int) bool {
		if servers[i].Local != servers[j].Local {
			return servers[i].Local
		}
		if servers[i].Owned != servers[j].Owned {
			return servers[i].Owned
		}
		return servers[i].Name < servers[j].Name
	})
	return servers
}

// preferredConnection chooses the best reachable Plex connection.
func preferredConnection(connections []connection) (connection, bool) {
	if len(connections) == 0 {
		return connection{}, false
	}
	copyOf := append([]connection(nil), connections...)
	sort.SliceStable(copyOf, func(i, j int) bool {
		score := func(c connection) int {
			if c.Local && !c.Relay {
				return 0
			}
			if !c.Relay {
				return 1
			}
			return 2
		}
		return score(copyOf[i]) < score(copyOf[j])
	})
	for _, candidate := range copyOf {
		if parsed, err := url.Parse(candidate.URI); err == nil && parsed.Host != "" && (parsed.Scheme == "http" || parsed.Scheme == "https") {
			return candidate, true
		}
	}
	return connection{}, false
}

// containsProvide reports whether a Plex resource offers a capability.
func containsProvide(provides, wanted string) bool {
	for value := range strings.SplitSeq(provides, ",") {
		if strings.TrimSpace(value) == wanted {
			return true
		}
	}
	return false
}

// randomToken returns a cryptographically secure URL-safe token.
func randomToken(size int) (string, error) {
	raw := make([]byte, size)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(raw), nil
}

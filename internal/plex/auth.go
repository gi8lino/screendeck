package plex

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/gi8lino/screendeck/internal/media"
	"github.com/gi8lino/screendeck/internal/requestid"
)

// AuthMethod identifies a supported Plex authorization flow.
type AuthMethod string

const (
	// AuthMethodStandard uses Plex's standard PIN authorization flow.
	AuthMethodStandard AuthMethod = "standard"
	// AuthMethodJWT uses the experimental Ed25519 device-JWT authorization flow.
	AuthMethodJWT AuthMethod = "jwt"
)

// AuthState contains the persisted Plex authorization and selected-server state.
type AuthState struct {
	// Method identifies the selected Plex authorization flow.
	Method AuthMethod
	// ClientID is the Plex client identifier associated with the authorization.
	ClientID string
	// KeyID identifies the registered experimental device key.
	KeyID string
	// PrivateKey is the Ed25519 device key used for experimental JWT authorization.
	PrivateKey ed25519.PrivateKey
	// UserToken is the Plex account token returned by authorization.
	UserToken string
	// TokenExpiresAt is the known expiry time of the account token.
	TokenExpiresAt time.Time
	// ServerID identifies the Plex server selected by the user.
	ServerID string
	// ServerName is the friendly name of the selected Plex server.
	ServerName string
	// ServerURL is the discovered URL of the selected Plex server.
	ServerURL string
	// ServerToken is the token verified for the selected Plex server.
	ServerToken string
}

// AuthStore persists and restores Plex authorization state.
type AuthStore interface {
	LoadPlexAuth(context.Context) (AuthState, error)
	SavePlexAuth(context.Context, AuthState) error
}

// AuthStart describes a newly started Plex authorization session.
type AuthStart struct {
	// AuthURL is the Plex URL the user opens to authorize ScreenDeck.
	AuthURL string `json:"authUrl"`
	// SetupToken authenticates temporary setup polling and server selection.
	SetupToken string `json:"setupToken"`
	// ExpiresAt is the time at which the temporary setup session expires.
	ExpiresAt time.Time `json:"expiresAt"`
}

// AuthStatus reports the current Plex authorization state.
type AuthStatus struct {
	// Status is the current authorization state.
	Status string `json:"status"`
	// Servers contains Plex servers available after authorization.
	Servers []ServerInfo `json:"servers,omitempty"`
}

// ServerInfo describes a Plex Media Server available for selection.
type ServerInfo struct {
	// ID is the stable identifier.
	ID string `json:"id"`
	// Name is the display name.
	Name string `json:"name"`
	// Owned reports whether the Plex account owns the server.
	Owned bool `json:"owned"`
	// Local reports whether the selected connection is local.
	Local bool `json:"local"`
	// Relay reports whether the selected connection uses Plex Relay.
	Relay bool `json:"relay"`
	// Platform is the server platform reported by Plex.
	Platform string `json:"platform,omitempty"`
}

// resource models a server resource returned by the Plex cloud API.
type resource struct {
	// Name is the display name.
	Name string `json:"name"`
	// ClientIdentifier is Plex's stable identifier for the resource.
	ClientIdentifier string `json:"clientIdentifier"`
	// Provides lists capabilities advertised by the resource.
	Provides string `json:"provides"`
	// Owned reports whether the Plex account owns the server.
	Owned bool `json:"owned"`
	// AccessToken is the resource-specific Plex token.
	AccessToken string `json:"accessToken"`
	// Platform is the server platform reported by Plex.
	Platform string `json:"platform"`
	// Connections lists advertised endpoints for the resource.
	Connections []connection `json:"connections"`
}

// connection describes a connection endpoint advertised for a Plex resource.
type connection struct {
	// URI is the advertised connection URL.
	URI string `json:"uri"`
	// Local reports whether the selected connection is local.
	Local bool `json:"local"`
	// Relay reports whether the selected connection uses Plex Relay.
	Relay bool `json:"relay"`
}

// authorizationPINResponse decodes the Plex PIN creation response.
type authorizationPINResponse struct {
	// ID is the stable identifier.
	ID int64 `json:"id"`
	// Code is the Plex PIN code used by the browser authorization flow.
	Code string `json:"code"`
	// ExpiresIn is the PIN lifetime in seconds.
	ExpiresIn int `json:"expiresIn"`
}

// authorizationStatusResponse decodes the Plex PIN status response.
type authorizationStatusResponse struct {
	// AuthToken is the token returned after successful authorization.
	AuthToken string `json:"authToken"`
}

// authNonceResponse decodes the nonce returned for JWT refresh.
type authNonceResponse struct {
	// Nonce is the one-time value used when signing a refresh JWT.
	Nonce string `json:"nonce"`
}

// authTokenResponse decodes the refreshed Plex account token.
type authTokenResponse struct {
	// AuthToken is the token returned after successful authorization.
	AuthToken string `json:"auth_token"`
}

// deviceJWK represents the Ed25519 public key registered for experimental JWT authorization.
type deviceJWK struct {
	// KeyType identifies the JWK key family.
	KeyType string `json:"kty"`
	// Curve identifies the Ed25519 JWK curve.
	Curve string `json:"crv"`
	// X contains the base64url-encoded public key bytes.
	X string `json:"x"`
	// KeyID identifies the registered experimental device key.
	KeyID string `json:"kid"`
	// Use identifies the JWK as a signing key.
	Use string `json:"use"`
	// Algorithm identifies the JWK signing algorithm.
	Algorithm string `json:"alg"`
}

// authorizationPINRequest encodes the experimental Plex PIN request.
type authorizationPINRequest struct {
	// JWK is the public device key registered with Plex.
	JWK *deviceJWK `json:"jwk,omitempty"`
	// Strong requests Plex's strong authorization flow.
	Strong bool `json:"strong"`
}

// xmlResourcesResponse decodes the XML resource listing.
type xmlResourcesResponse struct {
	// Devices contains resources returned by the Plex XML endpoint.
	Devices []xmlResource `xml:"Device"`
}

// xmlResource models a Plex resource from the Plex XML API.
type xmlResource struct {
	// Name is the display name.
	Name string `xml:"name,attr"`
	// ClientIdentifier is Plex's stable identifier for the resource.
	ClientIdentifier string `xml:"clientIdentifier,attr"`
	// Provides lists capabilities advertised by the resource.
	Provides string `xml:"provides,attr"`
	// Owned reports whether the Plex account owns the server.
	Owned int `xml:"owned,attr"`
	// AccessToken is the resource-specific Plex token.
	AccessToken string `xml:"accessToken,attr"`
	// Platform is the server platform reported by Plex.
	Platform string `xml:"platform,attr"`
	// Connections lists advertised endpoints for the resource.
	Connections []xmlConnection `xml:"Connection"`
}

// xmlConnection models a Plex connection from the Plex XML API.
type xmlConnection struct {
	// URI is the advertised connection URL.
	URI string `xml:"uri,attr"`
	// Local reports whether the selected connection is local.
	Local int `xml:"local,attr"`
	// Relay reports whether the selected connection uses Plex Relay.
	Relay int `xml:"relay,attr"`
}

// tokenRefreshRequest carries a signed device JWT for token refresh.
type tokenRefreshRequest struct {
	// JWT is the signed device token submitted during refresh.
	JWT string `json:"jwt"`
}

// tokenClaims contains the JWT claims needed to determine token expiry.
type tokenClaims struct {
	// ExpiresAt contains the exp claim as a Unix timestamp.
	ExpiresAt int64 `json:"exp"`
}

// tokenCandidate associates a candidate Plex token with a diagnostic label.
type tokenCandidate struct {
	// kind is the diagnostic label for the token candidate.
	kind string
	// value is the candidate token value.
	value string
}

// pendingAuth contains transient state for an in-progress Plex authorization.
type pendingAuth struct {
	// method is the authorization method used by the pending session.
	method AuthMethod
	// clientID is the Plex client identifier sent with requests.
	clientID string
	// keyID identifies the pending JWT device key.
	keyID string
	// privateKey is the pending JWT device private key.
	privateKey ed25519.PrivateKey
	// pinID is the Plex PIN identifier being polled.
	pinID int64
	// expiresAt is the pending authorization expiry time.
	expiresAt time.Time
	// userToken is populated once Plex authorization completes.
	userToken string
	// tokenExp is the parsed account-token expiry time.
	tokenExp time.Time
	// resources contains Plex servers discovered after authorization.
	resources map[string]resource
}

// authorizationRequest contains method-specific values for a Plex PIN request.
type authorizationRequest struct {
	// keyID identifies the generated JWT device key when used.
	keyID string
	// privateKey is the generated JWT device private key when used.
	privateKey ed25519.PrivateKey
	// query contains method-specific Plex PIN request parameters.
	query url.Values
	// body contains the optional Plex PIN request payload.
	body any
}

// AuthManager coordinates Plex authorization, server selection, and token refresh.
type AuthManager struct {
	// store persists Plex authorization state.
	store AuthStore
	// logger records Plex diagnostics.
	logger *slog.Logger
	// cloudBase is the Plex cloud API base URL.
	cloudBase *url.URL
	// serverURLOverride replaces discovered server URLs at runtime.
	serverURLOverride string
	// experimental reports whether JWT authorization is enabled.
	experimental bool
	// httpClient executes Plex HTTP requests.
	httpClient *http.Client

	// mu protects mutable in-memory state.
	mu sync.Mutex
	// refreshMu serializes Plex token refresh operations.
	refreshMu sync.Mutex
	// state is the active persisted Plex authorization state.
	state AuthState
	// pending contains temporary authorization sessions keyed by setup token.
	pending map[string]*pendingAuth
	// now supplies the current time and is replaceable in tests.
	now func() time.Time
}

// NewAuthManager creates an authentication manager and restores saved state.
func NewAuthManager(
	ctx context.Context,
	store AuthStore,
	logger *slog.Logger,
	cloudURL string,
	serverURLOverride string,
	experimental bool,
) (*AuthManager, error) {
	if logger == nil {
		logger = slog.Default()
	}

	base, err := parseHTTPURL(strings.TrimRight(cloudURL, "/"))
	if err != nil {
		return nil, ErrInvalidCloudURL
	}

	serverURLOverride = strings.TrimRight(serverURLOverride, "/")
	if serverURLOverride != "" {
		if _, err := parseHTTPURL(serverURLOverride); err != nil {
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
	if err != nil && !errors.Is(err, ErrAuthNotFound) {
		return nil, fmt.Errorf("load Plex authentication: %w", err)
	}
	if err == nil {
		manager.state = state
	}

	manager.requestLogger(ctx).Info("Plex authentication manager ready",
		"event", "plex_auth_ready",
		"configured", authStateConfigured(state),
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
func (m *AuthManager) Configured() (configured bool, serverName string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return authStateConfigured(m.state), m.state.ServerName
}

// Start begins a Plex device authorization flow.
func (m *AuthManager) Start(ctx context.Context, method AuthMethod) (AuthStart, error) {
	if configured, _ := m.Configured(); configured {
		return AuthStart{}, ErrAlreadyConfigured
	}

	if !ValidAuthMethod(method) {
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

	request, err := buildAuthorizationRequest(method)
	if err != nil {
		return AuthStart{}, err
	}

	var pin authorizationPINResponse
	if err := m.cloudJSON(ctx, http.MethodPost, "/api/v2/pins", clientID, "", request.query, request.body, &pin); err != nil {
		return AuthStart{}, err
	}
	if pin.ID == 0 || pin.Code == "" {
		return AuthStart{}, ErrInvalidAuthorizationPIN
	}

	setupToken, err := randomToken(32)
	if err != nil {
		return AuthStart{}, fmt.Errorf("generate Plex setup token: %w", err)
	}

	pending := &pendingAuth{
		method:     method,
		clientID:   clientID,
		keyID:      request.keyID,
		privateKey: request.privateKey,
		pinID:      pin.ID,
		expiresAt:  m.now().Add(authorizationTTL(pin.ExpiresIn)),
	}
	m.storePendingAuthorization(setupToken, pending)

	logger.Info("Plex authorization started",
		"event", "plex_auth_started",
		"pin_id", pin.ID,
		"auth_method", method,
		"expires_at", pending.expiresAt,
	)

	return AuthStart{
		AuthURL:    plexAuthorizationURL(clientID, pin.Code),
		SetupToken: setupToken,
		ExpiresAt:  pending.expiresAt,
	}, nil
}

// authorizationTTL returns a bounded lifetime for a Plex setup session.
func authorizationTTL(expiresIn int) time.Duration {
	ttl := time.Duration(expiresIn) * time.Second
	if ttl <= 0 || ttl > 30*time.Minute {
		return 10 * time.Minute
	}
	return ttl
}

// storePendingAuthorization records a setup session and removes expired sessions.
func (m *AuthManager) storePendingAuthorization(setupToken string, pending *pendingAuth) {
	m.mu.Lock()
	defer m.mu.Unlock()

	now := m.now()
	for token, auth := range m.pending {
		if now.After(auth.expiresAt) {
			delete(m.pending, token)
		}
	}
	m.pending[setupToken] = pending
}

// plexAuthorizationURL builds the browser URL used to approve Plex access.
func plexAuthorizationURL(clientID, pinCode string) string {
	params := url.Values{
		"clientID":                    {clientID},
		"code":                        {pinCode},
		"context[device][product]":    {"ScreenDeck"},
		"context[device][version]":    {"1.0"},
		"context[device][platform]":   {"Web"},
		"context[device][deviceName]": {"ScreenDeck"},
		"context[device][model]":      {"Plex OAuth"},
		"context[device][layout]":     {"desktop"},
	}
	return authorizationURL + "#?" + params.Encode()
}

// Status polls and reports a Plex device authorization flow.
func (m *AuthManager) Status(ctx context.Context, setupToken string) (AuthStatus, error) {
	logger := m.requestLogger(ctx)
	pending := m.pendingAuthSnapshot(setupToken)

	if authorizationExpired(pending, m.now()) {
		logger.Warn("Plex authorization session expired",
			"event", "plex_auth_expired",
		)
		return AuthStatus{}, ErrAuthorizationExpired
	}
	if pending.userToken != "" {
		return AuthStatus{Status: "authorized", Servers: serverInfos(pending.resources)}, nil
	}

	logger.Debug("polling Plex authorization",
		"event", "plex_auth_polling",
		"pin_id", pending.pinID,
	)

	query, err := authorizationStatusQuery(pending, m.now())
	if err != nil {
		return AuthStatus{}, err
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

	tokenExpiresAt := authorizationTokenExpiry(pending.method, pin.AuthToken, m.now())
	if err := m.completePendingAuthorization(setupToken, pin.AuthToken, tokenExpiresAt, resources); err != nil {
		return AuthStatus{}, err
	}

	logger.Info("Plex authorization completed",
		"event", "plex_auth_authorized",
		"server_count", len(resources),
		"auth_method", pending.method,
		"token_expires_at", tokenExpiresAt,
	)

	return AuthStatus{
		Status:  "authorized",
		Servers: serverInfos(resources),
	}, nil
}

// completePendingAuthorization stores account authorization data in a pending session.
func (m *AuthManager) completePendingAuthorization(
	setupToken string,
	userToken string,
	tokenExpiresAt time.Time,
	resources map[string]resource,
) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	current := m.pending[setupToken]
	if authorizationExpired(current, m.now()) {
		return ErrAuthorizationExpired
	}

	current.userToken = userToken
	current.tokenExp = tokenExpiresAt
	current.resources = resources
	return nil
}

// Libraries returns libraries from the configured Plex server.
func (m *AuthManager) Libraries(ctx context.Context) ([]media.Library, error) {
	client, err := m.client(ctx)
	if err != nil {
		return nil, err
	}
	return client.Libraries(ctx)
}

// Items returns media from the configured Plex server.
func (m *AuthManager) Items(ctx context.Context, library media.Library) ([]media.Item, error) {
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

// authStateSnapshot returns an immutable copy of the active Plex authentication state.
func (m *AuthManager) authStateSnapshot() AuthState {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.state
}

// setAuthState replaces the active Plex authentication state.
func (m *AuthManager) setAuthState(state AuthState) {
	m.mu.Lock()
	m.state = state
	m.mu.Unlock()
}

// client returns an authenticated client for the selected Plex server.
func (m *AuthManager) client(ctx context.Context) (*Client, error) {
	state := m.authStateSnapshot()
	if !authStateConfigured(state) {
		return nil, ErrNotConfigured
	}

	if authenticationRefreshNeeded(state, m.now()) {
		if err := m.refresh(ctx, false); err != nil {
			return nil, fmt.Errorf("%w: %w", ErrAuthenticationRefresh, err)
		}
		state = m.authStateSnapshot()
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

// pendingAuthSnapshot returns an immutable snapshot of a pending setup session.
func (m *AuthManager) pendingAuthSnapshot(setupToken string) *pendingAuth {
	m.mu.Lock()
	defer m.mu.Unlock()
	pending := m.pending[setupToken]

	if pending == nil {
		return nil
	}
	snapshot := *pending

	return &snapshot
}

// authStateConfigured reports whether saved Plex state can create an authenticated client.
func authStateConfigured(state AuthState) bool {
	return state.ServerURL != "" && state.ServerToken != ""
}

// ValidAuthMethod reports whether method is a supported Plex authorization flow.
func ValidAuthMethod(method AuthMethod) bool {
	return method == AuthMethodStandard || method == AuthMethodJWT
}

// authorizationExpired reports whether a setup session is missing or past its expiry.
func authorizationExpired(pending *pendingAuth, now time.Time) bool {
	return pending == nil || now.After(pending.expiresAt)
}

// authorizationIncomplete reports whether a setup session cannot select a server yet.
func authorizationIncomplete(pending *pendingAuth, now time.Time) bool {
	return authorizationExpired(pending, now) || pending.userToken == ""
}

// parseHTTPURL parses and validates an absolute HTTP or HTTPS URL.
func parseHTTPURL(rawURL string) (*url.URL, error) {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return nil, err
	}

	if parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		return nil, errors.New("URL must be absolute HTTP or HTTPS")
	}

	return parsed, nil
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

// requestLogger returns a logger enriched with request context.
func (m *AuthManager) requestLogger(ctx context.Context) *slog.Logger {
	return requestid.WithLogger(m.logger, ctx)
}

// randomToken returns a cryptographically secure URL-safe token.
func randomToken(size int) (string, error) {
	raw := make([]byte, size)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(raw), nil
}

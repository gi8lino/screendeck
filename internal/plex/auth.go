package plex

import (
	"bytes"
	"cmp"
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
	"slices"
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

// buildAuthorizationRequest prepares the method-specific Plex PIN request.
func buildAuthorizationRequest(method AuthMethod) (authorizationRequest, error) {
	request := authorizationRequest{query: url.Values{}}
	if method != AuthMethodJWT {
		request.query.Set("strong", "true")
		return request, nil
	}

	// The experimental flow registers an ephemeral device key with Plex and
	// keeps the private half server-side for subsequent signed JWT requests.
	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return authorizationRequest{}, fmt.Errorf("generate Plex device key: %w", err)
	}

	digest := sha256.Sum256(publicKey)
	keyID := base64.RawURLEncoding.EncodeToString(digest[:12])
	request.keyID = keyID
	request.privateKey = privateKey
	request.body = authorizationPINRequest{
		JWK: &deviceJWK{
			KeyType:   "OKP",
			Curve:     "Ed25519",
			X:         base64.RawURLEncoding.EncodeToString(publicKey),
			KeyID:     keyID,
			Use:       "sig",
			Algorithm: "EdDSA",
		},
		Strong: true,
	}

	return request, nil
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

// authorizationStatusQuery builds the method-specific PIN polling parameters.
func authorizationStatusQuery(pending *pendingAuth, now time.Time) (url.Values, error) {
	query := url.Values{}
	if pending.method != AuthMethodJWT {
		return query, nil
	}

	deviceJWT, err := signDeviceJWT(
		pending.privateKey,
		pending.keyID,
		map[string]any{
			"aud": "plex.tv",
			"iss": pending.clientID,
			"iat": now.Unix(),
			"exp": now.Add(5 * time.Minute).Unix(),
		},
	)
	if err != nil {
		return nil, fmt.Errorf("sign Plex device JWT: %w", err)
	}

	query.Set("deviceJWT", deviceJWT)
	return query, nil
}

// authorizationTokenExpiry returns the stored account-token expiry for an authorization method.
func authorizationTokenExpiry(method AuthMethod, token string, now time.Time) time.Time {
	if method != AuthMethodJWT {
		return time.Time{}
	}
	return tokenExpiry(token, now.Add(7*24*time.Hour))
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

// SelectServer persists the selected Plex server connection.
func (m *AuthManager) SelectServer(ctx context.Context, setupToken, serverID string) error {
	logger := m.requestLogger(ctx)
	pending := m.pendingAuthSnapshot(setupToken)
	if authorizationIncomplete(pending, m.now()) {
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
	logServerSelection(logger, server)

	selected, ok := preferredConnection(server.Connections)
	if !ok {
		return ErrNoUsableConnection
	}

	effectiveURL := m.serverURL(selected.URI)
	logSelectedConnection(logger, server, selected, effectiveURL, m.serverURLOverride != "")

	// Plex deployments differ in which account or resource token they accept,
	// so verification tries candidates in the safest method-specific order.
	candidates := serverVerificationCandidates(pending.method, pending.userToken, server.AccessToken)
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

	state := authStateForServer(pending, server, selected, serverToken)
	if err := m.persistSelectedServer(ctx, setupToken, state); err != nil {
		return err
	}

	logger.Info("Plex server selected",
		"event", "plex_server_selected",
		"server_id", state.ServerID,
		"server_name", state.ServerName,
		"effective_url", safeURL(effectiveURL),
	)
	return nil
}

// logServerSelection records the selected Plex resource and its advertised connections.
func logServerSelection(logger *slog.Logger, server resource) {
	logger.Info("selecting Plex server",
		"event", "plex_server_selecting",
		"server_id", server.ClientIdentifier,
		"server_name", server.Name,
		"owned", server.Owned,
		"platform", server.Platform,
		"connection_count", len(server.Connections),
	)

	for _, candidate := range server.Connections {
		logger.Debug("discovered Plex connection",
			"event", "plex_connection_discovered",
			"server_id", server.ClientIdentifier,
			"url", safeURL(candidate.URI),
			"local", candidate.Local,
			"relay", candidate.Relay,
		)
	}
}

// logSelectedConnection records the Plex endpoint selected for runtime use.
func logSelectedConnection(
	logger *slog.Logger,
	server resource,
	selected connection,
	effectiveURL string,
	overridden bool,
) {
	logger.Info("selected Plex connection",
		"event", "plex_connection_selected",
		"server_id", server.ClientIdentifier,
		"discovered_url", safeURL(selected.URI),
		"effective_url", safeURL(effectiveURL),
		"local", selected.Local,
		"relay", selected.Relay,
		"url_override", overridden,
	)
}

// authStateForServer builds persisted authentication state for a selected Plex server.
func authStateForServer(
	pending *pendingAuth,
	server resource,
	selected connection,
	serverToken string,
) AuthState {
	return AuthState{
		Method:         pending.method,
		ClientID:       pending.clientID,
		KeyID:          pending.keyID,
		PrivateKey:     pending.privateKey,
		UserToken:      pending.userToken,
		TokenExpiresAt: pending.tokenExp,
		ServerID:       server.ClientIdentifier,
		ServerName:     server.Name,
		ServerURL:      selected.URI,
		ServerToken:    serverToken,
	}
}

// persistSelectedServer saves authentication state and completes the setup session.
func (m *AuthManager) persistSelectedServer(
	ctx context.Context,
	setupToken string,
	state AuthState,
) error {
	if err := m.store.SavePlexAuth(ctx, state); err != nil {
		return fmt.Errorf("save Plex authentication: %w", err)
	}

	m.mu.Lock()
	m.state = state
	delete(m.pending, setupToken)
	m.mu.Unlock()
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

// Refresh renews Plex authentication when necessary.
func (m *AuthManager) Refresh(ctx context.Context) error {
	return m.refresh(ctx, true)
}

// refresh renews and persists Plex authentication state.
func (m *AuthManager) refresh(ctx context.Context, force bool) error {
	m.refreshMu.Lock()
	defer m.refreshMu.Unlock()

	logger := m.requestLogger(ctx)
	state := m.authStateSnapshot()
	if state.UserToken == "" {
		return ErrAuthNotFound
	}
	if state.Method != AuthMethodJWT {
		logger.Debug("Plex token refresh is not required for standard authentication",
			"event", "plex_token_refresh_skipped",
			"auth_method", state.Method,
		)
		return nil
	}
	if !force && !tokenNeedsRefresh(state.TokenExpiresAt, m.now()) {
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

	accountToken, err := m.refreshAccountToken(ctx, state)
	if err != nil {
		return err
	}

	state, effectiveURL, err := m.refreshServerAccess(ctx, logger, state, accountToken)
	if err != nil {
		return err
	}
	if err := m.store.SavePlexAuth(ctx, state); err != nil {
		return fmt.Errorf("save refreshed Plex authentication: %w", err)
	}
	m.setAuthState(state)

	logger.Info("Plex authentication refreshed",
		"event", "plex_token_refreshed",
		"server_id", state.ServerID,
		"effective_url", safeURL(effectiveURL),
		"token_expires_at", state.TokenExpiresAt,
	)
	return nil
}

// refreshAccountToken requests a new Plex account JWT for the active device key.
func (m *AuthManager) refreshAccountToken(ctx context.Context, state AuthState) (string, error) {
	var nonceResponse authNonceResponse
	if err := m.cloudJSON(ctx, http.MethodGet, "/api/v2/auth/nonce", state.ClientID, "", nil, nil, &nonceResponse); err != nil {
		return "", err
	}

	now := m.now()
	deviceJWT, err := signDeviceJWT(state.PrivateKey, state.KeyID, map[string]any{
		"nonce": nonceResponse.Nonce,
		"scope": "friendly_name",
		"aud":   "plex.tv",
		"iss":   state.ClientID,
		"iat":   now.Unix(),
		"exp":   now.Add(5 * time.Minute).Unix(),
	})
	if err != nil {
		return "", fmt.Errorf("sign Plex refresh JWT: %w", err)
	}

	var tokenResponse authTokenResponse
	if err := m.cloudJSON(
		ctx,
		http.MethodPost,
		"/api/v2/auth/token",
		state.ClientID,
		"",
		nil,
		tokenRefreshRequest{JWT: deviceJWT},
		&tokenResponse,
	); err != nil {
		return "", err
	}
	if tokenResponse.AuthToken == "" {
		return "", ErrEmptyRefreshedToken
	}
	return tokenResponse.AuthToken, nil
}

// refreshServerAccess verifies the refreshed account token and returns updated server state.
func (m *AuthManager) refreshServerAccess(
	ctx context.Context,
	logger *slog.Logger,
	state AuthState,
	accountToken string,
) (refreshedState AuthState, effectiveURL string, err error) {
	resources, err := m.resources(ctx, AuthMethodJWT, state.ClientID, accountToken)
	if err != nil {
		return AuthState{}, "", err
	}

	// Refresh can also return updated server connection metadata and a new
	// resource token, both of which must be verified before they are persisted.
	resourceToken := ""
	if server, ok := resources[state.ServerID]; ok {
		if selected, found := preferredConnection(server.Connections); found {
			state.ServerURL = selected.URI
		}
		resourceToken = server.AccessToken
	}

	effectiveURL = m.serverURL(state.ServerURL)
	serverToken, err := m.verifyServer(
		ctx,
		effectiveURL,
		state.ClientID,
		tokenCandidate{kind: "account_jwt", value: accountToken},
		tokenCandidate{kind: "resource_token", value: resourceToken},
	)
	if err != nil {
		logger.Error("refreshed Plex token verification failed",
			"event", "plex_token_refresh_verification_failed",
			"server_id", state.ServerID,
			"url", safeURL(effectiveURL),
			"error", err,
		)
		return AuthState{}, "", fmt.Errorf("%w: %w", ErrAuthenticationRefresh, err)
	}

	state.UserToken = accountToken
	state.ServerToken = serverToken
	state.TokenExpiresAt = tokenExpiry(accountToken, m.now().Add(7*24*time.Hour))

	refreshedState = state
	return refreshedState, effectiveURL, nil
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

// serverVerificationCandidates returns Plex tokens in method-specific verification order.
func serverVerificationCandidates(
	method AuthMethod,
	accountToken string,
	resourceToken string,
) []tokenCandidate {
	if method == AuthMethodJWT {
		return []tokenCandidate{
			{
				kind:  "account_jwt",
				value: accountToken,
			},
			{
				kind:  "resource_token",
				value: resourceToken,
			},
		}
	}
	return []tokenCandidate{
		{
			kind:  "resource_token",
			value: resourceToken,
		},
		{
			kind:  "account_token",
			value: accountToken,
		},
	}
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

// tokenNeedsRefresh reports whether a JWT should be refreshed before normal use.
func tokenNeedsRefresh(expiresAt, now time.Time) bool {
	return expiresAt.IsZero() || expiresAt.Sub(now) < 12*time.Hour
}

// authenticationRefreshNeeded reports whether the active authentication state needs JWT refresh.
func authenticationRefreshNeeded(state AuthState, now time.Time) bool {
	return state.Method == AuthMethodJWT && tokenNeedsRefresh(state.TokenExpiresAt, now)
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

// usableServerResource reports whether a Plex resource can represent a selectable server.
func usableServerResource(item resource) bool {
	return item.ClientIdentifier != "" && containsProvide(item.Provides, "server") && len(item.Connections) > 0
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
func (m *AuthManager) verifyServer(
	ctx context.Context,
	serverURL string,
	clientID string,
	candidates ...tokenCandidate,
) (string, error) {
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
func (m *AuthManager) resources(
	ctx context.Context,
	method AuthMethod,
	clientID string,
	token string,
) (map[string]resource, error) {
	if method == AuthMethodStandard {
		return m.xmlResources(ctx, clientID, token)
	}

	logger := m.requestLogger(ctx)

	query := url.Values{
		"includeHttps": {"1"},
		"includeRelay": {"1"},
		"includeIPv6":  {"1"},
	}

	var response []resource
	if err := m.cloudJSON(ctx, http.MethodGet, "/api/v2/resources", clientID, token, query, nil, &response); err != nil {
		return nil, err
	}

	resources := make(map[string]resource)
	for _, item := range response {
		if !usableServerResource(item) {
			continue
		}

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

	if len(resources) == 0 {
		return nil, ErrNoServers
	}

	logger.Info("Plex servers discovered",
		"event", "plex_servers_discovered",
		"server_count", len(resources),
	)

	return resources, nil
}

// xmlResources retrieves Plex resources with server-compatible resource tokens.
func (m *AuthManager) xmlResources(
	ctx context.Context,
	clientID string,
	token string,
) (map[string]resource, error) {
	logger := m.requestLogger(ctx)
	query := url.Values{
		"includeHttps": {"1"},
		"includeRelay": {"1"},
		"includeIPv6":  {"1"},
	}

	var response xmlResourcesResponse
	if err := m.cloudXML(ctx, http.MethodGet, "/api/resources", clientID, token, query, &response); err != nil {
		return nil, err
	}

	resources := make(map[string]resource)
	for _, item := range response.Devices {
		converted := resourceFromXML(item)
		if !usableServerResource(converted) {
			continue
		}

		resources[converted.ClientIdentifier] = converted
		logger.Debug("discovered Plex server",
			"event", "plex_server_discovered",
			"server_id", converted.ClientIdentifier,
			"server_name", converted.Name,
			"owned", converted.Owned,
			"platform", converted.Platform,
			"connection_count", len(converted.Connections),
			"has_resource_token", converted.AccessToken != "",
			"auth_method", AuthMethodStandard,
		)
	}
	if len(resources) == 0 {
		return nil, ErrNoServers
	}

	logger.Info("Plex servers discovered",
		"event", "plex_servers_discovered",
		"server_count", len(resources),
		"auth_method", AuthMethodStandard,
	)
	return resources, nil
}

// resourceFromXML converts an XML Plex resource into the common resource model.
func resourceFromXML(item xmlResource) resource {
	connections := make([]connection, 0, len(item.Connections))
	for _, candidate := range item.Connections {
		connections = append(connections, connection{
			URI:   candidate.URI,
			Local: candidate.Local == 1,
			Relay: candidate.Relay == 1,
		})
	}

	return resource{
		Name:             item.Name,
		ClientIdentifier: item.ClientIdentifier,
		Provides:         item.Provides,
		Owned:            item.Owned == 1,
		AccessToken:      item.AccessToken,
		Platform:         item.Platform,
		Connections:      connections,
	}
}

// cloudJSON sends a JSON request to the Plex cloud API.
func (m *AuthManager) cloudJSON(
	ctx context.Context,
	method string,
	path string,
	clientID string,
	token string,
	query url.Values,
	body any,
	target any,
) error {
	var reader io.Reader
	if body != nil {
		encoded, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("encode Plex authentication request: %w", err)
		}
		reader = bytes.NewReader(encoded)
	}

	response, err := m.cloudRequest(ctx, method, path, clientID, token, query, reader, "application/json", "application/json")
	if err != nil {
		return err
	}
	defer response.Body.Close() // nolint:errcheck

	if target == nil {
		return nil
	}
	if err := json.NewDecoder(io.LimitReader(response.Body, 8<<20)).Decode(target); err != nil {
		return fmt.Errorf("%w: %s %s: %w", ErrCloudDecode, method, path, err)
	}
	return nil
}

// cloudXML sends an XML request to the Plex XML cloud API.
func (m *AuthManager) cloudXML(
	ctx context.Context,
	method string,
	path string,
	clientID string,
	token string,
	query url.Values,
	target any,
) error {
	response, err := m.cloudRequest(ctx, method, path, clientID, token, query, nil, "application/xml", "")
	if err != nil {
		return err
	}
	defer response.Body.Close() // nolint:errcheck

	if err := xml.NewDecoder(io.LimitReader(response.Body, 8<<20)).Decode(target); err != nil {
		return fmt.Errorf("%w: %s %s: %w", ErrCloudDecode, method, path, err)
	}
	return nil
}

// cloudRequest sends one authenticated request to the Plex cloud API.
func (m *AuthManager) cloudRequest(
	ctx context.Context,
	method string,
	path string,
	clientID string,
	token string,
	query url.Values,
	body io.Reader,
	accept string,
	contentType string,
) (*http.Response, error) {
	logger := m.requestLogger(ctx)
	started := time.Now()
	logger.Debug("sending Plex authentication request",
		"event", "plex_cloud_request",
		"method", method,
		"path", path,
		"authenticated", token != "",
	)

	u := m.cloudBase.JoinPath(path)
	u.RawQuery = query.Encode()

	req, err := http.NewRequestWithContext(ctx, method, u.String(), body)
	if err != nil {
		return nil, fmt.Errorf("create Plex authentication request: %w", err)
	}
	setPlexCloudHeaders(req, clientID, token, accept, contentType)

	response, err := m.httpClient.Do(req)
	if err != nil {
		logger.Error("Plex authentication request failed",
			"event", "plex_cloud_request_failed",
			"method", method,
			"path", path,
			"duration_ms", time.Since(started).Milliseconds(),
			"error", err,
		)
		return nil, fmt.Errorf("%w: %s %s: %w", ErrCloudUnavailable, method, path, err)
	}

	logger.Debug("Plex authentication response received",
		"event", "plex_cloud_response",
		"method", method,
		"path", path,
		"status", response.StatusCode,
		"duration_ms", time.Since(started).Milliseconds(),
	)
	if response.StatusCode >= 200 && response.StatusCode < 300 {
		return response, nil
	}

	defer response.Body.Close()                                   // nolint:errcheck
	message, _ := io.ReadAll(io.LimitReader(response.Body, 1024)) // nolint:errcheck
	return nil, fmt.Errorf(
		"%w: %s %s: %s: %s",
		ErrCloudResponse,
		method,
		path,
		response.Status,
		strings.TrimSpace(string(message)),
	)
}

// setPlexCloudHeaders adds the common headers required by Plex cloud requests.
func setPlexCloudHeaders(
	req *http.Request,
	clientID string,
	token string,
	accept string,
	contentType string,
) {
	req.Header.Set("Accept", accept)
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	req.Header.Set("X-Plex-Product", "ScreenDeck")
	req.Header.Set("X-Plex-Version", "1.0")
	req.Header.Set("X-Plex-Client-Identifier", clientID)
	if token != "" {
		// Keep credentials in headers rather than URLs so they cannot leak through
		// query strings, upstream error messages, or request logs.
		req.Header.Set("X-Plex-Token", token)
	}
}

// requestLogger returns a logger enriched with request context.
func (m *AuthManager) requestLogger(ctx context.Context) *slog.Logger {
	return requestid.WithLogger(m.logger, ctx)
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
		servers = append(servers, ServerInfo{
			ID:       server.ClientIdentifier,
			Name:     server.Name,
			Owned:    server.Owned,
			Local:    selected.Local,
			Relay:    selected.Relay,
			Platform: server.Platform,
		})
	}
	slices.SortFunc(servers, func(a, b ServerInfo) int {
		if a.Local != b.Local {
			if a.Local {
				return -1
			}
			return 1
		}
		if a.Owned != b.Owned {
			if a.Owned {
				return -1
			}
			return 1
		}
		return cmp.Compare(a.Name, b.Name)
	})
	return servers
}

// preferredConnection chooses the best reachable Plex connection.
func preferredConnection(connections []connection) (selected connection, ok bool) {
	if len(connections) == 0 {
		return connection{}, false
	}
	copyOf := slices.Clone(connections)
	slices.SortStableFunc(copyOf, func(a, b connection) int {
		score := func(c connection) int {
			if c.Local && !c.Relay {
				return 0
			}
			if !c.Relay {
				return 1
			}
			return 2
		}
		return cmp.Compare(score(a), score(b))
	})
	for _, candidate := range copyOf {
		if _, err := parseHTTPURL(candidate.URI); err != nil {
			continue
		}
		return candidate, true
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

package plex

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// memoryAuthStore is an in-memory Plex authentication store used by tests.
type memoryAuthStore struct {
	// state contains the most recently persisted authentication state.
	state *AuthState
}

// pinRequestBody decodes the PIN request observed by the Plex test server.
type pinRequestBody struct {
	// JWK is the public device key registered with Plex.
	JWK pinRequestJWK `json:"jwk"`
	// Strong requests Plex's strong authorization flow.
	Strong bool `json:"strong"`
}

// pinRequestJWK decodes the public key embedded in a PIN request.
type pinRequestJWK struct {
	// X contains the base64url-encoded public key bytes.
	X string `json:"x"`
}

// refreshRequestBody decodes a token refresh request in tests.
type refreshRequestBody struct {
	// JWT is the signed device token submitted during refresh.
	JWT string `json:"jwt"`
}

// LoadPlexAuth returns authentication state from the test store.
func (s *memoryAuthStore) LoadPlexAuth(context.Context) (AuthState, error) {
	if s.state == nil {
		return AuthState{}, ErrAuthNotFound
	}
	return *s.state, nil
}

// SavePlexAuth saves authentication state in the test store.
func (s *memoryAuthStore) SavePlexAuth(_ context.Context, state AuthState) error {
	copyOf := state
	s.state = &copyOf
	return nil
}

// TestStandardAuthorizationServerSelection verifies the standard Plex PIN authentication flow.
func TestStandardAuthorizationServerSelection(t *testing.T) {
	pms := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Plex-Token") != "standard-server-token" {
			http.Error(w, "bad server token", http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"MediaContainer":{"Directory":[{"key":"1","title":"Films","type":"movie"}]}}`) // nolint:errcheck
	}))
	defer pms.Close()

	var clientID string
	cloud := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/api/v2/pins":
			clientID = r.Header.Get("X-Plex-Client-Identifier")
			if r.URL.Query().Get("strong") != "true" {
				http.Error(w, "standard PIN must be strong", http.StatusBadRequest)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprint(w, `{"id":123,"code":"pin-code","expiresIn":600}`) // nolint:errcheck
		case r.Method == http.MethodGet && r.URL.Path == "/api/v2/pins/123":
			if r.URL.Query().Get("deviceJWT") != "" {
				http.Error(w, "unexpected device JWT", http.StatusBadRequest)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprint(w, `{"authToken":"standard-account-token"}`) // nolint:errcheck
		case r.Method == http.MethodGet && r.URL.Path == "/api/resources":
			if r.Header.Get("X-Plex-Token") != "standard-account-token" || r.Header.Get("Accept") != "application/xml" {
				http.Error(w, "bad XML resource request", http.StatusUnauthorized)
				return
			}
			w.Header().Set("Content-Type", "application/xml")
			fmt.Fprintf(w, `<MediaContainer size="1"><Device name="Home Plex" clientIdentifier="server-1" provides="server" owned="1" platform="Linux" accessToken="standard-server-token"><Connection uri=%q local="1" relay="0"/></Device></MediaContainer>`, pms.URL) // nolint:errcheck
		default:
			http.NotFound(w, r)
		}
	}))
	defer cloud.Close()

	store := &memoryAuthStore{}
	manager, err := NewAuthManager(t.Context(), store, nil, cloud.URL, pms.URL, false)
	require.NoError(t, err)
	started, err := manager.Start(t.Context(), AuthMethodStandard)
	require.NoError(t, err)
	status, err := manager.Status(t.Context(), started.SetupToken)
	require.NoError(t, err)
	assert.Equal(t, "authorized", status.Status)
	require.Len(t, status.Servers, 1)
	require.NoError(t, manager.SelectServer(t.Context(), started.SetupToken, "server-1"))
	require.NotNil(t, store.state)
	assert.NotEmpty(t, clientID)
	assert.Equal(t, AuthMethodStandard, store.state.Method)
	assert.Empty(t, store.state.PrivateKey)
	assert.True(t, store.state.TokenExpiresAt.IsZero())
	libraries, err := manager.Libraries(t.Context())
	require.NoError(t, err)
	assert.Len(t, libraries, 1)
	require.NoError(t, manager.Refresh(t.Context()))
}

// TestJWTAuthorizationRequiresExperimentalMode verifies JWT cannot be started accidentally.
func TestJWTAuthorizationRequiresExperimentalMode(t *testing.T) {
	manager, err := NewAuthManager(t.Context(), &memoryAuthStore{}, nil, "https://test", "", false)
	require.NoError(t, err)
	_, err = manager.Start(t.Context(), AuthMethodJWT)
	require.ErrorIs(t, err, ErrExperimentalAuthDisabled)
}

// TestJWTAuthorizationServerSelectionAndRefresh verifies the complete Plex authentication lifecycle.
func TestJWTAuthorizationServerSelectionAndRefresh(t *testing.T) {
	pms := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if token := r.Header.Get("X-Plex-Token"); !strings.HasPrefix(token, "e30.") {
			http.Error(w, "bad server token", http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"MediaContainer":{"Directory":[{"key":"1","title":"Films","type":"movie"},{"key":"2","title":"Series","type":"show"}]}}`) // nolint:errcheck
	}))
	defer pms.Close()
	discoveredURL := "http://127.0.0.1:1"

	userJWT := testJWT(t, time.Now().Add(7*24*time.Hour), "user")
	refreshedJWT := testJWT(t, time.Now().Add(7*24*time.Hour), "refreshed")
	var publicKey ed25519.PublicKey
	var clientID string
	var refreshed atomic.Bool
	cloud := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/api/v2/pins":
			clientID = r.Header.Get("X-Plex-Client-Identifier")
			var body pinRequestBody
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil || !body.Strong {
				http.Error(w, "bad pin body", http.StatusBadRequest)
				return
			}
			decoded, err := base64.RawURLEncoding.DecodeString(body.JWK.X)
			if err != nil {
				http.Error(w, "bad device key", http.StatusBadRequest)
				return
			}
			publicKey = ed25519.PublicKey(decoded)
			fmt.Fprint(w, `{"id":123,"code":"pin-code","expiresIn":600}`) // nolint:errcheck
		case r.Method == http.MethodGet && r.URL.Path == "/api/v2/pins/123":
			if err := validateDeviceJWT(r.URL.Query().Get("deviceJWT"), publicKey, clientID, ""); err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			fmt.Fprintf(w, `{"authToken":%q}`, userJWT) // nolint:errcheck
		case r.Method == http.MethodGet && r.URL.Path == "/api/v2/resources":
			pmsToken := "server-token"
			if refreshed.Load() {
				pmsToken = "server-token-2"
			}
			fmt.Fprintf(w, `[{"name":"Home Plex","clientIdentifier":"server-1","provides":"server","owned":true,"platform":"Linux","accessToken":%q,"connections":[{"uri":%q,"local":true,"relay":false}]}]`, pmsToken, discoveredURL) // nolint:errcheck
		case r.Method == http.MethodGet && r.URL.Path == "/api/v2/auth/nonce":
			fmt.Fprint(w, `{"nonce":"refresh-nonce"}`) // nolint:errcheck
		case r.Method == http.MethodPost && r.URL.Path == "/api/v2/auth/token":
			var body refreshRequestBody
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				http.Error(w, "bad refresh body", http.StatusBadRequest)
				return
			}
			if err := validateDeviceJWT(body.JWT, publicKey, clientID, "refresh-nonce"); err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			refreshed.Store(true)
			fmt.Fprintf(w, `{"auth_token":%q}`, refreshedJWT) // nolint:errcheck
		default:
			http.NotFound(w, r)
		}
	}))
	defer cloud.Close()

	store := &memoryAuthStore{}
	var logs bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&logs, &slog.HandlerOptions{Level: slog.LevelDebug}))
	manager, err := NewAuthManager(t.Context(), store, logger, cloud.URL, pms.URL, true)
	require.NoError(t, err)
	started, err := manager.Start(t.Context(), AuthMethodJWT)
	require.NoError(t, err)
	assert.Contains(t, started.AuthURL, "app.tv/auth")
	assert.NotEmpty(t, started.SetupToken)
	status, err := manager.Status(t.Context(), started.SetupToken)
	require.NoError(t, err)
	assert.Equal(t, "authorized", status.Status)
	require.Len(t, status.Servers, 1)
	assert.True(t, status.Servers[0].Local)
	require.NoError(t, manager.SelectServer(t.Context(), started.SetupToken, "server-1"))
	configured, name := manager.Configured()
	assert.True(t, configured)
	assert.Equal(t, "Home Plex", name)
	libraries, err := manager.Libraries(t.Context())
	require.NoError(t, err)
	require.Len(t, libraries, 2)
	assert.Equal(t, "show", libraries[1].Type)
	require.NotNil(t, store.state)
	beforeRefresh := store.state.UserToken
	require.NoError(t, manager.Refresh(t.Context()))
	require.NotNil(t, store.state)
	assert.Equal(t, store.state.UserToken, store.state.ServerToken)
	assert.NotEqual(t, beforeRefresh, store.state.UserToken)
	assert.Equal(t, discoveredURL, store.state.ServerURL)
	for _, event := range []string{"plex_auth_started", "plex_servers_discovered", "plex_connection_selected", "plex_server_verified", "plex_token_refreshed"} {
		assert.Contains(t, logs.String(), `"event":"`+event+`"`)
	}
	for _, secret := range []string{"server-token", "server-token-2"} {
		assert.NotContains(t, logs.String(), secret)
	}
}

// validateDeviceJWT verifies the signature and claims of a device JWT.
func validateDeviceJWT(token string, publicKey ed25519.PublicKey, issuer, nonce string) error {
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return fmt.Errorf("invalid device JWT")
	}
	signature, err := base64.RawURLEncoding.DecodeString(parts[2])
	if err != nil {
		return fmt.Errorf("decode device JWT signature: %w", err)
	}
	if !ed25519.Verify(publicKey, []byte(parts[0]+"."+parts[1]), signature) {
		return fmt.Errorf("verify device JWT signature")
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return fmt.Errorf("decode device JWT payload: %w", err)
	}
	var claims map[string]any
	if err := json.Unmarshal(payload, &claims); err != nil {
		return fmt.Errorf("decode device JWT claims: %w", err)
	}
	if claims["aud"] != "tv" || claims["iss"] != issuer {
		return fmt.Errorf("unexpected device JWT claims")
	}
	if nonce != "" && claims["nonce"] != nonce {
		return fmt.Errorf("missing device JWT nonce")
	}
	return nil
}

// testJWT creates an unsigned JWT payload for expiration tests.
func testJWT(t *testing.T, expires time.Time, marker string) string {
	t.Helper()
	payload, err := json.Marshal(map[string]any{"exp": expires.Unix(), "marker": marker})
	require.NoError(t, err)
	return "e30." + base64.RawURLEncoding.EncodeToString(payload) + ".signature"
}

// TestVerifyServer verifies token fallback and stable error classification.
func TestVerifyServer(t *testing.T) {
	t.Run("falls back to resource token", func(t *testing.T) {
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
		token, err := manager.verifyServer(t.Context(), server.URL, "client",
			tokenCandidate{kind: "account_jwt", value: "account-jwt"},
			tokenCandidate{kind: "resource_token", value: "resource-token"},
		)
		require.NoError(t, err)
		assert.Equal(t, "resource-token", token)
		assert.Equal(t, int32(2), requests.Load())
	})

	t.Run("returns server contact sentinel", func(t *testing.T) {
		manager := &AuthManager{logger: slog.New(slog.NewTextHandler(io.Discard, nil))}
		_, err := manager.verifyServer(
			t.Context(),
			"http://127.0.0.1:1",
			"client",
			tokenCandidate{kind: "account_jwt", value: "token"},
		)
		require.ErrorIs(t, err, ErrServerContact)
	})
}

// TestSafeURLRedactsSensitiveParts verifies diagnostic URLs cannot expose credentials or queries.
func TestSafeURLRedactsSensitiveParts(t *testing.T) {
	redacted := safeURL("https://user:secret@plex.example:32400/library?X-Plex-Token=secret#fragment")
	assert.Equal(t, "https://user:xxxxx@plex.example:32400/library", redacted)
}

// TestStatusReturnsAuthorizedPendingSessionWithoutPolling verifies completed setup sessions return immediately.
func TestStatusReturnsAuthorizedPendingSessionWithoutPolling(t *testing.T) {
	now := time.Now().UTC()
	manager := &AuthManager{
		logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
		pending: map[string]*pendingAuth{
			"setup": {
				method:    AuthMethodStandard,
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

	status, err := manager.Status(t.Context(), "setup")
	require.NoError(t, err)
	assert.Equal(t, "authorized", status.Status)
	require.Len(t, status.Servers, 1)
	assert.Equal(t, "server", status.Servers[0].ID)
	assert.True(t, status.Servers[0].Local)
}

// TestBuildAuthorizationRequest verifies standard and JWT PIN request construction.
func TestBuildAuthorizationRequest(t *testing.T) {
	t.Run("standard", func(t *testing.T) {
		request, err := buildAuthorizationRequest(AuthMethodStandard)
		require.NoError(t, err)
		assert.Equal(t, "true", request.query.Get("strong"))
		assert.Empty(t, request.keyID)
		assert.Empty(t, request.privateKey)
		assert.Nil(t, request.body)
	})

	t.Run("JWT", func(t *testing.T) {
		request, err := buildAuthorizationRequest(AuthMethodJWT)
		require.NoError(t, err)
		assert.Empty(t, request.query.Get("strong"))
		assert.Len(t, request.privateKey, ed25519.PrivateKeySize)
		assert.NotEmpty(t, request.keyID)

		body, ok := request.body.(authorizationPINRequest)
		require.True(t, ok)
		require.NotNil(t, body.JWK)
		assert.True(t, body.Strong)
		assert.Equal(t, "OKP", body.JWK.KeyType)
		assert.Equal(t, "Ed25519", body.JWK.Curve)
		assert.Equal(t, "sig", body.JWK.Use)
		assert.Equal(t, "EdDSA", body.JWK.Algorithm)
		assert.Equal(t, request.keyID, body.JWK.KeyID)

		publicKey := request.privateKey.Public().(ed25519.PublicKey)
		assert.Equal(t, base64.RawURLEncoding.EncodeToString(publicKey), body.JWK.X)
	})
}

// TestAuthorizationTTL verifies Plex setup lifetimes are bounded to a safe fallback.
func TestAuthorizationTTL(t *testing.T) {
	t.Run("normal", func(t *testing.T) {
		assert.Equal(t, 5*time.Minute, authorizationTTL(300))
	})

	t.Run("maximum", func(t *testing.T) {
		assert.Equal(t, 30*time.Minute, authorizationTTL(1800))
	})

	t.Run("zero uses fallback", func(t *testing.T) {
		assert.Equal(t, 10*time.Minute, authorizationTTL(0))
	})

	t.Run("negative uses fallback", func(t *testing.T) {
		assert.Equal(t, 10*time.Minute, authorizationTTL(-1))
	})

	t.Run("over maximum uses fallback", func(t *testing.T) {
		assert.Equal(t, 10*time.Minute, authorizationTTL(1801))
	})
}

// TestPlexAuthorizationURL verifies the browser authorization URL contains the required Plex context.
func TestPlexAuthorizationURL(t *testing.T) {
	rawURL := plexAuthorizationURL("client-id", "pin-code")
	parsed, err := url.Parse(rawURL)
	require.NoError(t, err)
	assert.Equal(t, "https", parsed.Scheme)
	assert.Equal(t, "app.plex.tv", parsed.Host)
	assert.Equal(t, "/auth", parsed.Path)

	params, err := url.ParseQuery(strings.TrimPrefix(parsed.Fragment, "?"))
	require.NoError(t, err)
	assert.Equal(t, "client-id", params.Get("clientID"))
	assert.Equal(t, "pin-code", params.Get("code"))
	assert.Equal(t, "ScreenDeck", params.Get("context[device][product]"))
	assert.Equal(t, "1.0", params.Get("context[device][version]"))
	assert.Equal(t, "Web", params.Get("context[device][platform]"))
	assert.Equal(t, "ScreenDeck", params.Get("context[device][deviceName]"))
	assert.Equal(t, "Plex OAuth", params.Get("context[device][model]"))
	assert.Equal(t, "desktop", params.Get("context[device][layout]"))
}

// TestAuthorizationStatusQuery verifies only JWT polling includes a signed device token.
func TestAuthorizationStatusQuery(t *testing.T) {
	now := time.Unix(1_700_000_000, 0).UTC()

	t.Run("standard", func(t *testing.T) {
		query, err := authorizationStatusQuery(&pendingAuth{method: AuthMethodStandard}, now)
		require.NoError(t, err)
		assert.Empty(t, query)
	})

	t.Run("JWT", func(t *testing.T) {
		request, err := buildAuthorizationRequest(AuthMethodJWT)
		require.NoError(t, err)
		pending := &pendingAuth{
			method:     AuthMethodJWT,
			clientID:   "client-id",
			keyID:      request.keyID,
			privateKey: request.privateKey,
		}
		query, err := authorizationStatusQuery(pending, now)
		require.NoError(t, err)

		token := query.Get("deviceJWT")
		require.NotEmpty(t, token)
		parts := strings.Split(token, ".")
		require.Len(t, parts, 3)

		payload, err := base64.RawURLEncoding.DecodeString(parts[1])
		require.NoError(t, err)
		var claims map[string]any
		require.NoError(t, json.Unmarshal(payload, &claims))
		assert.Equal(t, "plex.tv", claims["aud"])
		assert.Equal(t, "client-id", claims["iss"])
		assert.Equal(t, float64(now.Unix()), claims["iat"])
		assert.Equal(t, float64(now.Add(5*time.Minute).Unix()), claims["exp"])

		signature, err := base64.RawURLEncoding.DecodeString(parts[2])
		require.NoError(t, err)
		publicKey := request.privateKey.Public().(ed25519.PublicKey)
		assert.True(t, ed25519.Verify(publicKey, []byte(parts[0]+"."+parts[1]), signature))
	})
}

// TestAuthorizationTokenExpiry verifies token expiry handling by authorization method.
func TestAuthorizationTokenExpiry(t *testing.T) {
	now := time.Unix(1_700_000_000, 0).UTC()

	t.Run("standard has no expiry", func(t *testing.T) {
		assert.True(t, authorizationTokenExpiry(AuthMethodStandard, "token", now).IsZero())
	})

	t.Run("JWT uses token claim", func(t *testing.T) {
		expected := now.Add(48 * time.Hour).UTC()
		payload, err := json.Marshal(map[string]any{"exp": expected.Unix()})
		require.NoError(t, err)
		token := "e30." + base64.RawURLEncoding.EncodeToString(payload) + ".signature"
		assert.Equal(t, expected, authorizationTokenExpiry(AuthMethodJWT, token, now))
	})

	t.Run("invalid JWT uses fallback", func(t *testing.T) {
		assert.Equal(t, now.Add(7*24*time.Hour), authorizationTokenExpiry(AuthMethodJWT, "invalid", now))
	})
}

// TestAuthStateForServer verifies selected Plex data is copied into persistent authentication state.
func TestAuthStateForServer(t *testing.T) {
	expires := time.Unix(1_700_000_000, 0).UTC()
	privateKey := ed25519.PrivateKey(make([]byte, ed25519.PrivateKeySize))
	pending := &pendingAuth{
		method:     AuthMethodJWT,
		clientID:   "client-id",
		keyID:      "key-id",
		privateKey: privateKey,
		userToken:  "user-token",
		tokenExp:   expires,
	}
	server := resource{
		Name:             "Home Plex",
		ClientIdentifier: "server-id",
	}
	selected := connection{URI: "https://plex.example.test:32400"}

	state := authStateForServer(pending, server, selected, "server-token")

	assert.Equal(t, AuthMethodJWT, state.Method)
	assert.Equal(t, "client-id", state.ClientID)
	assert.Equal(t, "key-id", state.KeyID)
	assert.Equal(t, privateKey, state.PrivateKey)
	assert.Equal(t, "user-token", state.UserToken)
	assert.Equal(t, expires, state.TokenExpiresAt)
	assert.Equal(t, "server-id", state.ServerID)
	assert.Equal(t, "Home Plex", state.ServerName)
	assert.Equal(t, selected.URI, state.ServerURL)
	assert.Equal(t, "server-token", state.ServerToken)
}

// TestResourceFromXML verifies XML Plex resources are converted without losing connection metadata.
func TestResourceFromXML(t *testing.T) {
	converted := resourceFromXML(xmlResource{
		Name:             "Home Plex",
		ClientIdentifier: "server-id",
		Provides:         "server,player",
		Owned:            1,
		AccessToken:      "resource-token",
		Platform:         "Linux",
		Connections: []xmlConnection{
			{URI: "http://192.0.2.10:32400", Local: 1, Relay: 0},
			{URI: "https://relay.example.test", Local: 0, Relay: 1},
		},
	})

	assert.Equal(t, "Home Plex", converted.Name)
	assert.Equal(t, "server-id", converted.ClientIdentifier)
	assert.Equal(t, "server,player", converted.Provides)
	assert.True(t, converted.Owned)
	assert.Equal(t, "resource-token", converted.AccessToken)
	assert.Equal(t, "Linux", converted.Platform)
	require.Len(t, converted.Connections, 2)
	assert.True(t, converted.Connections[0].Local)
	assert.False(t, converted.Connections[0].Relay)
	assert.False(t, converted.Connections[1].Local)
	assert.True(t, converted.Connections[1].Relay)
}

// TestPreferredConnection verifies Plex connection ordering and invalid URL handling.
func TestPreferredConnection(t *testing.T) {
	t.Run("prefers local direct connection", func(t *testing.T) {
		connections := []connection{
			{URI: "https://relay.example.test", Relay: true},
			{URI: "https://remote.example.test"},
			{URI: "http://192.0.2.10:32400", Local: true},
		}

		selected, ok := preferredConnection(connections)
		require.True(t, ok)
		assert.Equal(t, "http://192.0.2.10:32400", selected.URI)
		assert.True(t, selected.Local)
		assert.False(t, selected.Relay)
	})

	t.Run("skips invalid URL", func(t *testing.T) {
		selected, ok := preferredConnection([]connection{
			{URI: "://invalid", Local: true},
			{URI: "https://remote.example.test"},
		})
		require.True(t, ok)
		assert.Equal(t, "https://remote.example.test", selected.URI)
	})
}

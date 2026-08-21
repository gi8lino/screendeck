package plex_test

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gi8lino/screendeck/internal/plex"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// memoryAuthStore is an in-memory Plex authentication store used by tests.
type memoryAuthStore struct {
	// state contains the most recently persisted authentication state.
	state *plex.AuthState
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
func (s *memoryAuthStore) LoadPlexAuth(context.Context) (plex.AuthState, error) {
	if s.state == nil {
		return plex.AuthState{}, plex.ErrAuthNotFound
	}
	return *s.state, nil
}

// SavePlexAuth saves authentication state in the test store.
func (s *memoryAuthStore) SavePlexAuth(_ context.Context, state plex.AuthState) error {
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
			fmt.Fprintf(
				w,
				`<MediaContainer size="1"><Device name="Home Plex" clientIdentifier="server-1" provides="server" owned="1" platform="Linux" accessToken="standard-server-token"><Connection uri=%q local="1" relay="0"/></Device></MediaContainer>`,
				pms.URL,
			) // nolint:errcheck

		default:
			http.NotFound(w, r)
		}
	}))
	defer cloud.Close()

	store := &memoryAuthStore{}
	manager, err := plex.NewAuthManager(
		context.Background(),
		store,
		nil,
		cloud.URL,
		pms.URL,
		false,
	)
	require.NoError(t, err)

	started, err := manager.Start(context.Background(), plex.AuthMethodStandard)
	require.NoError(t, err)

	status, err := manager.Status(context.Background(), started.SetupToken)
	require.NoError(t, err)
	assert.Equal(t, "authorized", status.Status)
	require.Len(t, status.Servers, 1)

	require.NoError(
		t,
		manager.SelectServer(
			context.Background(),
			started.SetupToken,
			"server-1",
		),
	)

	require.NotNil(t, store.state)
	assert.NotEmpty(t, clientID)
	assert.Equal(t, plex.AuthMethodStandard, store.state.Method)
	assert.Empty(t, store.state.PrivateKey)
	assert.True(t, store.state.TokenExpiresAt.IsZero())

	libraries, err := manager.Libraries(context.Background())
	require.NoError(t, err)
	assert.Len(t, libraries, 1)

	require.NoError(t, manager.Refresh(context.Background()))
}

// TestJWTAuthorizationRequiresExperimentalMode verifies JWT cannot be started accidentally.
func TestJWTAuthorizationRequiresExperimentalMode(t *testing.T) {
	manager, err := plex.NewAuthManager(
		context.Background(),
		&memoryAuthStore{},
		nil,
		"https://plex.test",
		"",
		false,
	)
	require.NoError(t, err)

	_, err = manager.Start(context.Background(), plex.AuthMethodJWT)
	require.ErrorIs(t, err, plex.ErrExperimentalAuthDisabled)
}

// TestJWTAuthorizationServerSelectionAndRefresh verifies the complete Plex authentication lifecycle.
func TestJWTAuthorizationServerSelectionAndRefresh(t *testing.T) {
	pms := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if token := r.Header.Get("X-Plex-Token"); !strings.HasPrefix(token, "e30.") {
			http.Error(w, "bad server token", http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(
			w,
			`{"MediaContainer":{"Directory":[{"key":"1","title":"Films","type":"movie"},{"key":"2","title":"Series","type":"show"}]}}`,
		) // nolint:errcheck
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
			if err := validateDeviceJWT(
				t,
				r.URL.Query().Get("deviceJWT"),
				publicKey,
				clientID,
				"",
			); err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}

			fmt.Fprintf(w, `{"authToken":%q}`, userJWT) // nolint:errcheck

		case r.Method == http.MethodGet && r.URL.Path == "/api/v2/resources":
			pmsToken := "server-token"
			if refreshed.Load() {
				pmsToken = "server-token-2"
			}

			fmt.Fprintf(
				w,
				`[{"name":"Home Plex","clientIdentifier":"server-1","provides":"server","owned":true,"platform":"Linux","accessToken":%q,"connections":[{"uri":%q,"local":true,"relay":false}]}]`,
				pmsToken,
				discoveredURL,
			) // nolint:errcheck

		case r.Method == http.MethodGet && r.URL.Path == "/api/v2/auth/nonce":
			fmt.Fprint(w, `{"nonce":"refresh-nonce"}`) // nolint:errcheck

		case r.Method == http.MethodPost && r.URL.Path == "/api/v2/auth/token":
			var body refreshRequestBody
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				http.Error(w, "bad refresh body", http.StatusBadRequest)
				return
			}

			if err := validateDeviceJWT(
				t,
				body.JWT,
				publicKey,
				clientID,
				"refresh-nonce",
			); err != nil {
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
	logger := slog.New(
		slog.NewJSONHandler(
			&logs,
			&slog.HandlerOptions{Level: slog.LevelDebug},
		),
	)

	manager, err := plex.NewAuthManager(
		context.Background(),
		store,
		logger,
		cloud.URL,
		pms.URL,
		true,
	)
	require.NoError(t, err)

	started, err := manager.Start(context.Background(), plex.AuthMethodJWT)
	require.NoError(t, err)

	assert.Contains(t, started.AuthURL, "app.plex.tv/auth")
	assert.NotEmpty(t, started.SetupToken)

	status, err := manager.Status(context.Background(), started.SetupToken)
	require.NoError(t, err)
	assert.Equal(t, "authorized", status.Status)
	require.Len(t, status.Servers, 1)
	assert.True(t, status.Servers[0].Local)

	require.NoError(
		t,
		manager.SelectServer(
			context.Background(),
			started.SetupToken,
			"server-1",
		),
	)

	configured, name := manager.Configured()
	assert.True(t, configured)
	assert.Equal(t, "Home Plex", name)

	libraries, err := manager.Libraries(context.Background())
	require.NoError(t, err)
	require.Len(t, libraries, 2)
	assert.Equal(t, "show", libraries[1].Type)

	require.NotNil(t, store.state)
	beforeRefresh := store.state.UserToken

	require.NoError(t, manager.Refresh(context.Background()))

	require.NotNil(t, store.state)
	assert.Equal(t, store.state.UserToken, store.state.ServerToken)
	assert.NotEqual(t, beforeRefresh, store.state.UserToken)
	assert.Equal(t, discoveredURL, store.state.ServerURL)

	for _, event := range []string{
		"plex_auth_started",
		"plex_servers_discovered",
		"plex_connection_selected",
		"plex_server_verified",
		"plex_token_refreshed",
	} {
		assert.Contains(t, logs.String(), `"event":"`+event+`"`)
	}

	for _, secret := range []string{
		"server-token",
		"server-token-2",
	} {
		assert.NotContains(t, logs.String(), secret)
	}
}

// validateDeviceJWT verifies the signature and claims of a device JWT.
func validateDeviceJWT(
	t *testing.T,
	token string,
	publicKey ed25519.PublicKey,
	issuer,
	nonce string,
) error {
	t.Helper()
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return fmt.Errorf("invalid device JWT")
	}

	signature, err := base64.RawURLEncoding.DecodeString(parts[2])
	if err != nil {
		return fmt.Errorf("decode device JWT signature: %w", err)
	}

	if !ed25519.Verify(
		publicKey,
		[]byte(parts[0]+"."+parts[1]),
		signature,
	) {
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

	if claims["aud"] != "plex.tv" || claims["iss"] != issuer {
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

	payload, err := json.Marshal(map[string]any{
		"exp":    expires.Unix(),
		"marker": marker,
	})
	require.NoError(t, err)

	return "e30." + base64.RawURLEncoding.EncodeToString(payload) + ".signature"
}

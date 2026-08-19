package plex_test

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gi8lino/screendeck/internal/plex"
)

type memoryAuthStore struct{ state *plex.AuthState }

type pinRequestBody struct {
	JWK    pinRequestJWK `json:"jwk"`
	Strong bool          `json:"strong"`
}

type pinRequestJWK struct {
	X string `json:"x"`
}

type refreshRequestBody struct {
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

// TestLegacyAuthorizationServerSelection verifies the Tautulli-compatible Plex authentication flow.
func TestLegacyAuthorizationServerSelection(t *testing.T) {
	pms := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Plex-Token") != "legacy-server-token" {
			http.Error(w, "bad server token", http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"MediaContainer":{"Directory":[{"key":"1","title":"Films","type":"movie"}]}}`)
	}))
	defer pms.Close()

	var clientID string
	cloud := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/api/v2/pins":
			clientID = r.Header.Get("X-Plex-Client-Identifier")
			if r.URL.Query().Get("strong") != "true" {
				http.Error(w, "legacy pin must be strong", http.StatusBadRequest)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprint(w, `{"id":123,"code":"pin-code","expiresIn":600}`)
		case r.Method == http.MethodGet && r.URL.Path == "/api/v2/pins/123":
			if r.URL.Query().Get("deviceJWT") != "" {
				http.Error(w, "unexpected device JWT", http.StatusBadRequest)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprint(w, `{"authToken":"legacy-account-token"}`)
		case r.Method == http.MethodGet && r.URL.Path == "/api/resources":
			if r.Header.Get("X-Plex-Token") != "legacy-account-token" || r.Header.Get("Accept") != "application/xml" {
				http.Error(w, "bad legacy resource request", http.StatusUnauthorized)
				return
			}
			w.Header().Set("Content-Type", "application/xml")
			fmt.Fprintf(w, `<MediaContainer size="1"><Device name="Home Plex" clientIdentifier="server-1" provides="server" owned="1" platform="Linux" accessToken="legacy-server-token"><Connection uri=%q local="1" relay="0"/></Device></MediaContainer>`, pms.URL)
		default:
			http.NotFound(w, r)
		}
	}))
	defer cloud.Close()

	store := &memoryAuthStore{}
	manager, err := plex.NewAuthManager(context.Background(), store, nil, cloud.URL, pms.URL, false)
	if err != nil {
		t.Fatal(err)
	}
	started, err := manager.Start(context.Background(), plex.AuthMethodLegacy)
	if err != nil {
		t.Fatal(err)
	}
	status, err := manager.Status(context.Background(), started.SetupToken)
	if err != nil || status.Status != "authorized" || len(status.Servers) != 1 {
		t.Fatalf("unexpected auth status: %#v, %v", status, err)
	}
	if err := manager.SelectServer(context.Background(), started.SetupToken, "server-1"); err != nil {
		t.Fatal(err)
	}
	if clientID == "" || store.state == nil || store.state.Method != plex.AuthMethodLegacy || len(store.state.PrivateKey) != 0 || !store.state.TokenExpiresAt.IsZero() {
		t.Fatalf("unexpected legacy state: %#v", store.state)
	}
	if libraries, err := manager.Libraries(context.Background()); err != nil || len(libraries) != 1 {
		t.Fatalf("libraries=%#v err=%v", libraries, err)
	}
	if err := manager.Refresh(context.Background()); err != nil {
		t.Fatalf("legacy refresh should be a no-op: %v", err)
	}
}

// TestJWTAuthorizationRequiresExperimentalMode verifies JWT cannot be started accidentally.
func TestJWTAuthorizationRequiresExperimentalMode(t *testing.T) {
	manager, err := plex.NewAuthManager(context.Background(), &memoryAuthStore{}, nil, "https://plex.test", "", false)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := manager.Start(context.Background(), plex.AuthMethodJWT); !errors.Is(err, plex.ErrExperimentalAuthDisabled) {
		t.Fatalf("expected experimental authentication error, got %v", err)
	}
}

// TestJWTAuthorizationServerSelectionAndRefresh verifies the complete Plex authentication lifecycle.
func TestJWTAuthorizationServerSelectionAndRefresh(t *testing.T) {
	pms := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if token := r.Header.Get("X-Plex-Token"); !strings.HasPrefix(token, "e30.") {
			http.Error(w, "bad server token", http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"MediaContainer":{"Directory":[{"key":"1","title":"Films","type":"movie"},{"key":"2","title":"Series","type":"show"}]}}`)
	}))
	defer pms.Close()
	discoveredURL := "http://127.0.0.1:1"

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
			decoded, _ := base64.RawURLEncoding.DecodeString(body.JWK.X)
			publicKey = ed25519.PublicKey(decoded)
			fmt.Fprint(w, `{"id":123,"code":"pin-code","expiresIn":600}`)
		case r.Method == http.MethodGet && r.URL.Path == "/api/v2/pins/123":
			assertDeviceJWT(t, r.URL.Query().Get("deviceJWT"), publicKey, clientID, "")
			fmt.Fprintf(w, `{"authToken":%q}`, testJWT(time.Now().Add(7*24*time.Hour), "user"))
		case r.Method == http.MethodGet && r.URL.Path == "/api/v2/resources":
			pmsToken := "server-token"
			if refreshed.Load() {
				pmsToken = "server-token-2"
			}
			fmt.Fprintf(w, `[{"name":"Home Plex","clientIdentifier":"server-1","provides":"server","owned":true,"platform":"Linux","accessToken":%q,"connections":[{"uri":%q,"local":true,"relay":false}]}]`, pmsToken, discoveredURL)
		case r.Method == http.MethodGet && r.URL.Path == "/api/v2/auth/nonce":
			fmt.Fprint(w, `{"nonce":"refresh-nonce"}`)
		case r.Method == http.MethodPost && r.URL.Path == "/api/v2/auth/token":
			var body refreshRequestBody
			_ = json.NewDecoder(r.Body).Decode(&body)
			assertDeviceJWT(t, body.JWT, publicKey, clientID, "refresh-nonce")
			refreshed.Store(true)
			fmt.Fprintf(w, `{"auth_token":%q}`, testJWT(time.Now().Add(7*24*time.Hour), "refreshed"))
		default:
			http.NotFound(w, r)
		}
	}))
	defer cloud.Close()

	store := &memoryAuthStore{}
	var logs bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&logs, &slog.HandlerOptions{Level: slog.LevelDebug}))
	manager, err := plex.NewAuthManager(context.Background(), store, logger, cloud.URL, pms.URL, true)
	if err != nil {
		t.Fatal(err)
	}
	started, err := manager.Start(context.Background(), plex.AuthMethodJWT)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(started.AuthURL, "app.plex.tv/auth") || started.SetupToken == "" {
		t.Fatalf("unexpected auth start: %#v", started)
	}
	status, err := manager.Status(context.Background(), started.SetupToken)
	if err != nil || status.Status != "authorized" || len(status.Servers) != 1 || !status.Servers[0].Local {
		t.Fatalf("unexpected auth status: %#v, %v", status, err)
	}
	if err := manager.SelectServer(context.Background(), started.SetupToken, "server-1"); err != nil {
		t.Fatal(err)
	}
	configured, name := manager.Configured()
	if !configured || name != "Home Plex" {
		t.Fatalf("configured=%v name=%q", configured, name)
	}
	libraries, err := manager.Libraries(context.Background())
	if err != nil || len(libraries) != 2 || libraries[1].Type != "show" {
		t.Fatalf("libraries=%#v err=%v", libraries, err)
	}
	beforeRefresh := store.state.UserToken
	if err := manager.Refresh(context.Background()); err != nil {
		t.Fatal(err)
	}
	if store.state == nil || store.state.ServerToken != store.state.UserToken || store.state.UserToken == beforeRefresh || store.state.ServerURL != discoveredURL {
		t.Fatalf("refresh was not persisted: %#v", store.state)
	}
	for _, event := range []string{"plex_auth_started", "plex_servers_discovered", "plex_connection_selected", "plex_server_verified", "plex_token_refreshed"} {
		if !strings.Contains(logs.String(), `"event":"`+event+`"`) {
			t.Fatalf("missing log event %q in %s", event, logs.String())
		}
	}
	for _, secret := range []string{"server-token", "server-token-2"} {
		if strings.Contains(logs.String(), secret) {
			t.Fatalf("logs contain Plex token %q", secret)
		}
	}
}

// assertDeviceJWT verifies the signature and claims of a device JWT.
func assertDeviceJWT(t *testing.T, token string, publicKey ed25519.PublicKey, issuer, nonce string) {
	t.Helper()
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		t.Fatalf("invalid device JWT: %q", token)
	}
	signature, _ := base64.RawURLEncoding.DecodeString(parts[2])
	if !ed25519.Verify(publicKey, []byte(parts[0]+"."+parts[1]), signature) {
		t.Fatal("device JWT signature did not verify")
	}
	payload, _ := base64.RawURLEncoding.DecodeString(parts[1])
	var claims map[string]any
	_ = json.Unmarshal(payload, &claims)
	if claims["aud"] != "plex.tv" || claims["iss"] != issuer {
		t.Fatalf("unexpected claims: %#v", claims)
	}
	if nonce != "" && claims["nonce"] != nonce {
		t.Fatalf("missing nonce in claims: %#v", claims)
	}
}

// testJWT creates an unsigned JWT payload for expiration tests.
func testJWT(expires time.Time, marker string) string {
	payload, _ := json.Marshal(map[string]any{"exp": expires.Unix(), "marker": marker})
	return "e30." + base64.RawURLEncoding.EncodeToString(payload) + ".signature"
}

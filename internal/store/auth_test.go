package store

import (
	"bytes"
	"crypto/ed25519"
	"crypto/rand"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/gi8lino/screendeck/internal/plex"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestPlexAuthenticationIsEncryptedAtRest verifies stored Plex secrets are encrypted.
func TestPlexAuthenticationIsEncryptedAtRest(t *testing.T) {
	ctx := t.Context()
	directory := t.TempDir()
	keyPath := filepath.Join(directory, "auth.key")
	database, err := Open(filepath.Join(directory, "test.db"), keyPath)
	require.NoError(t, err)

	_, privateKey, err := ed25519.GenerateKey(rand.Reader)
	require.NoError(t, err)
	state := plex.AuthState{
		Method: plex.AuthMethodJWT, ClientID: "client", KeyID: "key", PrivateKey: privateKey, UserToken: "secret-user-token",
		TokenExpiresAt: time.Now().Add(time.Hour), ServerID: "server", ServerName: "Plex",
		ServerURL: "http://plex.test:32400", ServerToken: "secret-server-token",
	}
	require.NoError(t, database.SavePlexAuth(ctx, state))

	loaded, err := database.LoadPlexAuth(ctx)
	require.NoError(t, err)
	assert.Equal(t, plex.AuthMethodJWT, loaded.Method)
	assert.Equal(t, state.UserToken, loaded.UserToken)
	assert.Equal(t, state.ServerToken, loaded.ServerToken)
	assert.Equal(t, state.PrivateKey, loaded.PrivateKey)

	var authMethod string
	var encryptedPrivate, encryptedUser, encryptedServer []byte
	const encryptedAuthQuery = `
SELECT
  auth_method,
  private_key,
  user_token,
  server_token
FROM plex_auth
WHERE id = 1
`
	require.NoError(t, database.db.QueryRow(encryptedAuthQuery).Scan(&authMethod, &encryptedPrivate, &encryptedUser, &encryptedServer))
	assert.Equal(t, string(plex.AuthMethodJWT), authMethod)
	assert.False(t, bytes.Contains(encryptedPrivate, privateKey))
	assert.False(t, bytes.Contains(encryptedUser, []byte(state.UserToken)))
	assert.False(t, bytes.Contains(encryptedServer, []byte(state.ServerToken)))

	info, err := os.Stat(keyPath)
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o600), info.Mode().Perm())

	require.NoError(t, database.Close())
	reopened, err := Open(filepath.Join(directory, "test.db"), keyPath)
	require.NoError(t, err)
	defer reopened.Close() // nolint:errcheck

	reloaded, err := reopened.LoadPlexAuth(ctx)
	require.NoError(t, err)
	assert.Equal(t, state.UserToken, reloaded.UserToken)
	assert.Equal(t, state.PrivateKey, reloaded.PrivateKey)
}

// TestStandardPlexAuthenticationRoundTrip verifies standard authentication does not require a device key.
func TestStandardPlexAuthenticationRoundTrip(t *testing.T) {
	database, err := Open(":memory:")
	require.NoError(t, err)
	defer database.Close() // nolint:errcheck

	state := plex.AuthState{
		Method: plex.AuthMethodStandard, ClientID: "client", UserToken: "standard-user-token",
		ServerID: "server", ServerName: "Plex", ServerURL: "http://plex.test:32400", ServerToken: "standard-server-token",
	}
	require.NoError(t, database.SavePlexAuth(t.Context(), state))

	loaded, err := database.LoadPlexAuth(t.Context())
	require.NoError(t, err)
	assert.Equal(t, plex.AuthMethodStandard, loaded.Method)
	assert.Empty(t, loaded.PrivateKey)
	assert.Equal(t, state.UserToken, loaded.UserToken)
	assert.Equal(t, state.ServerToken, loaded.ServerToken)
}

// TestValidatePlexAuthState verifies stored Plex key material matches the selected authentication method.
func TestValidatePlexAuthState(t *testing.T) {
	t.Run("standard without private key", func(t *testing.T) {
		require.NoError(t, validatePlexAuthState(plex.AuthState{Method: plex.AuthMethodStandard}))
	})

	t.Run("standard rejects private key", func(t *testing.T) {
		err := validatePlexAuthState(plex.AuthState{
			Method:     plex.AuthMethodStandard,
			PrivateKey: ed25519.PrivateKey(make([]byte, ed25519.PrivateKeySize)),
		})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "must not contain a private key")
	})

	t.Run("JWT accepts Ed25519 private key", func(t *testing.T) {
		require.NoError(t, validatePlexAuthState(plex.AuthState{
			Method:     plex.AuthMethodJWT,
			PrivateKey: ed25519.PrivateKey(make([]byte, ed25519.PrivateKeySize)),
		}))
	})

	t.Run("JWT rejects invalid private key", func(t *testing.T) {
		err := validatePlexAuthState(plex.AuthState{
			Method:     plex.AuthMethodJWT,
			PrivateKey: ed25519.PrivateKey(make([]byte, 10)),
		})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "valid Ed25519 private key")
	})

	t.Run("rejects invalid method", func(t *testing.T) {
		err := validatePlexAuthState(plex.AuthState{Method: plex.AuthMethod("invalid")})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "invalid Plex authentication method")
	})
}

// TestApplyStoredPlexPrivateKey verifies persisted key bytes are validated before assignment.
func TestApplyStoredPlexPrivateKey(t *testing.T) {
	t.Run("standard accepts empty key", func(t *testing.T) {
		state := plex.AuthState{Method: plex.AuthMethodStandard}
		require.NoError(t, applyStoredPlexPrivateKey(&state, nil))
		assert.Empty(t, state.PrivateKey)
	})

	t.Run("standard rejects stored key", func(t *testing.T) {
		state := plex.AuthState{Method: plex.AuthMethodStandard}
		err := applyStoredPlexPrivateKey(&state, []byte("unexpected"))
		require.Error(t, err)
		assert.Contains(t, err.Error(), "contains a private key")
	})

	t.Run("JWT assigns valid key", func(t *testing.T) {
		privateKey := make([]byte, ed25519.PrivateKeySize)
		state := plex.AuthState{Method: plex.AuthMethodJWT}
		require.NoError(t, applyStoredPlexPrivateKey(&state, privateKey))
		assert.Equal(t, ed25519.PrivateKey(privateKey), state.PrivateKey)
	})

	t.Run("JWT rejects invalid size", func(t *testing.T) {
		state := plex.AuthState{Method: plex.AuthMethodJWT}
		err := applyStoredPlexPrivateKey(&state, []byte("short"))
		require.Error(t, err)
		assert.Contains(t, err.Error(), "invalid size")
	})

	t.Run("rejects invalid method", func(t *testing.T) {
		state := plex.AuthState{Method: plex.AuthMethod("invalid")}
		err := applyStoredPlexPrivateKey(&state, nil)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "method is invalid")
	})
}

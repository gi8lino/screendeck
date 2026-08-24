package jellyfin

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type memoryAuthStore struct {
	state *AuthState
}

func (s *memoryAuthStore) LoadJellyfinAuth(context.Context) (AuthState, error) {
	if s.state == nil {
		return AuthState{}, ErrAuthNotFound
	}
	return *s.state, nil
}

func (s *memoryAuthStore) SaveJellyfinAuth(_ context.Context, state AuthState) error {
	copyOf := state
	s.state = &copyOf
	return nil
}

func TestAuthManagerConnect(t *testing.T) {
	t.Parallel()

	t.Run("persists access token without password", func(t *testing.T) {
		t.Parallel()

		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			switch r.URL.Path {
			case "/System/Info/Public":
				fmt.Fprint(w, `{"ServerName":"Home Jellyfin","Id":"server-1"}`) // nolint:errcheck
			case "/Users/AuthenticateByName":
				fmt.Fprint(w, `{"AccessToken":"access-token","ServerId":"server-1","User":{"Id":"user-1","Name":"Alice"}}`) // nolint:errcheck
			default:
				http.NotFound(w, r)
			}
		}))
		defer server.Close()

		store := &memoryAuthStore{}
		manager, err := NewProvider(t.Context(), store, nil, "test")
		require.NoError(t, err)
		require.NoError(t, manager.Connect(t.Context(), server.URL, " alice ", "secret-password"))

		configured, serverName := manager.Configured()
		assert.True(t, configured)
		assert.Equal(t, "Home Jellyfin", serverName)
		require.NotNil(t, store.state)
		assert.Equal(t, "access-token", store.state.AccessToken)
		assert.Equal(t, "Alice", store.state.Username)
		assert.NotEmpty(t, store.state.DeviceID)
		assert.NotContains(t, fmt.Sprintf("%+v", store.state), "secret-password")
	})
}

func TestAuthManagerConfigured(t *testing.T) {
	t.Parallel()

	t.Run("restores saved state", func(t *testing.T) {
		t.Parallel()

		store := &memoryAuthStore{state: &AuthState{
			ServerID:    "server-1",
			ServerName:  "Home Jellyfin",
			ServerURL:   "http://jellyfin.test:8096",
			UserID:      "user-1",
			Username:    "Alice",
			AccessToken: "access-token",
			DeviceID:    "device",
		}}
		manager, err := NewProvider(t.Context(), store, nil, "test")
		require.NoError(t, err)
		configured, serverName := manager.Configured()
		assert.True(t, configured)
		assert.Equal(t, "Home Jellyfin", serverName)
	})
}

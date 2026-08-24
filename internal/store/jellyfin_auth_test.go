package store

import (
	"testing"

	"github.com/gi8lino/screendeck/internal/jellyfin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestJellyfinAuth(t *testing.T) {
	database, err := Open(":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { _ = database.Close() })

	state := jellyfin.AuthState{
		ServerID:    "server",
		ServerName:  "Jellyfin",
		ServerURL:   "http://jellyfin.test:8096",
		UserID:      "user",
		Username:    "screen",
		AccessToken: "secret-token",
		DeviceID:    "device",
	}
	require.NoError(t, database.SaveJellyfinAuth(t.Context(), state))

	loaded, err := database.LoadJellyfinAuth(t.Context())
	require.NoError(t, err)
	assert.Equal(t, state, loaded)

	var stored []byte
	require.NoError(t, database.db.QueryRow(`SELECT access_token FROM jellyfin_auth WHERE id = 1`).Scan(&stored))
	assert.NotContains(t, string(stored), state.AccessToken)
}

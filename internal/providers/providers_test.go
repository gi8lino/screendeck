package providers_test

import (
	"io"
	"log/slog"
	"testing"

	"github.com/gi8lino/screendeck/internal/media"
	"github.com/gi8lino/screendeck/internal/plex"
	"github.com/gi8lino/screendeck/internal/providers"
	"github.com/gi8lino/screendeck/internal/store"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestNew verifies construction and restoration of the supported media providers.
func TestNew(t *testing.T) {
	t.Run("creates supported providers", func(t *testing.T) {
		database, err := store.Open(":memory:", "")
		require.NoError(t, err)
		defer database.Close() // nolint:errcheck

		services, err := providers.New(
			t.Context(),
			database,
			slog.New(slog.NewTextHandler(io.Discard, nil)),
			providers.Options{Version: "test"},
		)
		require.NoError(t, err)
		require.NotNil(t, services.Media)
		require.NotNil(t, services.Plex)
		require.NotNil(t, services.Jellyfin)
		assert.Equal(t, media.ProviderPlex, services.Plex.ID())
		assert.Equal(t, media.ProviderJellyfin, services.Jellyfin.ID())
	})

	t.Run("restores configured provider", func(t *testing.T) {
		database, err := store.Open(":memory:", "")
		require.NoError(t, err)
		defer database.Close() // nolint:errcheck

		require.NoError(t, database.SavePlexAuth(t.Context(), plex.AuthState{
			Method:      plex.AuthMethodStandard,
			ClientID:    "client",
			ServerID:    "server",
			ServerName:  "Test Plex",
			ServerURL:   "http://plex.test",
			ServerToken: "token",
		}))

		services, err := providers.New(
			t.Context(),
			database,
			slog.New(slog.NewTextHandler(io.Discard, nil)),
			providers.Options{Version: "test"},
		)
		require.NoError(t, err)
		assert.Equal(t, media.Status{
			Configured: true,
			Provider:   media.ProviderPlex,
			ServerName: "Test Plex",
		}, services.Media.Status())
	})
}

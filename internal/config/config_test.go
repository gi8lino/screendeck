package config

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestParse verifies command-line and environment configuration parsing and validation.
func TestParse(t *testing.T) {
	t.Run("flags", func(t *testing.T) {
		cfg, err := Parse([]string{
			"--listen-address", "127.0.0.1:9090",
			"--database-path", ":memory:",
			"--auth-key-path", "/tmp/test-auth.key",
			"--base-url", "http://movies.test/",
			"--exclude-libraries", "Kids, Archive",
			"--plex-url-override", "http://127.0.0.1:32400/",
			"--room-ttl", "2h",
			"--log-format", "text",
			"--experimental",
			"--debug",
			"--access-log",
		}, "test")
		require.NoError(t, err)
		assert.Equal(t, "127.0.0.1:9090", cfg.ListenAddress)
		assert.Equal(t, "http://movies.test", cfg.BaseURL)
		assert.Equal(t, []string{"Kids", "Archive"}, cfg.ExcludeLibraries)
		assert.Equal(t, "http://127.0.0.1:32400", cfg.PlexURLOverride)
		assert.Equal(t, 2*time.Hour, cfg.RoomTTL)
		assert.Equal(t, "text", string(cfg.LogFormat))
		assert.True(t, cfg.Debug)
		assert.True(t, cfg.AccessLog)
		assert.True(t, cfg.Experimental)
	})

	t.Run("environment", func(t *testing.T) {
		t.Setenv("SCREENDECK__AUTH_KEY_PATH", "/tmp/from-env.key")
		t.Setenv("SCREENDECK__EXCLUDE_LIBRARIES", "Kids, Archive")
		t.Setenv("SCREENDECK__PLEX_URL_OVERRIDE", "http://127.0.0.1:32400")
		t.Setenv("SCREENDECK__ROOM_TTL", "30m")
		t.Setenv("SCREENDECK__ACCESS_LOG", "true")

		cfg, err := Parse(nil, "test")
		require.NoError(t, err)
		assert.Equal(t, "/tmp/from-env.key", cfg.AuthKeyPath)
		assert.Equal(t, []string{"Kids", "Archive"}, cfg.ExcludeLibraries)
		assert.Equal(t, "http://127.0.0.1:32400", cfg.PlexURLOverride)
		assert.Equal(t, 30*time.Minute, cfg.RoomTTL)
		assert.True(t, cfg.AccessLog)
	})

	t.Run("rejects invalid Plex URL override", func(t *testing.T) {
		_, err := Parse([]string{"--plex-url-override", "localhost:32400"}, "test")
		require.Error(t, err)
	})

	t.Run("rejects non-absolute public URL", func(t *testing.T) {
		_, err := Parse([]string{"--base-url", "/screendeck"}, "test")
		require.Error(t, err)
	})

	t.Run("rejects non-HTTP public URL", func(t *testing.T) {
		_, err := Parse([]string{"--base-url", "mailto:host@example.test"}, "test")
		require.Error(t, err)
	})

	t.Run("rejects zero room TTL", func(t *testing.T) {
		_, err := Parse([]string{"--room-ttl", "0s"}, "test")
		require.Error(t, err)
	})

	t.Run("rejects zero cleanup interval", func(t *testing.T) {
		_, err := Parse([]string{"--room-cleanup-interval", "0s"}, "test")
		require.Error(t, err)
	})
}

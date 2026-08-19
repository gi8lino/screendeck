package config

import (
	"testing"
	"time"
)

// TestParseFlags verifies command-line configuration parsing.
func TestParseFlags(t *testing.T) {
	cfg, err := Parse([]string{
		"--listen-address", "127.0.0.1:9090",
		"--database-path", ":memory:",
		"--auth-key-path", "/tmp/test-auth.key",
		"--base-url", "http://movies.test/",
		"--plex-url-override", "http://127.0.0.1:32400/",
		"--room-ttl", "2h",
		"--log-format", "text",
		"--experimental",
		"--debug",
	}, "test")
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	if cfg.ListenAddress != "127.0.0.1:9090" || cfg.BaseURL != "http://movies.test" || cfg.PlexURLOverride != "http://127.0.0.1:32400" {
		t.Fatalf("unexpected addresses: %#v", cfg)
	}
	if cfg.RoomTTL != 2*time.Hour || cfg.LogFormat != "text" || !cfg.Debug || !cfg.Experimental {
		t.Fatalf("unexpected options: %#v", cfg)
	}
}

// TestParseEnvironment verifies environment configuration parsing.
func TestParseEnvironment(t *testing.T) {
	t.Setenv("SCREENDECK__AUTH_KEY_PATH", "/tmp/from-env.key")
	t.Setenv("SCREENDECK__PLEX_URL_OVERRIDE", "http://127.0.0.1:32400")
	t.Setenv("SCREENDECK__ROOM_TTL", "30m")
	cfg, err := Parse(nil, "test")
	if err != nil {
		t.Fatal(err)
	}
	if cfg.AuthKeyPath != "/tmp/from-env.key" || cfg.PlexURLOverride != "http://127.0.0.1:32400" || cfg.RoomTTL != 30*time.Minute {
		t.Fatalf("environment was not applied: %#v", cfg)
	}
}

// TestParseRejectsInvalidPlexURLOverride verifies the override requires an absolute HTTP URL.
func TestParseRejectsInvalidPlexURLOverride(t *testing.T) {
	if _, err := Parse([]string{"--plex-url-override", "localhost:32400"}, "test"); err == nil {
		t.Fatal("expected invalid Plex URL override to fail")
	}
}

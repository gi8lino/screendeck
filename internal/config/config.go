package config

import (
	"fmt"
	"net"
	"net/url"
	"strings"
	"time"

	"github.com/containeroo/tinyflags"
	"github.com/gi8lino/screendeck/internal/logging"
)

// Config contains runtime configuration for ScreenDeck.
type Config struct {
	// ListenAddress is the TCP address used by the HTTP server.
	ListenAddress string
	// DatabasePath is the path to the SQLite database.
	DatabasePath string
	// AuthKeyPath is the path to the local encryption key.
	AuthKeyPath string
	// BaseURL is the public URL used to generate room links.
	BaseURL string
	// PlexURLOverride replaces the discovered Plex server URL at runtime.
	PlexURLOverride string
	// RoomTTL controls how long rooms remain active.
	RoomTTL time.Duration
	// LogFormat selects structured text or JSON logging.
	LogFormat logging.LogFormat
	// Debug enables verbose request and diagnostic logging.
	Debug bool
	// Experimental enables experimental application features.
	Experimental bool
	// RoomCleanupInterval controls how often expired rooms are removed.
	RoomCleanupInterval time.Duration
	// ExcludeLibraries contains Plex library titles or keys excluded from room creation.
	ExcludeLibraries []string
	// Overridden records configuration values explicitly overridden by flags or environment variables.
	Overridden map[string]any
}

// Parse reads command-line and environment configuration.
func Parse(args []string, version string) (Config, error) {
	cfg := Config{}
	tf := tinyflags.NewFlagSet("screendeck", tinyflags.ContinueOnError)
	tf.Version(version)
	tf.EnvPrefix("SCREENDECK_")

	// Server
	listen := tf.TCPAddr("listen-address", &net.TCPAddr{Port: 8080}, "Address on which the web server listens").
		Short("a").
		Placeholder("ADDR").
		Value()

	// Authentication
	tf.StringVar(&cfg.DatabasePath, "database-path", "./data/screendeck.db", "Path to the SQLite database").
		Placeholder("PATH").
		Value()
	tf.StringVar(&cfg.AuthKeyPath, "auth-key-path", "./data/auth.key", "Path to the local authentication encryption key").
		Placeholder("PATH").
		Value()

	// Application
	tf.StringVar(&cfg.BaseURL, "base-url", "http://localhost:8080", "Public URL used for room links").
		Validate(func(u string) error {
			if _, err := url.ParseRequestURI(u); err != nil {
				return err
			}
			return nil
		}).
		Finalize(func(url string) string {
			return strings.TrimRight(url, "/")
		}).
		Placeholder("URL").
		Value()

	tf.DurationVar(&cfg.RoomCleanupInterval, "room-cleanup-interval", time.Hour, "How often expired rooms are deleted").
		Validate(func(d time.Duration) error {
			if d <= 0 {
				return fmt.Errorf("room-cleanup-interval must be a positive duration")
			}
			return nil
		}).
		Placeholder("DURATION").
		Value()

	tf.DurationVar(&cfg.RoomTTL, "room-ttl", 24*time.Hour, "How long rooms remain available").
		Validate(func(d time.Duration) error {
			if d <= 0 {
				return fmt.Errorf("room-ttl must be a positive duration")
			}
			return nil
		}).
		Placeholder("DURATION").Value()

	tf.StringSliceVar(&cfg.ExcludeLibraries, "exclude-libraries", []string{}, "Plex library titles or keys to exclude from room creation").
		TrimSpace().
		Placeholder("LIBRARY").
		Value()

	tf.StringVar(&cfg.PlexURLOverride, "plex-url-override", "", "Override the discovered Plex server URL").
		Validate(func(rawURL string) error {
			if rawURL == "" {
				return nil
			}
			if !validAbsoluteHTTPURL(rawURL) {
				return fmt.Errorf("must be an absolute HTTP or HTTPS URL")
			}
			return nil
		}).
		Finalize(func(rawURL string) string {
			return strings.TrimRight(rawURL, "/")
		}).
		Placeholder("URL").
		Value()

	// Logging
	tf.BoolVar(&cfg.Debug, "debug", false, "Enable debug request logging").
		Short("d").
		Value()

	logFormat := tf.String("log-format", "json", "Log output format").
		Choices(string(logging.LogFormatText), string(logging.LogFormatJSON)).
		Short("l").
		Placeholder("FORMAT").
		Value()

		// Experimental
	tf.BoolVar(&cfg.Experimental, "experimental", false, "Show experimental features, including Plex JWT authentication").Value()

	if err := tf.Parse(args); err != nil {
		return Config{}, err
	}
	cfg.ListenAddress = (*listen).String()
	cfg.LogFormat = logging.LogFormat(*logFormat)
	cfg.Overridden = tf.OverriddenValues()

	return cfg, nil
}

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

type Config struct {
	ListenAddress   string
	DatabasePath    string
	AuthKeyPath     string
	BaseURL         string
	PlexURLOverride string
	RoomTTL         time.Duration
	LogFormat       logging.LogFormat
	Debug           bool
	Experimental    bool
	Overridden      map[string]any
}

// Parse reads command-line and environment configuration.
func Parse(args []string, version string) (Config, error) {
	cfg := Config{}
	tf := tinyflags.NewFlagSet("screendeck", tinyflags.ContinueOnError)
	tf.Version(version)
	tf.EnvPrefix("SCREENDECK_")

	listen := tf.TCPAddr("listen-address", &net.TCPAddr{Port: 8080}, "Address on which the web server listens").
		Short("a").
		Placeholder("ADDR").
		Value()
	tf.StringVar(&cfg.DatabasePath, "database-path", "./data/screendeck.db", "Path to the SQLite database").
		Placeholder("PATH").
		Value()
	tf.StringVar(&cfg.AuthKeyPath, "auth-key-path", "./data/auth.key", "Path to the local authentication encryption key").
		Placeholder("PATH").
		Value()

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

	tf.StringVar(&cfg.PlexURLOverride, "plex-url-override", "", "Override the discovered Plex server URL").
		Validate(func(rawURL string) error {
			if rawURL == "" {
				return nil
			}
			parsed, err := url.ParseRequestURI(rawURL)
			if err != nil || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") {
				return fmt.Errorf("must be an absolute HTTP or HTTPS URL")
			}
			return nil
		}).
		Finalize(func(rawURL string) string {
			return strings.TrimRight(rawURL, "/")
		}).
		Placeholder("URL").
		Value()

	tf.DurationVar(&cfg.RoomTTL, "room-ttl", 24*time.Hour, "How long rooms remain available").
		Validate(func(d time.Duration) error {
			if d <= 0 {
				return fmt.Errorf("room-ttl must be a positive duration")
			}
			return nil
		}).
		Placeholder("DURATION").Value()

	tf.BoolVar(&cfg.Debug, "debug", false, "Enable debug request logging").
		Short("d").
		Value()

	tf.BoolVar(&cfg.Experimental, "experimental", false, "Show experimental features, including Plex JWT authentication").Value()

	logFormat := tf.String("log-format", "json", "Log output format").
		Choices(string(logging.LogFormatText), string(logging.LogFormatJSON)).
		Short("l").Placeholder("FORMAT").Value()

	if err := tf.Parse(args); err != nil {
		return Config{}, err
	}
	cfg.ListenAddress = (*listen).String()
	cfg.LogFormat = logging.LogFormat(*logFormat)
	cfg.Overridden = tf.OverriddenValues()

	return cfg, nil
}

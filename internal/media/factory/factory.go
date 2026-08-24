package factory

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/gi8lino/screendeck/internal/jellyfin"
	"github.com/gi8lino/screendeck/internal/media"
	"github.com/gi8lino/screendeck/internal/plex"
)

// Store contains the persistence contracts required by all media providers and the provider manager.
type Store interface {
	media.ProviderStore
	plex.AuthStore
	jellyfin.AuthStore
}

// Options contains provider-specific construction settings supplied by the application layer.
type Options struct {
	// Version identifies the ScreenDeck version sent to media servers.
	Version string
	// PlexURLOverride replaces the discovered Plex server URL at runtime.
	PlexURLOverride string
	// Experimental enables experimental Plex authentication features.
	Experimental bool
}

// Services contains the provider-neutral manager and provider-specific setup services.
type Services struct {
	// Media selects the active provider for runtime catalog operations.
	Media *media.Manager
	// Plex exposes Plex-specific authorization and server selection.
	Plex *plex.AuthManager
	// Jellyfin exposes Jellyfin-specific login and setup.
	Jellyfin *jellyfin.AuthManager
}

// Factory constructs all supported media providers and wires the active provider manager.
type Factory struct {
	store   Store
	logger  *slog.Logger
	options Options
}

// New creates a media provider factory.
func New(store Store, logger *slog.Logger, options Options) *Factory {
	if logger == nil {
		logger = slog.Default()
	}
	return &Factory{store: store, logger: logger, options: options}
}

// Create constructs provider-specific services and restores the active provider.
func (f *Factory) Create(ctx context.Context) (Services, error) {
	plexAuth, err := plex.NewProvider(
		ctx,
		f.store,
		f.logger.With("component", "plex"),
		f.options.PlexURLOverride,
		f.options.Experimental,
	)
	if err != nil {
		return Services{}, fmt.Errorf("configure Plex: %w", err)
	}

	jellyfinAuth, err := jellyfin.NewAuthManager(
		ctx,
		f.store,
		f.logger.With("component", "jellyfin"),
		f.options.Version,
	)
	if err != nil {
		return Services{}, fmt.Errorf("configure Jellyfin: %w", err)
	}

	manager, err := media.NewManager(ctx, f.store, plexAuth, jellyfinAuth)
	if err != nil {
		return Services{}, fmt.Errorf("configure media provider: %w", err)
	}

	return Services{
		Media:    manager,
		Plex:     plexAuth,
		Jellyfin: jellyfinAuth,
	}, nil
}

// Package providers assembles the supported media-provider integrations.
package providers

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

// New constructs all supported media providers and restores the active provider.
func New(
	ctx context.Context,
	store Store,
	logger *slog.Logger,
	options Options,
) (Services, error) {
	if logger == nil {
		logger = slog.Default()
	}

	plexAuth, err := plex.NewProvider(
		ctx,
		store,
		logger.With("component", "plex"),
		options.PlexURLOverride,
		options.Experimental,
	)
	if err != nil {
		return Services{}, fmt.Errorf("configure Plex: %w", err)
	}

	jellyfinAuth, err := jellyfin.NewProvider(
		ctx,
		store,
		logger.With("component", "jellyfin"),
		options.Version,
	)
	if err != nil {
		return Services{}, fmt.Errorf("configure Jellyfin: %w", err)
	}

	manager, err := media.NewManager(ctx, store, plexAuth, jellyfinAuth)
	if err != nil {
		return Services{}, fmt.Errorf("configure media provider: %w", err)
	}

	return Services{
		Media:    manager,
		Plex:     plexAuth,
		Jellyfin: jellyfinAuth,
	}, nil
}

package jellyfin

import (
	"context"
	"log/slog"

	"github.com/gi8lino/screendeck/internal/media"
)

var _ media.Provider = (*AuthManager)(nil) // Ensure AuthManager implements media provider interface.

// NewProvider creates the Jellyfin provider and restores saved authentication state.
func NewProvider(
	ctx context.Context,
	store AuthStore,
	logger *slog.Logger,
	version string,
) (*AuthManager, error) {
	return newAuthManager(ctx, store, logger, version)
}

// ID returns Jellyfin's stable media provider identifier.
func (*AuthManager) ID() media.ProviderID {
	return media.ProviderJellyfin
}

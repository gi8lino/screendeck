package plex

import (
	"context"
	"log/slog"

	"github.com/gi8lino/screendeck/internal/media"
)

const (
	cloudURL         = "https://clients.plex.tv"
	authorizationURL = "https://app.plex.tv/auth"
)

var _ media.Provider = (*AuthManager)(nil) // Ensure AuthManager implements media provider interface.

// NewProvider creates the Plex provider using the standard Plex cloud service.
func NewProvider(
	ctx context.Context,
	store AuthStore,
	logger *slog.Logger,
	serverURLOverride string,
	experimental bool,
) (*AuthManager, error) {
	return NewAuthManager(
		ctx,
		store,
		logger,
		cloudURL,
		serverURLOverride,
		experimental,
	)
}

// ID returns Plex's stable media provider identifier.
func (*AuthManager) ID() media.ProviderID {
	return media.ProviderPlex
}

package jellyfin

import "github.com/gi8lino/screendeck/internal/media"

var _ media.Provider = (*AuthManager)(nil) // Ensure AuthManager implements media provider interface.

// ID returns Jellyfin's stable media provider identifier.
func (*AuthManager) ID() media.ProviderID {
	return media.ProviderJellyfin
}

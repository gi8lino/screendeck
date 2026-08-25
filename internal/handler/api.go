package handler

import (
	"log/slog"

	"github.com/gi8lino/screendeck/internal/jellyfin"
	"github.com/gi8lino/screendeck/internal/media"
	"github.com/gi8lino/screendeck/internal/plex"
	"github.com/gi8lino/screendeck/internal/room"
)

// API bundles shared handler dependencies and runtime configuration.
type API struct {
	// Version is the application version reported by the API.
	Version string
	// Commit is the source revision reported by the API.
	Commit string
	// BaseURL is the public URL used to generate room links.
	BaseURL string
	// Experimental enables experimental application features.
	Experimental bool
	// Logger records handler diagnostics.
	Logger *slog.Logger
	// Rooms provides room application operations.
	Rooms *room.Service
	// Media selects the active media provider used by room catalog operations.
	Media *media.Manager
	// Plex manages Plex-specific authentication and server selection.
	Plex *plex.AuthManager
	// Jellyfin manages Jellyfin-specific authentication and catalog access.
	Jellyfin *jellyfin.AuthManager
	// healthProber provides the dependency check used by the health endpoint.
	healthProber healthProber
}

// New creates an API with the supplied application services.
func New(
	version string,
	commit string,
	baseURL string,
	experimental bool,
	rooms *room.Service,
	mediaManager *media.Manager,
	plexAuth *plex.AuthManager,
	jellyfinAuth *jellyfin.AuthManager,
	healthProber healthProber,
	logger *slog.Logger,
) *API {
	return &API{
		Version:      version,
		Commit:       commit,
		BaseURL:      baseURL,
		Experimental: experimental,
		Rooms:        rooms,
		Media:        mediaManager,
		Plex:         plexAuth,
		Jellyfin:     jellyfinAuth,
		healthProber: healthProber,
		Logger:       logger,
	}
}

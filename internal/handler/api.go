package handler

import (
	"log/slog"

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
	// Auth manages Plex authentication and server access.
	Auth *plex.AuthManager
}

// New creates an API with the supplied application services.
func New(
	version, commit, baseURL string,
	experimental bool,
	rooms *room.Service,
	auth *plex.AuthManager,
	logger *slog.Logger,
) *API {
	return &API{
		Version:      version,
		Commit:       commit,
		BaseURL:      baseURL,
		Experimental: experimental,
		Rooms:        rooms,
		Auth:         auth,
		Logger:       logger,
	}
}

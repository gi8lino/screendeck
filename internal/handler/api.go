package handler

import (
	"log/slog"

	"github.com/gi8lino/screendeck/internal/plex"
	"github.com/gi8lino/screendeck/internal/room"
)

// API bundles shared handler dependencies and runtime configuration.
type API struct {
	Version      string
	Commit       string
	BaseURL      string
	Experimental bool
	Logger       *slog.Logger
	Rooms        *room.Service
	Auth         *plex.AuthManager
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

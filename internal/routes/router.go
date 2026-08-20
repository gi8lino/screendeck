package routes

import (
	"io/fs"
	"log/slog"
	"net/http"

	"github.com/gi8lino/screendeck/internal/handler"
	"github.com/gi8lino/screendeck/internal/middleware"
)

// NewRouter wires the ScreenDeck HTTP routes and middleware.
func NewRouter(appFS fs.FS, api *handler.API, logger *slog.Logger, debug bool) (http.Handler, error) {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", api.Health())
	mux.HandleFunc("GET /api/config", api.Config())
	mux.HandleFunc("POST /api/plex/auth", api.StartPlexAuth())
	mux.HandleFunc("GET /api/plex/auth/status", api.PlexAuthStatus())
	mux.HandleFunc("POST /api/plex/server", api.SelectPlexServer())
	mux.HandleFunc("GET /api/libraries", api.Libraries())
	mux.HandleFunc("POST /api/catalog/options", api.CatalogOptions())
	mux.HandleFunc("POST /api/rooms", api.CreateRoom())
	mux.HandleFunc("POST /api/rooms/join", api.JoinRoom())
	mux.HandleFunc("GET /api/rooms/{code}/genres", api.RoomGenres())
	mux.HandleFunc("GET /api/rooms/{code}", api.RoomState())
	mux.HandleFunc("DELETE /api/rooms/{code}", api.LeaveRoom())
	mux.HandleFunc("POST /api/rooms/{code}/votes", api.Vote())
	mux.HandleFunc("POST /api/rooms/{code}/more-titles", api.AddMoreTitles())
	mux.HandleFunc("POST /api/rooms/{code}/round-ready", api.NextRoundReady())
	mux.HandleFunc("GET /api/rooms/{code}/events", api.Events())
	mux.HandleFunc("GET /api/posters/{itemID}", api.Poster())
	mux.Handle("GET /", http.FileServer(http.FS(appFS)))

	var routed http.Handler = mux
	routed = middleware.Chain(routed, middleware.SecurityHeaders, middleware.RecoverPanics(logger))
	if debug {
		return middleware.Chain(routed, middleware.RequestLogging(logger), middleware.RequestID()), nil
	}
	return middleware.Chain(routed, middleware.RequestID()), nil
}

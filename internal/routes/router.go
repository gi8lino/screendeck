package routes

import (
	"io/fs"
	"log/slog"
	"net/http"

	"github.com/gi8lino/screendeck/internal/handler"
	"github.com/gi8lino/screendeck/internal/middleware"
)

// NewRouter wires the ScreenDeck HTTP routes and middleware.
func NewRouter(appFS fs.FS, handlers *handler.API, logger *slog.Logger, accessLog bool) (http.Handler, error) {
	root := http.NewServeMux()
	root.HandleFunc("GET /healthz", handlers.Health())

	api := http.NewServeMux()
	api.HandleFunc("GET /config", handlers.Config())

	api.HandleFunc("POST /plex/auth", handlers.StartPlexAuth())
	api.HandleFunc("GET /plex/auth/status", handlers.PlexAuthStatus())
	api.HandleFunc("POST /plex/server", handlers.SelectPlexServer())
	api.HandleFunc("POST /jellyfin/connect", handlers.ConnectJellyfin())

	api.HandleFunc("GET /libraries", handlers.Libraries())
	api.HandleFunc("POST /catalog/options", handlers.CatalogOptions())
	api.HandleFunc("GET /posters/{itemID}", handlers.Poster())

	api.HandleFunc("GET /me/rooms", handlers.MyRooms())
	api.HandleFunc("POST /me/rooms/{code}/session", handlers.ResumeRoom())

	api.HandleFunc("POST /rooms", handlers.CreateRoom())
	api.HandleFunc("POST /rooms/join", handlers.JoinRoom())
	api.HandleFunc("GET /rooms/{code}", handlers.RoomState())
	api.HandleFunc("GET /rooms/{code}/genres", handlers.RoomGenres())
	api.HandleFunc("DELETE /rooms/{code}", handlers.LeaveRoom())
	api.HandleFunc("DELETE /rooms/{code}/participants/{participantID}", handlers.RemoveParticipant())
	api.HandleFunc("POST /rooms/{code}/votes", handlers.Vote())
	api.HandleFunc("POST /rooms/{code}/more-titles", handlers.AddMoreTitles())
	api.HandleFunc("POST /rooms/{code}/round-ready", handlers.NextRoundReady())
	api.HandleFunc("GET /rooms/{code}/events", handlers.Events())

	root.Handle("/api/", http.StripPrefix("/api", api))
	root.Handle("/", handler.Frontend(appFS))

	var routed http.Handler = root
	routed = middleware.Chain(routed, middleware.SecurityHeaders, middleware.RecoverPanics(logger))
	if accessLog {
		routed = middleware.Chain(routed, middleware.AccessLog(logger))
	}
	return middleware.Chain(routed, middleware.RequestID()), nil
}

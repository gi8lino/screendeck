package routes

import (
	"io/fs"
	"log/slog"
	"net/http"

	"github.com/gi8lino/screendeck/internal/handler"
	"github.com/gi8lino/screendeck/internal/jellyfin"
	"github.com/gi8lino/screendeck/internal/media"
	"github.com/gi8lino/screendeck/internal/middleware"
	"github.com/gi8lino/screendeck/internal/plex"
	"github.com/gi8lino/screendeck/internal/room"
)

// NewRouter wires the ScreenDeck HTTP routes and middleware.
func NewRouter(
	appFS fs.FS,
	version string,
	commit string,
	baseURL string,
	experimental bool,
	networkWarning bool,
	rooms *room.Service,
	mediaManager *media.Manager,
	plexAuth *plex.AuthManager,
	jellyfinAuth *jellyfin.AuthManager,
	healthProber handler.HealthProber,
	logger *slog.Logger,
	accessLog bool,
) http.Handler {
	root := http.NewServeMux()
	root.HandleFunc("GET /healthz", handler.Health(healthProber, logger))

	api := http.NewServeMux()
	api.HandleFunc("GET /config", handler.Config(
		version,
		commit,
		baseURL,
		experimental,
		networkWarning,
		mediaManager,
		logger,
	))

	api.HandleFunc("POST /plex/auth", handler.StartPlexAuth(mediaManager, plexAuth, logger))
	api.HandleFunc("GET /plex/auth/status", handler.PlexAuthStatus(plexAuth, logger))
	api.HandleFunc("POST /plex/server", handler.SelectPlexServer(mediaManager, plexAuth, logger))
	api.HandleFunc("POST /jellyfin/connect", handler.ConnectJellyfin(mediaManager, jellyfinAuth, logger))

	api.HandleFunc("GET /libraries", handler.Libraries(rooms, logger))
	api.HandleFunc("POST /catalog/options", handler.CatalogOptions(rooms, logger))
	api.HandleFunc("GET /posters/{itemID}", handler.Poster(rooms, logger))

	api.HandleFunc("GET /me/rooms", handler.MyRooms(rooms, logger))
	api.HandleFunc("POST /me/rooms/{code}/session", handler.ResumeRoom(rooms, logger))

	api.HandleFunc("POST /rooms", handler.CreateRoom(rooms, logger))
	api.HandleFunc("POST /rooms/join", handler.JoinRoom(rooms, logger))
	api.HandleFunc("GET /rooms/{code}", handler.RoomState(rooms, logger))
	api.HandleFunc("GET /rooms/{code}/genres", handler.RoomGenres(rooms, logger))
	api.HandleFunc("DELETE /rooms/{code}", handler.LeaveRoom(rooms, logger))
	api.HandleFunc("PATCH /rooms/{code}/settings", handler.UpdateRoomSettings(rooms, logger))
	api.HandleFunc("DELETE /rooms/{code}/participants/{participantID}", handler.RemoveParticipant(rooms, logger))
	api.HandleFunc("POST /rooms/{code}/votes", handler.Vote(rooms, logger))
	api.HandleFunc("POST /rooms/{code}/more-titles", handler.AddMoreTitles(rooms, logger))
	api.HandleFunc("POST /rooms/{code}/round-ready", handler.NextRoundReady(rooms, logger))
	api.HandleFunc("GET /rooms/{code}/events", handler.Events(rooms, logger))

	root.Handle("/api/", http.StripPrefix("/api", api))
	root.Handle("/", handler.Frontend(appFS))

	var routed http.Handler = root
	routed = middleware.Chain(routed, middleware.SecurityHeaders, middleware.RecoverPanics(logger))
	if accessLog {
		routed = middleware.Chain(routed, middleware.AccessLog(logger))
	}
	return middleware.Chain(routed, middleware.RequestID())
}

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
	mux.HandleFunc("POST /api/rooms/{code}/rounds", api.NextRound())
	mux.HandleFunc("GET /api/rooms/{code}/events", api.Events())
	mux.HandleFunc("GET /api/posters/{movieID}", api.Poster())
	mux.Handle("GET /", http.FileServer(http.FS(appFS)))

	var routed http.Handler = mux
	routed = middleware.Chain(routed, securityHeaders, recoverPanics(logger))
	if debug {
		return middleware.Chain(routed, middleware.RequestLogging(logger), middleware.RequestID()), nil
	}
	return middleware.Chain(routed, middleware.RequestID()), nil
}

// securityHeaders adds defensive browser headers to every response.
func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Content-Security-Policy", "default-src 'self'; img-src 'self' data:; style-src 'self'; script-src 'self'; connect-src 'self'")
		next.ServeHTTP(w, r)
	})
}

// recoverPanics converts handler panics into internal server errors.
func recoverPanics(logger *slog.Logger) middleware.Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			defer func() {
				if value := recover(); value != nil {
					logger.Error("request panic", "event", "request_panic", "value", value)
					http.Error(w, "internal server error", http.StatusInternalServerError)
				}
			}()
			next.ServeHTTP(w, r)
		})
	}
}

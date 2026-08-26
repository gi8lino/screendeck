package handler

import (
	"log/slog"
	"net/http"

	"github.com/gi8lino/screendeck/internal/media"
)

// Config returns the public application configuration handler.
func Config(
	version string,
	commit string,
	baseURL string,
	experimental bool,
	mediaManager *media.Manager,
	logger *slog.Logger,
) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		status := mediaManager.Status()
		respond(logger, w, http.StatusOK, map[string]any{
			"version":         version,
			"commit":          commit,
			"baseUrl":         baseURL,
			"experimental":    experimental,
			"mediaConfigured": status.Configured,
			"mediaProvider":   status.Provider,
			"mediaServerName": status.ServerName,
		})
	}
}

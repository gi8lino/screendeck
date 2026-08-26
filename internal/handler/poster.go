package handler

import (
	"io"
	"log/slog"
	"net/http"
	"strings"

	"github.com/gi8lino/screendeck/internal/room"
)

// Poster returns the proxied media poster handler.
func Poster(rooms *room.Service, logger *slog.Logger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		response, err := rooms.Poster(r.Context(), r.PathValue("itemID"))
		if err != nil {
			fail(logger, r, w, err)
			return
		}
		defer response.Body.Close() // nolint:errcheck
		contentType := response.Header.Get("Content-Type")
		if !strings.HasPrefix(contentType, "image/") {
			contentType = "image/jpeg"
		}
		w.Header().Set("Content-Type", contentType)
		w.Header().Set("Cache-Control", "private, max-age=86400")
		if _, err := io.Copy(w, response.Body); err != nil {
			logger.Debug("poster stream ended",
				"event", "poster_stream_ended",
				"error", err,
			)
		}
	}
}

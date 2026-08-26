package handler

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"strings"

	"github.com/gi8lino/screendeck/internal/room"
)

// posterReader retrieves a proxied poster response for a media item.
type posterReader interface {
	Poster(context.Context, string) (*room.PosterResponse, error)
}

// Poster returns the proxied media poster handler.
func Poster(rooms posterReader, logger *slog.Logger) http.HandlerFunc {
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

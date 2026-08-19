package handler

import (
	"io"
	"net/http"
	"strings"
)

// Poster returns the proxied media poster handler.
func (a *API) Poster() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		response, err := a.Rooms.Poster(r.Context(), r.PathValue("movieID"))
		if err != nil {
			a.fail(r, w, err)
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
			a.Logger.Debug("poster stream ended",
				"event", "poster_stream_ended",
				"error", err,
			)
		}
	}
}

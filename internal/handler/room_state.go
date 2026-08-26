package handler

import (
	"log/slog"
	"net/http"
)

// RoomGenres returns the personal genre choices available in a room.
func RoomGenres(rooms roomGenreReader, logger *slog.Logger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		genres, err := rooms.Genres(r.Context(), r.PathValue("code"))
		if err != nil {
			fail(logger, r, w, err)
			return
		}
		respond(logger, w, http.StatusOK, map[string][]string{"genres": genres})
	}
}

// RoomState returns the current room state handler.
func RoomState(rooms roomStateReader, logger *slog.Logger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		state, err := rooms.State(r.Context(), r.PathValue("code"), participantToken(r))
		if err != nil {
			fail(logger, r, w, err)
			return
		}
		respond(logger, w, http.StatusOK, state)
	}
}

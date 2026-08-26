package handler

import (
	"log/slog"
	"net/http"
)

// LeaveRoom returns the room departure handler.
func LeaveRoom(rooms roomLeaver, logger *slog.Logger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if err := rooms.Leave(r.Context(), r.PathValue("code"), participantToken(r)); err != nil {
			fail(logger, r, w, err)
			return
		}
		respond(logger, w, http.StatusOK, map[string]string{"status": "left"})
	}
}

// RemoveParticipant returns the handler that lets a room host remove another participant.
func RemoveParticipant(rooms participantRemover, logger *slog.Logger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if err := rooms.RemoveParticipant(
			r.Context(),
			r.PathValue("code"),
			participantToken(r),
			r.PathValue("participantID"),
		); err != nil {
			fail(logger, r, w, err)
			return
		}
		respond(logger, w, http.StatusOK, map[string]string{"status": "removed"})
	}
}

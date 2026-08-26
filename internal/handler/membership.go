package handler

import (
	"log/slog"
	"net/http"

	"github.com/gi8lino/screendeck/internal/room"
)

// MyRooms returns active rooms associated with the current browser identity.
func MyRooms(rooms *room.Service, logger *slog.Logger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		identityToken, err := ensureBrowserIdentity(w, r)
		if err != nil {
			fail(logger, r, w, err)
			return
		}
		memberships, err := rooms.RoomsForIdentity(r.Context(), identityToken)
		if err != nil {
			fail(logger, r, w, err)
			return
		}
		respond(logger, w, http.StatusOK, memberships)
	}
}

// ResumeRoom returns the saved participant session for an active browser room membership.
func ResumeRoom(rooms *room.Service, logger *slog.Logger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		identityToken, err := ensureBrowserIdentity(w, r)
		if err != nil {
			fail(logger, r, w, err)
			return
		}
		session, err := rooms.ResumeIdentity(r.Context(), identityToken, r.PathValue("code"))
		if err != nil {
			fail(logger, r, w, err)
			return
		}
		respond(logger, w, http.StatusOK, session)
	}
}

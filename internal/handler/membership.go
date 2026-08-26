package handler

import (
	"context"
	"log/slog"
	"net/http"

	"github.com/gi8lino/screendeck/internal/room"
)

// membershipReader lists rooms associated with a browser identity.
type membershipReader interface {
	RoomsForIdentity(context.Context, string) ([]room.Membership, error)
}

// membershipResumer restores a participant session for a browser identity.
type membershipResumer interface {
	ResumeIdentity(context.Context, string, string) (room.Session, error)
}

// MyRooms returns active rooms associated with the current browser identity.
func MyRooms(rooms membershipReader, logger *slog.Logger) http.HandlerFunc {
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
func ResumeRoom(rooms membershipResumer, logger *slog.Logger) http.HandlerFunc {
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

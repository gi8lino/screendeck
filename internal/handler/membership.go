package handler

import "net/http"

// MyRooms returns active rooms associated with the current browser identity.
func (a *API) MyRooms() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		identityToken, err := ensureBrowserIdentity(w, r)
		if err != nil {
			a.fail(r, w, err)
			return
		}
		rooms, err := a.Rooms.RoomsForIdentity(r.Context(), identityToken)
		if err != nil {
			a.fail(r, w, err)
			return
		}
		a.respond(w, http.StatusOK, rooms)
	}
}

// ResumeRoom returns the saved participant session for an active browser room membership.
func (a *API) ResumeRoom() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		identityToken, err := ensureBrowserIdentity(w, r)
		if err != nil {
			a.fail(r, w, err)
			return
		}
		session, err := a.Rooms.ResumeIdentity(r.Context(), identityToken, r.PathValue("code"))
		if err != nil {
			a.fail(r, w, err)
			return
		}
		a.respond(w, http.StatusOK, session)
	}
}

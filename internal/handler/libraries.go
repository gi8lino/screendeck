package handler

import (
	"net/http"

	"github.com/gi8lino/screendeck/internal/room"
)

// Libraries returns the media library listing handler.
func (a *API) Libraries() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		libraries, err := a.Rooms.Libraries(r.Context())
		if err != nil {
			a.fail(r, w, err)
			return
		}
		room.SortLibraries(libraries)
		a.respond(w, http.StatusOK, libraries)
	}
}

// CatalogOptions returns the catalog filter options handler.
func (a *API) CatalogOptions() http.HandlerFunc {
	// request describes the JSON payload accepted by this handler.
	type request struct {
		// LibraryKeys identifies the media libraries included in the room.
		LibraryKeys []string `json:"libraryKeys"`
	}
	return func(w http.ResponseWriter, r *http.Request) {
		var input request
		if err := decode(r, &input); err != nil {
			a.fail(r, w, err)
			return
		}
		options, err := a.Rooms.Options(r.Context(), input.LibraryKeys)
		if err != nil {
			a.fail(r, w, err)
			return
		}
		a.respond(w, http.StatusOK, options)
	}
}

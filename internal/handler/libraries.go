package handler

import (
	"context"
	"log/slog"
	"net/http"

	"github.com/gi8lino/screendeck/internal/media"
	"github.com/gi8lino/screendeck/internal/room"
)

// libraryReader lists media libraries available for room creation.
type libraryReader interface {
	Libraries(context.Context) ([]media.Library, error)
}

// catalogOptionsReader derives filter options for selected libraries.
type catalogOptionsReader interface {
	Options(context.Context, []string) (room.CatalogOptions, error)
}

// Libraries returns the media library listing handler.
func Libraries(rooms libraryReader, logger *slog.Logger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		libraries, err := rooms.Libraries(r.Context())
		if err != nil {
			fail(logger, r, w, err)
			return
		}
		room.SortLibraries(libraries)
		respond(logger, w, http.StatusOK, libraries)
	}
}

// CatalogOptions returns the catalog filter options handler.
func CatalogOptions(rooms catalogOptionsReader, logger *slog.Logger) http.HandlerFunc {
	// request describes the JSON payload accepted by this handler.
	type request struct {
		// LibraryKeys identifies the media libraries included in the room.
		LibraryKeys []string `json:"libraryKeys"`
	}
	return func(w http.ResponseWriter, r *http.Request) {
		input, err := decode[request](r)
		if err != nil {
			fail(logger, r, w, err)
			return
		}
		options, err := rooms.Options(r.Context(), input.LibraryKeys)
		if err != nil {
			fail(logger, r, w, err)
			return
		}
		respond(logger, w, http.StatusOK, options)
	}
}

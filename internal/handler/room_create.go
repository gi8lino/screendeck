package handler

import (
	"context"
	"log/slog"
	"net/http"
	"strings"

	"github.com/gi8lino/screendeck/internal/room"
)

// createRoomRequest describes the JSON payload accepted when creating a room.
type createRoomRequest struct {
	Name             string                `json:"name"`
	LibraryKeys      []string              `json:"libraryKeys"`
	Filters          room.Filters          `json:"filters"`
	Genres           []string              `json:"genres"`
	GenreMode        room.GenreMode        `json:"genreMode"`
	RoundSize        int                   `json:"roundSize"`
	LifetimeHours    int                   `json:"lifetimeHours"`
	SamplingStrategy room.SamplingStrategy `json:"samplingStrategy"`
}

// Valid returns every field-level room creation problem.
func (input createRoomRequest) Valid(context.Context) map[string]string {
	problems := make(map[string]string)

	if strings.TrimSpace(input.Name) == "" {
		problems["name"] = "Enter your name."
	}

	if len(input.LibraryKeys) == 0 {
		problems["libraryKeys"] = "Select at least one library."
	}

	if input.Filters.YearFrom < 0 {
		problems["filters.yearFrom"] = "The year must be zero or later."
	}

	if input.Filters.YearTo < 0 {
		problems["filters.yearTo"] = "The year must be zero or later."
	}

	if room.ReversedYearRange(input.Filters) {
		problems["filters.yearTo"] = "The final year must not be earlier than the starting year."
	}

	if input.Filters.MaxDurationMinutes < 0 {
		problems["filters.maxDurationMinutes"] = "The duration must be zero or greater."
	}

	if !room.ValidRoundSize(input.RoundSize) {
		problems["roundSize"] = "Choose a first-round size between 0 and 50,000."
	}

	if !room.ValidRoomLifetimeHours(input.LifetimeHours) {
		problems["lifetimeHours"] = "Choose a room lifetime between 6 hours and 7 days."
	}

	if !room.ValidGenreMode(input.GenreMode) {
		problems["genreMode"] = "Choose whether genres should match any or all selections."
	}

	if input.SamplingStrategy != "" && !room.ValidSamplingStrategy(input.SamplingStrategy) {
		problems["samplingStrategy"] = "Choose a valid first-round selection strategy."
	}

	return problems
}

// CreateRoom returns the room creation handler.
func CreateRoom(rooms roomCreator, logger *slog.Logger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		input, err := decodeValid[createRoomRequest](r)
		if err != nil {
			fail(logger, r, w, err)
			return
		}
		identityToken, err := ensureBrowserIdentity(w, r)
		if err != nil {
			fail(logger, r, w, err)
			return
		}
		session, err := rooms.CreateForIdentity(
			r.Context(),
			room.CreateOptions{
				Name:          input.Name,
				LibraryKeys:   input.LibraryKeys,
				Filters:       input.Filters,
				Genres:        input.Genres,
				GenreMode:     input.GenreMode,
				Sampling:      input.SamplingStrategy,
				RoundSize:     input.RoundSize,
				LifetimeHours: input.LifetimeHours,
				IdentityToken: identityToken,
			},
		)
		if err != nil {
			fail(logger, r, w, err)
			return
		}
		respond(logger, w, http.StatusCreated, session)
	}
}

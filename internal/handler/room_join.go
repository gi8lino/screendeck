package handler

import (
	"context"
	"log/slog"
	"net/http"
	"strings"

	"github.com/gi8lino/screendeck/internal/room"
)

// joinRoomRequest describes the JSON payload accepted when joining a room.
type joinRoomRequest struct {
	Code      string         `json:"code"`
	Name      string         `json:"name"`
	Genres    []string       `json:"genres"`
	GenreMode room.GenreMode `json:"genreMode"`
}

// Valid returns every field-level room joining problem.
func (input joinRoomRequest) Valid(context.Context) map[string]string {
	problems := make(map[string]string)
	if !validRoomCode(input.Code) {
		problems["code"] = "Enter a valid six-character room code."
	}
	if strings.TrimSpace(input.Name) == "" {
		problems["name"] = "Enter your name."
	}
	if !room.ValidGenreMode(input.GenreMode) {
		problems["genreMode"] = "Choose whether genres should match any or all selections."
	}
	return problems
}

// validRoomCode reports whether code has the expected length and alphabet.
func validRoomCode(code string) bool {
	code = strings.ToUpper(strings.TrimSpace(code))
	if len(code) != 6 {
		return false
	}
	const alphabet = "ABCDEFGHJKLMNPQRSTUVWXYZ23456789"
	for _, character := range code {
		if !strings.ContainsRune(alphabet, character) {
			return false
		}
	}
	return true
}

// JoinRoom returns the room joining handler.
func JoinRoom(rooms roomJoiner, logger *slog.Logger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		input, err := decodeValid[joinRoomRequest](r)
		if err != nil {
			fail(logger, r, w, err)
			return
		}
		identityToken, err := ensureBrowserIdentity(w, r)
		if err != nil {
			fail(logger, r, w, err)
			return
		}
		session, err := rooms.JoinForIdentity(
			r.Context(),
			input.Code,
			input.Name,
			input.Genres,
			input.GenreMode,
			identityToken,
		)
		if err != nil {
			fail(logger, r, w, err)
			return
		}
		respond(logger, w, http.StatusCreated, session)
	}
}

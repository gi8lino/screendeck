package handler

import (
	"errors"
	"log/slog"
	"net/http"
)

// Vote returns the media voting handler.
func Vote(rooms roomVoter, logger *slog.Logger) http.HandlerFunc {
	// request describes the JSON payload accepted by this handler.
	type request struct {
		// ItemID identifies the canonical media item being voted on.
		ItemID string `json:"itemId"`
		// Liked records whether the participant accepted the item.
		Liked bool `json:"liked"`
	}
	return func(w http.ResponseWriter, r *http.Request) {
		input, err := decode[request](r)
		if err != nil {
			fail(logger, r, w, err)
			return
		}
		if input.ItemID == "" {
			fail(logger, r, w, errors.New("itemId is required"))
			return
		}
		matched, err := rooms.Vote(r.Context(), r.PathValue("code"), participantToken(r), input.ItemID, input.Liked)
		if err != nil {
			fail(logger, r, w, err)
			return
		}
		respond(logger, w, http.StatusOK, map[string]bool{"matched": matched})
	}
}

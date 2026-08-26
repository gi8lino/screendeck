package handler

import (
	"context"
	"log/slog"
	"net/http"
)

// roomSettingsRequest describes host-controlled room admission settings.
type roomSettingsRequest struct {
	Locked *bool `json:"locked"`
}

// Valid validates a room settings update.
func (input roomSettingsRequest) Valid(context.Context) map[string]string {
	if input.Locked == nil {
		return map[string]string{"locked": "Choose whether the room accepts new participants."}
	}
	return nil
}

// UpdateRoomSettings returns the handler that changes host-controlled room settings.
func UpdateRoomSettings(rooms roomSettingsUpdater, logger *slog.Logger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		input, err := decodeValid[roomSettingsRequest](r)
		if err != nil {
			fail(logger, r, w, err)
			return
		}
		if err := rooms.SetRoomLocked(
			r.Context(),
			r.PathValue("code"),
			participantToken(r),
			*input.Locked,
		); err != nil {
			fail(logger, r, w, err)
			return
		}
		respond(logger, w, http.StatusOK, map[string]bool{"locked": *input.Locked})
	}
}

package handler

import (
	"log/slog"
	"net/http"
)

// AddMoreTitles returns the handler that expands the first round from its unused pool.
func AddMoreTitles(rooms roomExpander, logger *slog.Logger) http.HandlerFunc {
	// request describes the JSON payload accepted by this handler.
	type request struct {
		// Count is the requested number of additional titles.
		Count int `json:"count"`
	}
	return func(w http.ResponseWriter, r *http.Request) {
		input, err := decode[request](r)
		if err != nil {
			fail(logger, r, w, err)
			return
		}
		result, err := rooms.AddMoreTitles(r.Context(), r.PathValue("code"), participantToken(r), input.Count)
		if err != nil {
			fail(logger, r, w, err)
			return
		}
		respond(logger, w, http.StatusOK, result)
	}
}

// NextRoundReady returns the handler that records agreement to narrow the deck to current matches.
func NextRoundReady(rooms roundReadinessUpdater, logger *slog.Logger) http.HandlerFunc {
	// request describes the JSON payload accepted by this handler.
	type request struct {
		// Round identifies the room round the readiness update applies to.
		Round int `json:"round"`
		// Ready records whether the participant is ready to advance.
		Ready bool `json:"ready"`
	}
	return func(w http.ResponseWriter, r *http.Request) {
		input, err := decode[request](r)
		if err != nil {
			fail(logger, r, w, err)
			return
		}
		result, err := rooms.SetNextRoundReady(
			r.Context(),
			r.PathValue("code"),
			participantToken(r),
			input.Round,
			input.Ready,
		)
		if err != nil {
			fail(logger, r, w, err)
			return
		}
		respond(logger, w, http.StatusOK, result)
	}
}

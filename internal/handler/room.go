package handler

import (
	"errors"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/gi8lino/screendeck/internal/room"
)

// CreateRoom returns the room creation handler.
func (a *API) CreateRoom() http.HandlerFunc {
	// request describes the JSON payload accepted by this handler.
	type request struct {
		// Name is the display name.
		Name string `json:"name"`
		// LibraryKeys identifies the Plex libraries included in the room.
		LibraryKeys []string `json:"libraryKeys"`
		// Filters contains room-wide catalog filters.
		Filters room.Filters `json:"filters"`
		// Genres contains selected genre names.
		Genres []string `json:"genres"`
		// GenreMode controls whether selected genres match any or all.
		GenreMode room.GenreMode `json:"genreMode"`
		// RoundSize limits the number of titles initially activated.
		RoundSize int `json:"roundSize"`
		// SamplingStrategy controls how the initial title pool is ordered.
		SamplingStrategy room.SamplingStrategy `json:"samplingStrategy"`
	}
	return func(w http.ResponseWriter, r *http.Request) {
		var input request
		if err := decode(r, &input); err != nil {
			a.fail(r, w, err)
			return
		}
		session, err := a.Rooms.Create(
			r.Context(),
			input.Name,
			input.LibraryKeys,
			input.Filters,
			input.Genres,
			input.GenreMode,
			input.SamplingStrategy,
			input.RoundSize,
		)
		if err != nil {
			a.fail(r, w, err)
			return
		}
		a.respond(w, http.StatusCreated, session)
	}
}

// JoinRoom returns the room joining handler.
func (a *API) JoinRoom() http.HandlerFunc {
	// request describes the JSON payload accepted by this handler.
	type request struct {
		// Code is the six-character room identifier.
		Code string `json:"code"`
		// Name is the display name.
		Name string `json:"name"`
		// Genres contains selected genre names.
		Genres []string `json:"genres"`
		// GenreMode controls whether selected genres match any or all.
		GenreMode room.GenreMode `json:"genreMode"`
	}
	return func(w http.ResponseWriter, r *http.Request) {
		var input request
		if err := decode(r, &input); err != nil {
			a.fail(r, w, err)
			return
		}
		session, err := a.Rooms.Join(r.Context(), input.Code, input.Name, input.Genres, input.GenreMode)
		if err != nil {
			a.fail(r, w, err)
			return
		}
		a.respond(w, http.StatusCreated, session)
	}
}

// RoomGenres returns the personal genre choices available in a room.
func (a *API) RoomGenres() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		genres, err := a.Rooms.Genres(r.Context(), r.PathValue("code"))
		if err != nil {
			a.fail(r, w, err)
			return
		}
		a.respond(w, http.StatusOK, map[string][]string{"genres": genres})
	}
}

// RoomState returns the current room state handler.
func (a *API) RoomState() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		state, err := a.Rooms.State(r.Context(), r.PathValue("code"), participantToken(r))
		if err != nil {
			a.fail(r, w, err)
			return
		}
		a.respond(w, http.StatusOK, state)
	}
}

// Vote returns the media voting handler.
func (a *API) Vote() http.HandlerFunc {
	// request describes the JSON payload accepted by this handler.
	type request struct {
		// ItemID identifies the canonical media item being voted on.
		ItemID string `json:"itemId"`
		// LegacyID accepts the former movieId field for cached clients during migration.
		LegacyID string `json:"movieId"`
		// Liked records whether the participant accepted the item.
		Liked bool `json:"liked"`
	}
	return func(w http.ResponseWriter, r *http.Request) {
		var input request
		if err := decode(r, &input); err != nil {
			a.fail(r, w, err)
			return
		}
		if input.ItemID == "" {
			input.ItemID = input.LegacyID
		}
		if input.ItemID == "" {
			a.fail(r, w, errors.New("itemId is required"))
			return
		}
		matched, err := a.Rooms.Vote(r.Context(), r.PathValue("code"), participantToken(r), input.ItemID, input.Liked)
		if err != nil {
			a.fail(r, w, err)
			return
		}
		a.respond(w, http.StatusOK, map[string]bool{"matched": matched})
	}
}

// AddMoreTitles returns the handler that expands the first round from its unused pool.
func (a *API) AddMoreTitles() http.HandlerFunc {
	// request describes the JSON payload accepted by this handler.
	type request struct {
		// Count is the requested number of additional titles.
		Count int `json:"count"`
	}
	return func(w http.ResponseWriter, r *http.Request) {
		var input request
		if err := decode(r, &input); err != nil {
			a.fail(r, w, err)
			return
		}
		result, err := a.Rooms.AddMoreTitles(r.Context(), r.PathValue("code"), participantToken(r), input.Count)
		if err != nil {
			a.fail(r, w, err)
			return
		}
		a.respond(w, http.StatusOK, result)
	}
}

// NextRoundReady returns the handler that records agreement to narrow the deck to current matches.
func (a *API) NextRoundReady() http.HandlerFunc {
	// request describes the JSON payload accepted by this handler.
	type request struct {
		// Round identifies the room round the readiness update applies to.
		Round int `json:"round"`
		// Ready records whether the participant is ready to advance.
		Ready bool `json:"ready"`
	}
	return func(w http.ResponseWriter, r *http.Request) {
		var input request
		if err := decode(r, &input); err != nil {
			a.fail(r, w, err)
			return
		}
		result, err := a.Rooms.SetNextRoundReady(r.Context(), r.PathValue("code"), participantToken(r), input.Round, input.Ready)
		if err != nil {
			a.fail(r, w, err)
			return
		}
		a.respond(w, http.StatusOK, result)
	}
}

// LeaveRoom returns the room departure handler.
func (a *API) LeaveRoom() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if err := a.Rooms.Leave(r.Context(), r.PathValue("code"), participantToken(r)); err != nil {
			a.fail(r, w, err)
			return
		}
		a.respond(w, http.StatusOK, map[string]string{"status": "left"})
	}
}

// Events returns the room server-sent events handler.
func (a *API) Events() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		code, token := strings.ToUpper(r.PathValue("code")), r.URL.Query().Get("token")
		if _, err := a.Rooms.State(r.Context(), code, token); err != nil {
			a.fail(r, w, err)
			return
		}
		flusher, ok := w.(http.Flusher)
		if !ok {
			http.Error(w, "streaming unsupported", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("X-Accel-Buffering", "no")
		events, unsubscribe := a.Rooms.Subscribe(code)
		defer unsubscribe()
		if _, err := io.WriteString(w, "event: update\ndata: connected\n\n"); err != nil {
			return
		}
		flusher.Flush()
		heartbeat := time.NewTicker(20 * time.Second)
		defer heartbeat.Stop()
		for {
			select {
			case <-r.Context().Done():
				return
			case <-events:
				if _, err := io.WriteString(w, "event: update\ndata: changed\n\n"); err != nil {
					return
				}
				flusher.Flush()
			case <-heartbeat.C:
				if _, err := io.WriteString(w, ": keepalive\n\n"); err != nil {
					return
				}
				flusher.Flush()
			}
		}
	}
}

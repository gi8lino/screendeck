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
	type request struct {
		Name        string       `json:"name"`
		LibraryKeys []string     `json:"libraryKeys"`
		Filters     room.Filters `json:"filters"`
	}
	return func(w http.ResponseWriter, r *http.Request) {
		var input request
		if err := decode(r, &input); err != nil {
			a.fail(r, w, err)
			return
		}
		session, err := a.Rooms.Create(r.Context(), input.Name, input.LibraryKeys, input.Filters)
		if err != nil {
			a.fail(r, w, err)
			return
		}
		a.respond(w, http.StatusCreated, session)
	}
}

// JoinRoom returns the room joining handler.
func (a *API) JoinRoom() http.HandlerFunc {
	type request struct{ Code, Name string }
	return func(w http.ResponseWriter, r *http.Request) {
		var input request
		if err := decode(r, &input); err != nil {
			a.fail(r, w, err)
			return
		}
		session, err := a.Rooms.Join(r.Context(), input.Code, input.Name)
		if err != nil {
			a.fail(r, w, err)
			return
		}
		a.respond(w, http.StatusCreated, session)
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
	type request struct {
		MovieID string `json:"movieId"`
		Liked   bool   `json:"liked"`
	}
	return func(w http.ResponseWriter, r *http.Request) {
		var input request
		if err := decode(r, &input); err != nil {
			a.fail(r, w, err)
			return
		}
		if input.MovieID == "" {
			a.fail(r, w, errors.New("movieId is required"))
			return
		}
		matched, err := a.Rooms.Vote(r.Context(), r.PathValue("code"), participantToken(r), input.MovieID, input.Liked)
		if err != nil {
			a.fail(r, w, err)
			return
		}
		a.respond(w, http.StatusOK, map[string]bool{"matched": matched})
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
		_, _ = io.WriteString(w, "event: update\ndata: connected\n\n")
		flusher.Flush()
		heartbeat := time.NewTicker(20 * time.Second)
		defer heartbeat.Stop()
		for {
			select {
			case <-r.Context().Done():
				return
			case <-events:
				_, _ = io.WriteString(w, "event: update\ndata: changed\n\n")
				flusher.Flush()
			case <-heartbeat.C:
				_, _ = io.WriteString(w, ": keepalive\n\n")
				flusher.Flush()
			}
		}
	}
}

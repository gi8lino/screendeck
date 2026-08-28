package handler

import (
	"errors"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"
)

// Events returns the room server-sent events handler.
func Events(rooms roomEventSource, logger *slog.Logger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		code, token := strings.ToUpper(r.PathValue("code")), participantToken(r)
		if _, err := rooms.State(r.Context(), code, token); err != nil {
			fail(logger, r, w, err)
			return
		}
		flusher, ok := w.(http.Flusher)
		if !ok {
			http.Error(w, "streaming unsupported", http.StatusInternalServerError)
			return
		}
		if err := disableWriteDeadline(w); err != nil {
			fail(logger, r, w, err)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("X-Accel-Buffering", "no")
		events, unsubscribe := rooms.Subscribe(code)
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

// disableWriteDeadline allows a server-sent event response to remain open indefinitely.
func disableWriteDeadline(w http.ResponseWriter) error {
	err := http.NewResponseController(w).SetWriteDeadline(time.Time{})
	if errors.Is(err, http.ErrNotSupported) {
		return nil
	}
	return err
}

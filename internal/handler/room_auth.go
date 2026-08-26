package handler

import (
	"net/http"
	"strings"
)

// participantToken extracts a participant token from a request.
func participantToken(r *http.Request) string {
	return strings.TrimSpace(r.Header.Get("X-Participant-Token"))
}

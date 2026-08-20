package handler

import (
	"net/http"

	"github.com/gi8lino/screendeck/internal/plex"
)

// plexAuthRequest describes a request to start Plex authorization.
type plexAuthRequest struct {
	// Method identifies the selected Plex authorization flow.
	Method plex.AuthMethod `json:"method"`
}

// selectPlexServerRequest identifies the Plex server selected during setup.
type selectPlexServerRequest struct {
	// ServerID identifies the Plex server selected by the user.
	ServerID string `json:"serverId"`
}

// StartPlexAuth returns the handler that begins Plex authorization.
func (a *API) StartPlexAuth() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		input := plexAuthRequest{Method: plex.AuthMethodLegacy}
		if err := decode(r, &input); err != nil {
			a.fail(r, w, err)
			return
		}
		started, err := a.Auth.Start(r.Context(), input.Method)
		if err != nil {
			a.fail(r, w, err)
			return
		}
		a.respond(w, http.StatusCreated, started)
	}
}

// PlexAuthStatus returns the handler that polls Plex authorization.
func (a *API) PlexAuthStatus() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		status, err := a.Auth.Status(r.Context(), setupToken(r))
		if err != nil {
			a.fail(r, w, err)
			return
		}
		a.respond(w, http.StatusOK, status)
	}
}

// SelectPlexServer returns the handler that selects an authorized Plex server.
func (a *API) SelectPlexServer() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var input selectPlexServerRequest
		if err := decode(r, &input); err != nil {
			a.fail(r, w, err)
			return
		}
		if err := a.Auth.SelectServer(r.Context(), setupToken(r), input.ServerID); err != nil {
			a.fail(r, w, err)
			return
		}
		a.respond(w, http.StatusOK, map[string]string{"status": "connected"})
	}
}

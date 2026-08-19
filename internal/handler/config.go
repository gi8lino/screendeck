package handler

import "net/http"

// Config returns the public application configuration handler.
func (a *API) Config() http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		configured, serverName := a.Auth.Configured()
		a.respond(w, http.StatusOK, map[string]any{
			"version":        a.Version,
			"commit":         a.Commit,
			"baseUrl":        a.BaseURL,
			"experimental":   a.Experimental,
			"plexConfigured": configured,
			"plexServerName": serverName,
		})
	}
}

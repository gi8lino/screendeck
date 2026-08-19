package handler

import "net/http"

// Health returns the service health handler.
func (a *API) Health() http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		a.respond(w, http.StatusOK, map[string]string{"status": "ok"})
	}
}

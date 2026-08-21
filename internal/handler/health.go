package handler

import (
	"context"
	"net/http"
)

// healthProber defines the database operation required by the health check.
type healthProber interface {
	Ping(context.Context) error
}

// Health returns the service health handler.
func (a *API) Health() http.HandlerFunc {
	type status struct {
		Status string `json:"status"`
	}
	return func(w http.ResponseWriter, r *http.Request) {
		if err := a.healthProber.Ping(r.Context()); err != nil {
			a.respond(w, http.StatusServiceUnavailable, status{Status: "unhealthy"})
			return
		}

		a.respond(w, http.StatusOK, status{Status: "ok"})
	}
}

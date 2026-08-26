package handler

import (
	"context"
	"log/slog"
	"net/http"
)

// HealthProber defines the database operation required by the health check.
type HealthProber interface {
	Ping(context.Context) error
}

// Health returns the service health handler.
func Health(healthProber HealthProber, logger *slog.Logger) http.HandlerFunc {
	type status struct {
		Status string `json:"status"`
	}
	return func(w http.ResponseWriter, r *http.Request) {
		if err := healthProber.Ping(r.Context()); err != nil {
			respond(logger, w, http.StatusServiceUnavailable, status{Status: "unhealthy"})
			return
		}

		respond(logger, w, http.StatusOK, status{Status: "ok"})
	}
}

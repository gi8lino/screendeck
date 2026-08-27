package handler

import (
	"cmp"
	"context"
	"log/slog"
	"net/http"
	"strings"

	"github.com/gi8lino/screendeck/internal/media"
	"github.com/gi8lino/screendeck/internal/plex"
)

// plexAuthStarter begins a Plex authentication flow.
type plexAuthStarter interface {
	Start(context.Context, plex.AuthMethod) (plex.AuthStart, error)
}

// plexAuthStatusReader polls a Plex authentication flow.
type plexAuthStatusReader interface {
	Status(context.Context, string) (plex.AuthStatus, error)
}

// plexServerSelector selects a server discovered during Plex authentication.
type plexServerSelector interface {
	SelectServer(context.Context, string, string) error
}

// setupToken extracts a Plex setup token from a request.
func setupToken(r *http.Request) string {
	return strings.TrimSpace(r.Header.Get("X-Setup-Token"))
}

// plexAuthRequest describes a request to start Plex authorization.
type plexAuthRequest struct {
	// Method identifies the selected Plex authorization flow.
	Method plex.AuthMethod `json:"method"`
}

// Valid validates a request to start Plex authorization.
func (input plexAuthRequest) Valid(context.Context) map[string]string {
	if input.Method != "" && !plex.ValidAuthMethod(input.Method) {
		return map[string]string{"method": "Choose a supported Plex authentication method."}
	}
	return nil
}

// selectPlexServerRequest identifies the Plex server selected during setup.
type selectPlexServerRequest struct {
	// ServerID identifies the Plex server selected by the user.
	ServerID string `json:"serverId"`
}

// Valid validates a Plex server selection request.
func (input selectPlexServerRequest) Valid(context.Context) map[string]string {
	if strings.TrimSpace(input.ServerID) == "" {
		return map[string]string{"serverId": "Choose a Plex server."}
	}
	return nil
}

// StartPlexAuth returns the handler that begins Plex authorization.
func StartPlexAuth(
	mediaManager providerSelector,
	plexAuth plexAuthStarter,
	logger *slog.Logger,
) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if err := mediaManager.CheckProvider(media.ProviderPlex); err != nil {
			fail(logger, r, w, err)
			return
		}
		input, err := decodeValid[plexAuthRequest](r)
		if err != nil {
			fail(logger, r, w, err)
			return
		}
		input.Method = cmp.Or(input.Method, plex.AuthMethodStandard)
		started, err := plexAuth.Start(r.Context(), input.Method)
		if err != nil {
			fail(logger, r, w, err)
			return
		}
		respond(logger, w, http.StatusCreated, started)
	}
}

// PlexAuthStatus returns the handler that polls Plex authorization.
func PlexAuthStatus(plexAuth plexAuthStatusReader, logger *slog.Logger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		status, err := plexAuth.Status(r.Context(), setupToken(r))
		if err != nil {
			fail(logger, r, w, err)
			return
		}
		respond(logger, w, http.StatusOK, status)
	}
}

// SelectPlexServer returns the handler that selects an authorized Plex server.
func SelectPlexServer(
	mediaManager providerSelector,
	plexAuth plexServerSelector,
	logger *slog.Logger,
) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if err := mediaManager.CheckProvider(media.ProviderPlex); err != nil {
			fail(logger, r, w, err)
			return
		}
		input, err := decodeValid[selectPlexServerRequest](r)
		if err != nil {
			fail(logger, r, w, err)
			return
		}
		input.ServerID = strings.TrimSpace(input.ServerID)
		if err := plexAuth.SelectServer(r.Context(), setupToken(r), input.ServerID); err != nil {
			fail(logger, r, w, err)
			return
		}
		if err := mediaManager.SetActive(r.Context(), media.ProviderPlex); err != nil {
			fail(logger, r, w, err)
			return
		}
		respond(logger, w, http.StatusOK, map[string]string{"status": "connected"})
	}
}

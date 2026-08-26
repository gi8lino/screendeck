package handler

import (
	"context"
	"log/slog"
	"net/http"
	"net/url"
	"strings"

	"github.com/gi8lino/screendeck/internal/jellyfin"
	"github.com/gi8lino/screendeck/internal/media"
)

// jellyfinConnectRequest contains the credentials used to authorize ScreenDeck against Jellyfin.
type jellyfinConnectRequest struct {
	// ServerURL is the absolute URL of the Jellyfin server.
	ServerURL string `json:"serverUrl"`
	// Username is the Jellyfin user ScreenDeck authenticates as.
	Username string `json:"username"`
	// Password is used only for the Jellyfin authentication request.
	Password string `json:"password"`
}

// Valid returns every field-level Jellyfin connection problem.
func (input jellyfinConnectRequest) Valid(context.Context) map[string]string {
	problems := make(map[string]string)

	serverURL := strings.TrimSpace(input.ServerURL)
	parsed, err := url.ParseRequestURI(serverURL)
	if err != nil || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		problems["serverUrl"] = "Enter an absolute HTTP or HTTPS server URL."
	}

	if strings.TrimSpace(input.Username) == "" {
		problems["username"] = "Enter a Jellyfin username."
	}

	return problems
}

// ConnectJellyfin authenticates to a Jellyfin server and selects Jellyfin as the instance media provider.
func ConnectJellyfin(
	mediaManager *media.Manager,
	jellyfinAuth *jellyfin.AuthManager,
	logger *slog.Logger,
) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if err := mediaManager.CheckProvider(media.ProviderJellyfin); err != nil {
			fail(logger, r, w, err)
			return
		}
		input, err := decodeValid[jellyfinConnectRequest](r)
		if err != nil {
			fail(logger, r, w, err)
			return
		}
		input.ServerURL = strings.TrimSpace(input.ServerURL)
		input.Username = strings.TrimSpace(input.Username)
		if err := jellyfinAuth.Connect(r.Context(), input.ServerURL, input.Username, input.Password); err != nil {
			fail(logger, r, w, err)
			return
		}
		if err := mediaManager.SetActive(r.Context(), media.ProviderJellyfin); err != nil {
			fail(logger, r, w, err)
			return
		}
		respond(logger, w, http.StatusOK, map[string]string{"status": "connected"})
	}
}

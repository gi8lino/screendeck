package routes

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"testing/fstest"
	"time"

	"github.com/gi8lino/screendeck/internal/handler"
	"github.com/gi8lino/screendeck/internal/plex"
	"github.com/gi8lino/screendeck/internal/room"
	"github.com/gi8lino/screendeck/internal/store"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestRoomFlowThroughHTTP verifies the room lifecycle through the API.
func TestRoomFlowThroughHTTP(t *testing.T) {
	plexServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/library/sections":
			_, _ = fmt.Fprint(w, `{"MediaContainer":{"Directory":[{"key":"1","title":"Films","type":"movie"}]}}`)
		case "/library/sections/1/all":
			_, _ = fmt.Fprint(w, `{"MediaContainer":{"Metadata":[{"ratingKey":"42","title":"Arrival","year":2016,"thumb":"/poster"}]}}`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer plexServer.Close()

	database, err := store.Open(":memory:")
	require.NoError(t, err)
	defer database.Close()

	plexClient, err := plex.New(plexServer.URL, "token")
	require.NoError(t, err)
	rooms := room.NewService(database, plexClient, time.Hour)
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	_, privateKey, err := ed25519.GenerateKey(rand.Reader)
	require.NoError(t, err)
	require.NoError(t, database.SavePlexAuth(context.Background(), plex.AuthState{
		ClientID: "client", KeyID: "key", PrivateKey: privateKey, UserToken: "user-token", TokenExpiresAt: time.Now().Add(time.Hour),
		ServerID: "server", ServerName: "Test Plex", ServerURL: plexServer.URL, ServerToken: "token",
	}))

	authManager, err := plex.NewAuthManager(context.Background(), database, logger, plexServer.URL, "", false)
	require.NoError(t, err)
	api := handler.New("test", "commit", "http://movies.test", false, rooms, authManager, logger)
	appFS := fstest.MapFS{"index.html": {Data: []byte("<!doctype html><title>ScreenDeck</title>")}}
	router, err := NewRouter(appFS, api, logger, false)
	require.NoError(t, err)

	host := postJSON(t, router, "/api/rooms", `{"name":"Host","libraryKeys":["1"]}`, "")
	require.Equal(t, http.StatusCreated, host.Code, host.Body.String())
	var hostSession room.Session
	decodeResponse(t, host, &hostSession)

	guest := postJSON(t, router, "/api/rooms/join", fmt.Sprintf(`{"name":"Guest","code":%q}`, hostSession.Code), "")
	require.Equal(t, http.StatusCreated, guest.Code, guest.Body.String())
	var guestSession room.Session
	decodeResponse(t, guest, &guestSession)

	hostState := getState(t, router, hostSession)
	require.NotNil(t, hostState.Candidate)
	require.Len(t, hostState.Participants, 2)
	itemID := hostState.Candidate.RatingKey

	first := postJSON(t, router, "/api/rooms/"+hostSession.Code+"/votes", fmt.Sprintf(`{"itemId":%q,"liked":true}`, itemID), hostSession.Token)
	require.Equal(t, http.StatusOK, first.Code, first.Body.String())
	assert.NotContains(t, first.Body.String(), `"matched":true`)

	second := postJSON(t, router, "/api/rooms/"+hostSession.Code+"/votes", fmt.Sprintf(`{"itemId":%q,"liked":true}`, itemID), guestSession.Token)
	require.Equal(t, http.StatusOK, second.Code, second.Body.String())
	assert.Contains(t, second.Body.String(), `"matched":true`)

	state := getState(t, router, guestSession)
	require.Len(t, state.Matches, 1)
	assert.Equal(t, "Arrival", state.Matches[0].Title)
}

// postJSON sends a JSON test request to an HTTP handler.
func postJSON(t *testing.T, router http.Handler, path, body, token string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, path, bytes.NewBufferString(body))
	if token != "" {
		req.Header.Set("X-Participant-Token", token)
	}
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, req)
	return recorder
}

// getState retrieves and decodes room state in an HTTP test.
func getState(t *testing.T, router http.Handler, session room.Session) store.RoomState {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/api/rooms/"+session.Code, nil)
	req.Header.Set("X-Participant-Token", session.Token)
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, req)
	require.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())
	var state store.RoomState
	decodeResponse(t, recorder, &state)
	return state
}

// decodeResponse decodes a recorded JSON response into a test target.
func decodeResponse(t *testing.T, recorder *httptest.ResponseRecorder, target any) {
	t.Helper()
	require.NoError(t, json.NewDecoder(recorder.Body).Decode(target))
}

// TestHealthAndFrontend verifies the health endpoint and embedded frontend.
func TestHealthAndFrontend(t *testing.T) {
	database, err := store.Open(":memory:")
	require.NoError(t, err)
	defer database.Close()

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	authManager, err := plex.NewAuthManager(context.Background(), database, logger, "http://plex.test", "", false)
	require.NoError(t, err)
	api := handler.New("test", "commit", "http://movies.test", false, room.NewService(database, authManager, time.Hour), authManager, logger)
	appFS := fstest.MapFS{"index.html": {Data: []byte("ScreenDeck")}}
	router, err := NewRouter(appFS, api, logger, false)
	require.NoError(t, err)

	health := httptest.NewRecorder()
	router.ServeHTTP(health, httptest.NewRequest(http.MethodGet, "/healthz", nil).WithContext(context.Background()))
	assert.Equal(t, http.StatusOK, health.Code)

	frontend := httptest.NewRecorder()
	router.ServeHTTP(frontend, httptest.NewRequest(http.MethodGet, "/", nil).WithContext(context.Background()))
	assert.Equal(t, http.StatusOK, frontend.Code)
}

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
)

// TestRoomFlowThroughHTTP verifies the room lifecycle through the API.
func TestRoomFlowThroughHTTP(t *testing.T) {
	plexServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/library/sections":
			fmt.Fprint(w, `{"MediaContainer":{"Directory":[{"key":"1","title":"Films","type":"movie"}]}}`)
		case "/library/sections/1/all":
			fmt.Fprint(w, `{"MediaContainer":{"Metadata":[{"ratingKey":"42","title":"Arrival","year":2016,"thumb":"/poster"}]}}`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer plexServer.Close()
	database, err := store.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	plexClient, _ := plex.New(plexServer.URL, "token")
	rooms := room.NewService(database, plexClient, time.Hour)
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	_, privateKey, _ := ed25519.GenerateKey(rand.Reader)
	if err := database.SavePlexAuth(context.Background(), plex.AuthState{
		ClientID: "client", KeyID: "key", PrivateKey: privateKey, UserToken: "user-token", TokenExpiresAt: time.Now().Add(time.Hour),
		ServerID: "server", ServerName: "Test Plex", ServerURL: plexServer.URL, ServerToken: "token",
	}); err != nil {
		t.Fatal(err)
	}
	authManager, err := plex.NewAuthManager(context.Background(), database, logger, plexServer.URL, "", false)
	if err != nil {
		t.Fatal(err)
	}
	api := handler.New("test", "commit", "http://movies.test", false, rooms, authManager, logger)
	appFS := fstest.MapFS{"index.html": {Data: []byte("<!doctype html><title>ScreenDeck</title>")}}
	router, err := NewRouter(appFS, api, logger, false)
	if err != nil {
		t.Fatal(err)
	}

	host := postJSON(t, router, "/api/rooms", `{"name":"Host","libraryKeys":["1"]}`, "")
	if host.Code != http.StatusCreated {
		t.Fatalf("create status=%d body=%s", host.Code, host.Body.String())
	}
	var hostSession room.Session
	decodeResponse(t, host, &hostSession)
	guest := postJSON(t, router, "/api/rooms/join", fmt.Sprintf(`{"name":"Guest","code":%q}`, hostSession.Code), "")
	var guestSession room.Session
	decodeResponse(t, guest, &guestSession)

	hostState := getState(t, router, hostSession)
	if hostState.Candidate == nil || len(hostState.Participants) != 2 {
		t.Fatalf("unexpected host state: %#v", hostState)
	}
	movieID := hostState.Candidate.RatingKey
	first := postJSON(t, router, "/api/rooms/"+hostSession.Code+"/votes", fmt.Sprintf(`{"movieId":%q,"liked":true}`, movieID), hostSession.Token)
	if first.Code != http.StatusOK || bytes.Contains(first.Body.Bytes(), []byte(`"matched":true`)) {
		t.Fatalf("first vote: %d %s", first.Code, first.Body.String())
	}
	second := postJSON(t, router, "/api/rooms/"+hostSession.Code+"/votes", fmt.Sprintf(`{"movieId":%q,"liked":true}`, movieID), guestSession.Token)
	if second.Code != http.StatusOK || !bytes.Contains(second.Body.Bytes(), []byte(`"matched":true`)) {
		t.Fatalf("second vote: %d %s", second.Code, second.Body.String())
	}
	state := getState(t, router, guestSession)
	if len(state.Matches) != 1 || state.Matches[0].Title != "Arrival" {
		t.Fatalf("unexpected matches: %#v", state.Matches)
	}
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
	if recorder.Code != http.StatusOK {
		t.Fatalf("state status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	var state store.RoomState
	decodeResponse(t, recorder, &state)
	return state
}

// decodeResponse decodes a recorded JSON response into a test target.
func decodeResponse(t *testing.T, recorder *httptest.ResponseRecorder, target any) {
	t.Helper()
	if err := json.NewDecoder(recorder.Body).Decode(target); err != nil {
		t.Fatal(err)
	}
}

// TestHealthAndFrontend verifies the health endpoint and embedded frontend.
func TestHealthAndFrontend(t *testing.T) {
	database, _ := store.Open(":memory:")
	defer database.Close()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	authManager, _ := plex.NewAuthManager(context.Background(), database, logger, "http://plex.test", "", false)
	api := handler.New("test", "commit", "http://movies.test", false, room.NewService(database, authManager, time.Hour), authManager, logger)
	appFS := fstest.MapFS{"index.html": {Data: []byte("ScreenDeck")}}
	router, err := NewRouter(appFS, api, logger, false)
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"/healthz", "/"} {
		recorder := httptest.NewRecorder()
		router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, path, nil).WithContext(context.Background()))
		if recorder.Code != http.StatusOK {
			t.Fatalf("GET %s status=%d", path, recorder.Code)
		}
	}
}

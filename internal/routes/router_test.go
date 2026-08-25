package routes

import (
	"bytes"
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
	mediafactory "github.com/gi8lino/screendeck/internal/media/factory"
	"github.com/gi8lino/screendeck/internal/plex"
	"github.com/gi8lino/screendeck/internal/room"
	"github.com/gi8lino/screendeck/internal/store"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRoomFlowThroughHTTP(t *testing.T) {
	t.Parallel()

	plexServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/library/sections":
			fmt.Fprint(w, `{"MediaContainer":{"Directory":[{"key":"1","title":"Films","type":"movie"}]}}`) // nolint:errcheck
		case "/library/sections/1/all":
			fmt.Fprint(w, `{"MediaContainer":{"Metadata":[{"ratingKey":"42","title":"Arrival","year":2016,"thumb":"/poster"}]}}`) // nolint:errcheck
		default:
			http.NotFound(w, r)
		}
	}))
	defer plexServer.Close()

	database, err := store.Open(":memory:")
	require.NoError(t, err)
	defer database.Close() // nolint:errcheck

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	require.NoError(t, database.SavePlexAuth(t.Context(), plex.AuthState{
		Method:      plex.AuthMethodStandard,
		ClientID:    "client",
		UserToken:   "user-token",
		ServerID:    "server",
		ServerName:  "Test Plex",
		ServerURL:   plexServer.URL,
		ServerToken: "token",
	}))

	mediaServices, err := mediafactory.New(
		database,
		logger,
		mediafactory.Options{Version: "test"},
	).Create(t.Context())
	require.NoError(t, err)
	rooms := room.NewService(database, mediaServices.Media, time.Hour, nil)
	api := handler.New(
		"test",
		"commit",
		"http://movies.test",
		false,
		rooms,
		mediaServices.Media,
		mediaServices.Plex,
		mediaServices.Jellyfin,
		database,
		logger,
	)
	appFS := fstest.MapFS{"index.html": {Data: []byte("<!doctype html><title>ScreenDeck</title>")}}
	router := NewRouter(appFS, api, logger, false)

	invalid := postJSON(t, router, "/api/rooms", `{}`, "")
	require.Equal(t, http.StatusBadRequest, invalid.Code, invalid.Body.String())
	var validation struct {
		Error    string            `json:"error"`
		Problems map[string]string `json:"problems"`
	}
	decodeResponse(t, invalid, &validation)
	assert.Equal(t, "request validation failed", validation.Error)
	assert.Equal(t, "Enter your name.", validation.Problems["name"])
	assert.Equal(t, "Select at least one library.", validation.Problems["libraryKeys"])

	host := postJSON(t, router, "/api/rooms", `{"name":"Host","libraryKeys":["1"],"lifetimeHours":6}`, "")
	require.Equal(t, http.StatusCreated, host.Code, host.Body.String())
	var hostSession room.Session
	decodeResponse(t, host, &hostSession)

	lockRequest := httptest.NewRequest(
		http.MethodPatch,
		"/api/rooms/"+hostSession.Code+"/settings",
		bytes.NewBufferString(`{"locked":true}`),
	)
	lockRequest.Header.Set("X-Participant-Token", hostSession.Token)
	lockResponse := httptest.NewRecorder()
	router.ServeHTTP(lockResponse, lockRequest)
	require.Equal(t, http.StatusOK, lockResponse.Code, lockResponse.Body.String())

	lockedGuest := postJSON(t, router, "/api/rooms/join", fmt.Sprintf(`{"name":"Guest","code":%q}`, hostSession.Code), "")
	require.Equal(t, http.StatusConflict, lockedGuest.Code, lockedGuest.Body.String())
	assert.Contains(t, lockedGuest.Body.String(), "room is locked")

	unlockRequest := httptest.NewRequest(
		http.MethodPatch,
		"/api/rooms/"+hostSession.Code+"/settings",
		bytes.NewBufferString(`{"locked":false}`),
	)
	unlockRequest.Header.Set("X-Participant-Token", hostSession.Token)
	unlockResponse := httptest.NewRecorder()
	router.ServeHTTP(unlockResponse, unlockRequest)
	require.Equal(t, http.StatusOK, unlockResponse.Code, unlockResponse.Body.String())

	guest := postJSON(t, router, "/api/rooms/join", fmt.Sprintf(`{"name":"Guest","code":%q}`, hostSession.Code), "")
	require.Equal(t, http.StatusCreated, guest.Code, guest.Body.String())
	var guestSession room.Session
	decodeResponse(t, guest, &guestSession)

	hostCookies := host.Result().Cookies()
	require.Len(t, hostCookies, 1)
	roomsRequest := httptest.NewRequest(http.MethodGet, "/api/me/rooms", nil)
	roomsRequest.AddCookie(hostCookies[0])
	roomsResponse := httptest.NewRecorder()
	router.ServeHTTP(roomsResponse, roomsRequest)
	require.Equal(t, http.StatusOK, roomsResponse.Code, roomsResponse.Body.String())
	var memberships []store.RoomMembership
	decodeResponse(t, roomsResponse, &memberships)
	require.Len(t, memberships, 1)
	assert.Equal(t, hostSession.Code, memberships[0].Code)
	assert.True(t, memberships[0].IsHost)
	assert.Equal(t, 2, memberships[0].ParticipantCount)

	resumeRequest := httptest.NewRequest(http.MethodPost, "/api/me/rooms/"+hostSession.Code+"/session", nil)
	resumeRequest.AddCookie(hostCookies[0])
	resumeResponse := httptest.NewRecorder()
	router.ServeHTTP(resumeResponse, resumeRequest)
	require.Equal(t, http.StatusOK, resumeResponse.Code, resumeResponse.Body.String())
	var resumed room.Session
	decodeResponse(t, resumeResponse, &resumed)
	assert.Equal(t, hostSession, resumed)

	rejoinRequest := httptest.NewRequest(
		http.MethodPost,
		"/api/rooms/join",
		bytes.NewBufferString(fmt.Sprintf(`{"name":"Duplicate","code":%q}`, hostSession.Code)),
	)
	rejoinRequest.AddCookie(hostCookies[0])
	rejoinResponse := httptest.NewRecorder()
	router.ServeHTTP(rejoinResponse, rejoinRequest)
	require.Equal(t, http.StatusCreated, rejoinResponse.Code, rejoinResponse.Body.String())
	var rejoined room.Session
	decodeResponse(t, rejoinResponse, &rejoined)
	assert.Equal(t, hostSession, rejoined)

	hostState := getState(t, router, hostSession)
	require.NotNil(t, hostState.Candidate)
	require.Len(t, hostState.Participants, 2)
	assert.WithinDuration(t, time.Now().Add(6*time.Hour), hostState.Room.ExpiresAt, 5*time.Second)
	itemID := hostState.Candidate.ID

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

func TestJellyfinFlowThroughHTTP(t *testing.T) {
	t.Parallel()

	jellyfinServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/System/Info/Public":
			fmt.Fprint(w, `{"ServerName":"Test Jellyfin","Id":"server-1"}`) // nolint:errcheck
		case "/Users/AuthenticateByName":
			fmt.Fprint(w, `{"AccessToken":"access-token","ServerId":"server-1","User":{"Id":"user-1","Name":"Host"}}`) // nolint:errcheck
		case "/Users/user-1/Views":
			fmt.Fprint(w, `{"Items":[{"Id":"movies","Name":"Films","CollectionType":"movies"}]}`) // nolint:errcheck
		case "/Items":
			fmt.Fprint(w, `{"Items":[{"Id":"item-1","Name":"Arrival","Type":"Movie","ProductionYear":2016,"ImageTags":{"Primary":"poster"}}]}`) // nolint:errcheck
		default:
			http.NotFound(w, r)
		}
	}))
	defer jellyfinServer.Close()

	database, err := store.Open(":memory:")
	require.NoError(t, err)
	defer database.Close() // nolint:errcheck

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	mediaServices, err := mediafactory.New(
		database,
		logger,
		mediafactory.Options{Version: "test"},
	).Create(t.Context())
	require.NoError(t, err)
	rooms := room.NewService(database, mediaServices.Media, time.Hour, nil)
	api := handler.New(
		"test",
		"commit",
		"http://movies.test",
		false,
		rooms,
		mediaServices.Media,
		mediaServices.Plex,
		mediaServices.Jellyfin,
		database,
		logger,
	)
	appFS := fstest.MapFS{"index.html": {Data: []byte("ScreenDeck")}}
	router := NewRouter(appFS, api, logger, false)

	connected := postJSON(
		t,
		router,
		"/api/jellyfin/connect",
		fmt.Sprintf(`{"serverUrl":%q,"username":"Host","password":"secret"}`, jellyfinServer.URL),
		"",
	)
	require.Equal(t, http.StatusOK, connected.Code, connected.Body.String())

	config := httptest.NewRecorder()
	router.ServeHTTP(config, httptest.NewRequest(http.MethodGet, "/api/config", nil))
	require.Equal(t, http.StatusOK, config.Code, config.Body.String())
	assert.Contains(t, config.Body.String(), `"mediaConfigured":true`)
	assert.Contains(t, config.Body.String(), `"mediaProvider":"jellyfin"`)
	assert.Contains(t, config.Body.String(), `"mediaServerName":"Test Jellyfin"`)

	libraries := httptest.NewRecorder()
	router.ServeHTTP(libraries, httptest.NewRequest(http.MethodGet, "/api/libraries", nil))
	require.Equal(t, http.StatusOK, libraries.Code, libraries.Body.String())
	assert.Contains(t, libraries.Body.String(), `"title":"Films"`)

	host := postJSON(t, router, "/api/rooms", `{"name":"Host","libraryKeys":["movies"]}`, "")
	require.Equal(t, http.StatusCreated, host.Code, host.Body.String())
	var session room.Session
	decodeResponse(t, host, &session)

	state := getState(t, router, session)
	require.NotNil(t, state.Candidate)
	assert.Equal(t, "item-1", state.Candidate.ID)
	assert.Equal(t, "Arrival", state.Candidate.Title)
}

// postJSON sends a JSON test request to an HTTP handler.
func postJSON(
	t *testing.T,
	router http.Handler,
	path string,
	body string,
	token string,
) *httptest.ResponseRecorder {
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

func TestHealthAndFrontend(t *testing.T) {
	t.Parallel()

	database, err := store.Open(":memory:")
	require.NoError(t, err)
	defer database.Close() // nolint:errcheck

	var logs bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&logs, nil))
	mediaServices, err := mediafactory.New(
		database,
		logger,
		mediafactory.Options{Version: "test"},
	).Create(t.Context())
	require.NoError(t, err)
	api := handler.New(
		"test",
		"commit",
		"http://movies.test",
		false,
		room.NewService(database, mediaServices.Media, time.Hour, nil),
		mediaServices.Media,
		mediaServices.Plex,
		mediaServices.Jellyfin,
		database,
		logger,
	)
	appFS := fstest.MapFS{"index.html": {Data: []byte("ScreenDeck")}}
	router := NewRouter(appFS, api, logger, true)

	health := httptest.NewRecorder()
	router.ServeHTTP(health, httptest.NewRequest(http.MethodGet, "/healthz", nil).WithContext(t.Context()))
	assert.Equal(t, http.StatusOK, health.Code)

	frontend := httptest.NewRecorder()
	router.ServeHTTP(frontend, httptest.NewRequest(http.MethodGet, "/", nil).WithContext(t.Context()))
	assert.Equal(t, http.StatusOK, frontend.Code)

	config := httptest.NewRecorder()
	router.ServeHTTP(config, httptest.NewRequest(http.MethodGet, "/api/config", nil).WithContext(t.Context()))
	assert.Equal(t, http.StatusOK, config.Code)
	assert.Contains(t, logs.String(), `"path":"/api/config"`)
}

package handler

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gi8lino/screendeck/internal/room"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type fakeMembershipReader struct {
	identityToken string
	memberships   []room.Membership
	err           error
}

func (f *fakeMembershipReader) RoomsForIdentity(_ context.Context, identityToken string) ([]room.Membership, error) {
	f.identityToken = identityToken
	return f.memberships, f.err
}

type fakeMembershipResumer struct {
	identityToken string
	code          string
	session       room.Session
	err           error
}

func (f *fakeMembershipResumer) ResumeIdentity(_ context.Context, identityToken, code string) (room.Session, error) {
	f.identityToken = identityToken
	f.code = code
	return f.session, f.err
}

// TestMyRooms verifies browser identity is forwarded when listing memberships.
func TestMyRooms(t *testing.T) {
	t.Parallel()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	t.Run("uses existing browser identity", func(t *testing.T) {
		t.Parallel()
		identityToken, err := newBrowserIdentityToken()
		require.NoError(t, err)

		rooms := &fakeMembershipReader{memberships: []room.Membership{{Code: "ABC234", Name: "Alice"}}}
		request := httptest.NewRequest(http.MethodGet, "/api/me/rooms", nil)
		request.AddCookie(&http.Cookie{Name: browserIdentityCookieName, Value: identityToken})
		response := httptest.NewRecorder()

		MyRooms(rooms, logger).ServeHTTP(response, request)

		assert.Equal(t, http.StatusOK, response.Code)
		assert.Equal(t, identityToken, rooms.identityToken)
		assert.Empty(t, response.Result().Cookies())
		assert.Contains(t, response.Body.String(), `"code":"ABC234"`)
	})

	t.Run("maps service error", func(t *testing.T) {
		t.Parallel()
		identityToken, err := newBrowserIdentityToken()
		require.NoError(t, err)

		rooms := &fakeMembershipReader{err: room.ErrNotFound}
		request := httptest.NewRequest(http.MethodGet, "/api/me/rooms", nil)
		request.AddCookie(&http.Cookie{Name: browserIdentityCookieName, Value: identityToken})
		response := httptest.NewRecorder()

		MyRooms(rooms, logger).ServeHTTP(response, request)

		assert.Equal(t, http.StatusNotFound, response.Code)
	})
}

// TestResumeRoom verifies browser identity and room code are forwarded when resuming.
func TestResumeRoom(t *testing.T) {
	t.Parallel()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	t.Run("returns saved session", func(t *testing.T) {
		t.Parallel()
		identityToken, err := newBrowserIdentityToken()
		require.NoError(t, err)

		rooms := &fakeMembershipResumer{session: room.Session{Code: "ABC234", Token: "participant-token"}}
		request := httptest.NewRequest(http.MethodPost, "/api/me/rooms/ABC234/session", nil)
		request.SetPathValue("code", "ABC234")
		request.AddCookie(&http.Cookie{Name: browserIdentityCookieName, Value: identityToken})
		response := httptest.NewRecorder()

		ResumeRoom(rooms, logger).ServeHTTP(response, request)

		assert.Equal(t, http.StatusOK, response.Code)
		assert.Equal(t, identityToken, rooms.identityToken)
		assert.Equal(t, "ABC234", rooms.code)
		assert.JSONEq(t, `{"code":"ABC234","token":"participant-token"}`, response.Body.String())
	})

	t.Run("maps service error", func(t *testing.T) {
		t.Parallel()
		identityToken, err := newBrowserIdentityToken()
		require.NoError(t, err)

		rooms := &fakeMembershipResumer{err: room.ErrForbidden}
		request := httptest.NewRequest(http.MethodPost, "/api/me/rooms/ABC234/session", nil)
		request.SetPathValue("code", "ABC234")
		request.AddCookie(&http.Cookie{Name: browserIdentityCookieName, Value: identityToken})
		response := httptest.NewRecorder()

		ResumeRoom(rooms, logger).ServeHTTP(response, request)

		assert.Equal(t, http.StatusForbidden, response.Code)
	})
}

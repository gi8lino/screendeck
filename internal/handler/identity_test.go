package handler

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestEnsureBrowserIdentity verifies browser identities are created once and restored from the cookie.
func TestEnsureBrowserIdentity(t *testing.T) {
	request := httptest.NewRequest(http.MethodGet, "/api/me/rooms", nil)
	response := httptest.NewRecorder()
	token, err := ensureBrowserIdentity(response, request)
	require.NoError(t, err)
	assert.True(t, validBrowserIdentityToken(token))

	cookies := response.Result().Cookies()
	require.Len(t, cookies, 1)
	assert.Equal(t, browserIdentityCookieName, cookies[0].Name)
	assert.Equal(t, token, cookies[0].Value)
	assert.Equal(t, "/", cookies[0].Path)
	assert.True(t, cookies[0].HttpOnly)
	assert.Equal(t, http.SameSiteLaxMode, cookies[0].SameSite)

	restoredRequest := httptest.NewRequest(http.MethodGet, "/api/me/rooms", nil)
	restoredRequest.AddCookie(cookies[0])
	restoredResponse := httptest.NewRecorder()
	restored, err := ensureBrowserIdentity(restoredResponse, restoredRequest)
	require.NoError(t, err)
	assert.Equal(t, token, restored)
	assert.Empty(t, restoredResponse.Result().Cookies())
}

// TestValidBrowserIdentityToken verifies only correctly sized URL-safe identity tokens are accepted.
func TestValidBrowserIdentityToken(t *testing.T) {
	token, err := newBrowserIdentityToken()
	require.NoError(t, err)
	assert.True(t, validBrowserIdentityToken(token))
	assert.False(t, validBrowserIdentityToken("not-a-valid-token"))
	assert.False(t, validBrowserIdentityToken(""))
}

// TestRequestUsesHTTPS verifies direct TLS and forwarded HTTPS requests mark identity cookies secure.
func TestRequestUsesHTTPS(t *testing.T) {
	plain := httptest.NewRequest(http.MethodGet, "http://example.test/", nil)
	assert.False(t, requestUsesHTTPS(plain))

	forwarded := httptest.NewRequest(http.MethodGet, "http://example.test/", nil)
	forwarded.Header.Set("X-Forwarded-Proto", "https")
	assert.True(t, requestUsesHTTPS(forwarded))

	secure := httptest.NewRequest(http.MethodGet, "https://example.test/", nil)
	assert.True(t, requestUsesHTTPS(secure))
}

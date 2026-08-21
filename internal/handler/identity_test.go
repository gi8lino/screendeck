package handler

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestEnsureBrowserIdentity verifies browser identity creation and reuse.
func TestEnsureBrowserIdentity(t *testing.T) {
	t.Parallel()

	t.Run("creates identity", func(t *testing.T) {
		t.Parallel()
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
	})

	t.Run("reuses valid identity", func(t *testing.T) {
		t.Parallel()
		token, err := newBrowserIdentityToken()
		require.NoError(t, err)

		request := httptest.NewRequest(http.MethodGet, "/api/me/rooms", nil)
		request.AddCookie(&http.Cookie{Name: browserIdentityCookieName, Value: token})
		response := httptest.NewRecorder()

		restored, err := ensureBrowserIdentity(response, request)
		require.NoError(t, err)
		assert.Equal(t, token, restored)
		assert.Empty(t, response.Result().Cookies())
	})
}

// TestNewBrowserIdentityToken verifies generated browser identity tokens are valid.
func TestNewBrowserIdentityToken(t *testing.T) {
	t.Parallel()

	t.Run("generates valid token", func(t *testing.T) {
		t.Parallel()
		token, err := newBrowserIdentityToken()
		require.NoError(t, err)
		assert.True(t, validBrowserIdentityToken(token))
	})
}

// TestValidBrowserIdentityToken verifies browser identity token validation.
func TestValidBrowserIdentityToken(t *testing.T) {
	t.Parallel()

	t.Run("valid", func(t *testing.T) {
		t.Parallel()
		token, err := newBrowserIdentityToken()
		require.NoError(t, err)
		assert.True(t, validBrowserIdentityToken(token))
	})

	t.Run("invalid encoding", func(t *testing.T) {
		t.Parallel()
		assert.False(t, validBrowserIdentityToken("not-a-valid-token"))
	})

	t.Run("empty", func(t *testing.T) {
		t.Parallel()
		assert.False(t, validBrowserIdentityToken(""))
	})

	t.Run("wrong size", func(t *testing.T) {
		t.Parallel()
		assert.False(t, validBrowserIdentityToken("YWJj"))
	})
}

// TestRequestUsesHTTPS verifies direct and proxied HTTPS detection.
func TestRequestUsesHTTPS(t *testing.T) {
	t.Parallel()

	t.Run("plain HTTP", func(t *testing.T) {
		t.Parallel()
		request := httptest.NewRequest(http.MethodGet, "http://example.test/", nil)
		assert.False(t, requestUsesHTTPS(request))
	})

	t.Run("forwarded HTTPS", func(t *testing.T) {
		t.Parallel()
		request := httptest.NewRequest(http.MethodGet, "http://example.test/", nil)
		request.Header.Set("X-Forwarded-Proto", "https")
		assert.True(t, requestUsesHTTPS(request))
	})

	t.Run("direct TLS", func(t *testing.T) {
		t.Parallel()
		request := httptest.NewRequest(http.MethodGet, "https://example.test/", nil)
		assert.True(t, requestUsesHTTPS(request))
	})
}

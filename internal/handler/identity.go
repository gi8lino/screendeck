package handler

import (
	"crypto/rand"
	"encoding/base64"
	"errors"
	"net/http"
	"strings"
	"time"
)

const (
	browserIdentityCookieName = "screendeck_identity"
	browserIdentitySize       = 32
	browserIdentityLifetime   = 365 * 24 * time.Hour
)

// errBrowserIdentityUnavailable indicates that the server could not establish a browser identity.
var errBrowserIdentityUnavailable = errors.New("browser identity unavailable")

// ensureBrowserIdentity returns a valid persistent browser identity and creates one when necessary.
func ensureBrowserIdentity(w http.ResponseWriter, r *http.Request) (string, error) {
	cookie, err := r.Cookie(browserIdentityCookieName)
	if err == nil && validBrowserIdentityToken(cookie.Value) {
		return cookie.Value, nil
	}

	token, err := newBrowserIdentityToken()
	if err != nil {
		return "", errors.Join(errBrowserIdentityUnavailable, err)
	}
	http.SetCookie(w, &http.Cookie{
		Name:     browserIdentityCookieName,
		Value:    token,
		Path:     "/",
		Expires:  time.Now().Add(browserIdentityLifetime),
		MaxAge:   int(browserIdentityLifetime / time.Second),
		HttpOnly: true,
		Secure:   requestUsesHTTPS(r),
		SameSite: http.SameSiteLaxMode,
	})
	return token, nil
}

// newBrowserIdentityToken creates a cryptographically random browser identity token.
func newBrowserIdentityToken() (string, error) {
	raw := make([]byte, browserIdentitySize)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(raw), nil
}

// validBrowserIdentityToken reports whether a cookie contains a correctly encoded identity token.
func validBrowserIdentityToken(token string) bool {
	raw, err := base64.RawURLEncoding.DecodeString(strings.TrimSpace(token))
	return err == nil && len(raw) == browserIdentitySize
}

// requestUsesHTTPS reports whether a request arrived over HTTPS directly or through a trusted proxy header.
func requestUsesHTTPS(r *http.Request) bool {
	if r.TLS != nil {
		return true
	}
	return strings.EqualFold(strings.TrimSpace(r.Header.Get("X-Forwarded-Proto")), "https")
}

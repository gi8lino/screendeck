package jellyfin

import "errors"

var (
	// ErrInvalidServerURL indicates that the configured Jellyfin URL is not absolute HTTP or HTTPS.
	ErrInvalidServerURL = errors.New("invalid Jellyfin server URL")
	// ErrInvalidClientConfig indicates missing credentials required to create a Jellyfin client.
	ErrInvalidClientConfig = errors.New("invalid Jellyfin client configuration")
	// ErrInvalidLibrary indicates that a Jellyfin library cannot be queried safely.
	ErrInvalidLibrary = errors.New("invalid Jellyfin library")
	// ErrInvalidPosterReference indicates that a stored Jellyfin poster reference is malformed.
	ErrInvalidPosterReference = errors.New("invalid Jellyfin poster reference")
	// ErrAuthenticationFailed indicates that Jellyfin rejected the supplied username or password.
	ErrAuthenticationFailed = errors.New("Jellyfin authentication failed")
	// ErrServerContact indicates that the Jellyfin server could not be reached.
	ErrServerContact = errors.New("contact Jellyfin server")
	// ErrServerResponse indicates that Jellyfin returned an unsuccessful HTTP response.
	ErrServerResponse = errors.New("unexpected Jellyfin server response")
	// ErrServerDecode indicates that a Jellyfin JSON response could not be decoded.
	ErrServerDecode = errors.New("decode Jellyfin server response")
	// ErrAuthNotFound indicates that no Jellyfin authentication state has been persisted.
	ErrAuthNotFound = errors.New("Jellyfin authentication not found")
)

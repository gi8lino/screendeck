package plex

import "errors"

var (
	// ErrAuthNotFound indicates that no saved Plex authentication exists.
	ErrAuthNotFound = errors.New("Plex authentication not found")
	// ErrInvalidCloudURL indicates that the Plex cloud endpoint is invalid.
	ErrInvalidCloudURL = errors.New("invalid Plex cloud URL")
	// ErrInvalidServerURLOverride indicates that the runtime Plex URL override is invalid.
	ErrInvalidServerURLOverride = errors.New("invalid Plex server URL override")
	// ErrAlreadyConfigured indicates that Plex has already been configured.
	ErrAlreadyConfigured = errors.New("Plex is already configured")
	// ErrInvalidAuthMethod indicates that an unsupported Plex authentication method was requested.
	ErrInvalidAuthMethod = errors.New("invalid Plex authentication method")
	// ErrExperimentalAuthDisabled indicates that JWT authentication was requested without experimental mode.
	ErrExperimentalAuthDisabled = errors.New("experimental Plex JWT authentication is disabled")
	// ErrInvalidAuthorizationPIN indicates an incomplete PIN response from Plex.
	ErrInvalidAuthorizationPIN = errors.New("Plex returned an invalid authorization PIN")
	// ErrAuthorizationExpired indicates that the temporary authorization session expired.
	ErrAuthorizationExpired = errors.New("Plex authorization session expired")
	// ErrAuthorizationIncomplete indicates that server selection preceded authorization.
	ErrAuthorizationIncomplete = errors.New("complete Plex authorization first")
	// ErrServerUnavailable indicates that the selected server was not discovered.
	ErrServerUnavailable = errors.New("selected Plex server is unavailable")
	// ErrNoUsableConnection indicates that Plex advertised no valid server connection.
	ErrNoUsableConnection = errors.New("selected Plex server has no usable connection")
	// ErrEmptyRefreshedToken indicates that Plex returned no refreshed JWT.
	ErrEmptyRefreshedToken = errors.New("Plex returned an empty refreshed token")
	// ErrNotConfigured indicates that no usable Plex configuration exists.
	ErrNotConfigured = errors.New("Plex is not configured")
	// ErrMissingToken indicates that no candidate authentication token was available.
	ErrMissingToken = errors.New("Plex did not provide an authentication token")
	// ErrNoServers indicates that the account exposes no accessible media servers.
	ErrNoServers = errors.New("no accessible Plex Media Servers were found")
	// ErrServerVerification categorizes failures while verifying server access.
	ErrServerVerification = errors.New("verify Plex server")
	// ErrAuthenticationRefresh categorizes Plex JWT refresh failures.
	ErrAuthenticationRefresh = errors.New("refresh Plex authentication")
	// ErrCloudUnavailable categorizes failures contacting the Plex cloud API.
	ErrCloudUnavailable = errors.New("contact Plex authentication service")
	// ErrCloudResponse categorizes unsuccessful Plex cloud API responses.
	ErrCloudResponse = errors.New("Plex authentication service returned an error")
	// ErrCloudDecode categorizes malformed Plex cloud API responses.
	ErrCloudDecode = errors.New("decode Plex authentication response")
	// ErrServerContact categorizes failures contacting a Plex Media Server.
	ErrServerContact = errors.New("contact Plex server")
	// ErrServerResponse categorizes unsuccessful Plex Media Server responses.
	ErrServerResponse = errors.New("Plex server returned an error")
	// ErrServerDecode categorizes malformed Plex Media Server responses.
	ErrServerDecode = errors.New("decode Plex server response")
	// ErrInvalidClientConfig indicates that the Plex client URL or token is missing.
	ErrInvalidClientConfig = errors.New("Plex URL and token are required")
	// ErrInvalidClientID indicates that the Plex client identifier is missing.
	ErrInvalidClientID = errors.New("Plex client identifier is required")
	// ErrInvalidServerURL indicates that a Plex Media Server URL is invalid.
	ErrInvalidServerURL = errors.New("invalid Plex URL")
	// ErrInvalidLibrary indicates that a Plex library cannot be requested safely.
	ErrInvalidLibrary = errors.New("invalid Plex library")
	// ErrInvalidPosterPath indicates that a Plex poster path cannot be requested safely.
	ErrInvalidPosterPath = errors.New("invalid Plex poster path")
)

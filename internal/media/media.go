package media

import (
	"context"
	"errors"
	"net/http"
)

// ProviderID identifies a supported media-server integration.
type ProviderID string

const (
	// ProviderPlex identifies Plex Media Server.
	ProviderPlex ProviderID = "plex"
	// ProviderJellyfin identifies Jellyfin.
	ProviderJellyfin ProviderID = "jellyfin"
)

var (
	// ErrNotConfigured indicates that no media provider is ready for catalog access.
	ErrNotConfigured = errors.New("media server is not configured")
	// ErrProviderNotFound indicates that no active provider has been persisted yet.
	ErrProviderNotFound = errors.New("media provider is not configured")
	// ErrProviderConflict indicates that a different provider is already active.
	ErrProviderConflict = errors.New("a different media provider is already configured")
	// ErrUnknownProvider indicates that a provider name is unsupported.
	ErrUnknownProvider = errors.New("unknown media provider")
	// ErrDuplicateProvider indicates that more than one implementation registered the same provider ID.
	ErrDuplicateProvider = errors.New("duplicate media provider")
)

// Library describes one movie or TV library exposed by a media server.
type Library struct {
	// Key is the provider-specific stable library identifier.
	Key string `json:"key"`
	// Title is the display title.
	Title string `json:"title"`
	// Type identifies the supported media type: movie or show.
	Type string `json:"type"`
}

// Item contains the provider-neutral metadata ScreenDeck needs for a movie or show.
type Item struct {
	// ID is the provider-specific stable item identifier.
	ID string `json:"id"`
	// LibraryKey identifies the library containing the item.
	LibraryKey string `json:"libraryKey"`
	// Type identifies the media type: movie or show.
	Type string `json:"type"`
	// GUID is a stable provider or external identifier when one is available.
	GUID string `json:"guid"`
	// Title is the display title.
	Title string `json:"title"`
	// Year is the release year when available.
	Year int `json:"year"`
	// Summary is the media description text.
	Summary string `json:"summary"`
	// Duration is the duration in milliseconds.
	Duration int `json:"duration"`
	// Rating is the provider's numeric community rating.
	Rating float64 `json:"rating"`
	// Poster is the provider-specific poster reference persisted by ScreenDeck.
	Poster string `json:"-"`
	// Genres contains genre names reported by the provider.
	Genres []string `json:"genres"`
	// Viewed reports whether the configured media user has watched the item.
	Viewed bool `json:"viewed"`
	// AddedAt is the provider's added-at Unix timestamp when available.
	AddedAt int64 `json:"addedAt"`
}

// Catalog defines the common read-only media operations required by ScreenDeck.
type Catalog interface {
	Libraries(context.Context) ([]Library, error)
	Items(context.Context, Library) ([]Item, error)
	Poster(context.Context, string) (*http.Response, error)
}

// Provider is a media-server integration that exposes ScreenDeck's runtime catalog contract.
// Provider-specific setup and authentication remain outside this interface.
type Provider interface {
	Catalog
	ID() ProviderID
	Configured() (configured bool, serverName string)
}

// ProviderStore persists the active media provider.
type ProviderStore interface {
	LoadMediaProvider(context.Context) (ProviderID, error)
	SaveMediaProvider(context.Context, ProviderID) error
}

// Status describes the configured media provider exposed to the browser.
type Status struct {
	// Configured reports whether catalog access is available.
	Configured bool `json:"configured"`
	// Provider identifies the configured provider.
	Provider ProviderID `json:"provider,omitempty"`
	// ServerName is the friendly media-server name.
	ServerName string `json:"serverName,omitempty"`
}

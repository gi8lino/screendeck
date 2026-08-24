package plex

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/gi8lino/screendeck/internal/media"
)

// Client performs authenticated read-only requests against a Plex Media Server.
type Client struct {
	// baseURL is the parsed Plex server URL.
	baseURL *url.URL
	// token is the Plex token sent with server requests.
	token string
	// clientID is the Plex client identifier sent with requests.
	clientID string
	// httpClient executes Plex HTTP requests.
	httpClient *http.Client
}

// librariesResponse models the top-level Plex library response.
type librariesResponse struct {
	// MediaContainer contains the Plex response payload.
	MediaContainer librariesContainer `json:"MediaContainer"`
}

// librariesContainer contains Plex library directory entries.
type librariesContainer struct {
	// Directories contains raw Plex library section entries.
	Directories []librarySection `json:"Directory"`
}

// librarySection models a Plex library section before conversion to media.Library.
type librarySection struct {
	// Key identifies a Plex library section.
	Key string `json:"key"`
	// Title is the Plex library display title.
	Title string `json:"title"`
	// Type identifies the Plex library type.
	Type string `json:"type"`
}

// itemsResponse models the top-level Plex media item response.
type itemsResponse struct {
	// MediaContainer contains the Plex response payload.
	MediaContainer itemsContainer `json:"MediaContainer"`
}

// itemsContainer contains Plex media metadata entries.
type itemsContainer struct {
	// Metadata contains media entries.
	Metadata []metadataItem `json:"Metadata"`
}

// metadataItem models the raw Plex fields used to build a provider-neutral media item.
type metadataItem struct {
	// RatingKey is Plex's stable item identifier.
	RatingKey string `json:"ratingKey"`
	// GUID is Plex's item GUID.
	GUID string `json:"guid"`
	// Title is the item display title.
	Title string `json:"title"`
	// Year is the release year.
	Year int `json:"year"`
	// Summary is the Plex item description.
	Summary string `json:"summary"`
	// Duration is the Plex duration in milliseconds.
	Duration int `json:"duration"`
	// Rating is Plex's numeric rating.
	Rating float64 `json:"rating"`
	// Thumb is the Plex poster path.
	Thumb string `json:"thumb"`
	// ViewCount is the Plex play count for a movie.
	ViewCount int `json:"viewCount"`
	// LeafCount is the number of episodes in a show.
	LeafCount int `json:"leafCount"`
	// ViewedLeafCount is the number of watched episodes in a show.
	ViewedLeafCount int `json:"viewedLeafCount"`
	// Type identifies the Plex media type.
	Type string `json:"type"`
	// AddedAt is the Plex added-at Unix timestamp.
	AddedAt int64 `json:"addedAt"`
	// Genres contains raw Plex genre tags.
	Genres []metadataGenre `json:"Genre"`
}

// metadataGenre models one Plex genre tag.
type metadataGenre struct {
	Tag string `json:"tag"`
}

// New creates a Plex client with a generated client identifier.
func New(rawURL, token string) (*Client, error) {
	return NewWithClientID(rawURL, token, "screendeck-go")
}

// NewWithClientID creates a Plex client with a fixed client identifier.
func NewWithClientID(rawURL, token, clientID string) (*Client, error) {
	if rawURL == "" || token == "" {
		return nil, ErrInvalidClientConfig
	}
	if clientID == "" {
		return nil, ErrInvalidClientID
	}
	u, err := parseHTTPURL(strings.TrimRight(rawURL, "/"))
	if err != nil {
		return nil, ErrInvalidServerURL
	}
	return &Client{baseURL: u, token: token, clientID: clientID, httpClient: &http.Client{Timeout: 20 * time.Second}}, nil
}

// Libraries returns the supported libraries exposed by Plex.
func (c *Client) Libraries(ctx context.Context) ([]media.Library, error) {
	var response librariesResponse
	if err := c.getJSON(ctx, "/library/sections", nil, &response); err != nil {
		return nil, err
	}
	libraries := make([]media.Library, 0, len(response.MediaContainer.Directories))
	for _, library := range response.MediaContainer.Directories {
		if !supportedLibraryType(library.Type) {
			continue
		}
		libraries = append(libraries, media.Library{Key: library.Key, Title: library.Title, Type: library.Type})
	}
	return libraries, nil
}

// Items returns media items in a Plex library.
func (c *Client) Items(ctx context.Context, library media.Library) ([]media.Item, error) {
	if !validLibrary(library) {
		return nil, ErrInvalidLibrary
	}

	mediaType := "1"
	if library.Type == "show" {
		mediaType = "2"
	}
	query := url.Values{
		"type":                   {mediaType},
		"X-Plex-Container-Start": {"0"},
		"X-Plex-Container-Size":  {"50000"},
	}

	var response itemsResponse
	if err := c.getJSON(ctx, "/library/sections/"+library.Key+"/all", query, &response); err != nil {
		return nil, err
	}

	items := make([]media.Item, 0, len(response.MediaContainer.Metadata))
	for _, metadata := range response.MediaContainer.Metadata {
		items = append(items, itemFromMetadata(library, metadata))
	}
	return items, nil
}

// itemFromMetadata converts raw Plex metadata into provider-neutral media metadata.
func itemFromMetadata(library media.Library, metadata metadataItem) media.Item {
	genres := make([]string, 0, len(metadata.Genres))
	for _, genre := range metadata.Genres {
		genres = append(genres, genre.Tag)
	}

	viewed := metadata.ViewCount > 0
	if library.Type == "show" {
		viewed = metadata.LeafCount > 0 && metadata.ViewedLeafCount >= metadata.LeafCount
	}

	itemType := metadata.Type
	if itemType == "" {
		itemType = library.Type
	}

	return media.Item{
		ID:         metadata.RatingKey,
		LibraryKey: library.Key,
		Type:       itemType,
		GUID:       metadata.GUID,
		Title:      metadata.Title,
		Year:       metadata.Year,
		Summary:    metadata.Summary,
		Duration:   metadata.Duration,
		Rating:     metadata.Rating,
		Poster:     metadata.Thumb,
		Genres:     genres,
		Viewed:     viewed,
		AddedAt:    metadata.AddedAt,
	}
}

// supportedLibraryType reports whether ScreenDeck supports a Plex library type.
func supportedLibraryType(libraryType string) bool {
	return libraryType == "movie" || libraryType == "show"
}

// validLibrary reports whether a Plex library can be requested safely.
func validLibrary(library media.Library) bool {
	return library.Key != "" && !strings.ContainsAny(library.Key, "/?") && supportedLibraryType(library.Type)
}

// validPosterPath reports whether a Plex poster path is safe to append to the server URL.
func validPosterPath(path string) bool {
	return strings.HasPrefix(path, "/") && !strings.Contains(path, "//")
}

// Poster retrieves a poster image from Plex.
func (c *Client) Poster(ctx context.Context, path string) (*http.Response, error) {
	if !validPosterPath(path) {
		return nil, ErrInvalidPosterPath
	}
	// Fetch the artwork directly with the token in a header. Plex's transcode
	// endpoint requires embedding the token in its source URL, which risks the
	// credential appearing in upstream errors or logs.
	return c.do(ctx, path, nil)
}

// getJSON retrieves and decodes a Plex JSON response.
func (c *Client) getJSON(ctx context.Context, path string, query url.Values, target any) error {
	response, err := c.do(ctx, path, query)
	if err != nil {
		return err
	}
	defer response.Body.Close() // nolint:errcheck
	if err := json.NewDecoder(io.LimitReader(response.Body, 64<<20)).Decode(target); err != nil {
		return fmt.Errorf("%w: %w", ErrServerDecode, err)
	}
	return nil
}

// do sends an authenticated request to Plex.
func (c *Client) do(ctx context.Context, path string, query url.Values) (*http.Response, error) {
	u := *c.baseURL
	u.Path = strings.TrimRight(u.Path, "/") + path
	if query != nil {
		u.RawQuery = query.Encode()
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("X-Plex-Token", c.token)
	req.Header.Set("X-Plex-Product", "ScreenDeck")
	req.Header.Set("X-Plex-Version", "1.0")
	req.Header.Set("X-Plex-Client-Identifier", c.clientID)
	response, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("%w: %s %s: %w", ErrServerContact, req.Method, u.Redacted(), err)
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		defer response.Body.Close()                                // nolint:errcheck
		body, _ := io.ReadAll(io.LimitReader(response.Body, 1024)) // nolint:errcheck
		return nil, fmt.Errorf("%w: %s %s: %s", ErrServerResponse, req.Method, response.Status, strconv.Quote(strings.TrimSpace(string(body))))
	}
	return response, nil
}

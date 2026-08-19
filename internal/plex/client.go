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
)

type Library struct {
	Key   string `json:"key"`
	Title string `json:"title"`
	Type  string `json:"type"`
}

type Item struct {
	RatingKey string   `json:"id"`
	Library   string   `json:"libraryKey"`
	Type      string   `json:"type"`
	GUID      string   `json:"guid"`
	Title     string   `json:"title"`
	Year      int      `json:"year"`
	Summary   string   `json:"summary"`
	Duration  int      `json:"duration"`
	Rating    float64  `json:"rating"`
	Thumb     string   `json:"-"`
	Genres    []string `json:"genres"`
	Viewed    bool     `json:"viewed"`
}

type Client struct {
	baseURL    *url.URL
	token      string
	clientID   string
	httpClient *http.Client
}

type librariesResponse struct {
	MediaContainer librariesContainer `json:"MediaContainer"`
}

type librariesContainer struct {
	Directories []Library `json:"Directory"`
}

type itemsResponse struct {
	MediaContainer itemsContainer `json:"MediaContainer"`
}

type itemsContainer struct {
	Metadata []metadataItem `json:"Metadata"`
}

type metadataItem struct {
	RatingKey       string          `json:"ratingKey"`
	GUID            string          `json:"guid"`
	Title           string          `json:"title"`
	Year            int             `json:"year"`
	Summary         string          `json:"summary"`
	Duration        int             `json:"duration"`
	Rating          float64         `json:"rating"`
	Thumb           string          `json:"thumb"`
	ViewCount       int             `json:"viewCount"`
	LeafCount       int             `json:"leafCount"`
	ViewedLeafCount int             `json:"viewedLeafCount"`
	Type            string          `json:"type"`
	Genres          []metadataGenre `json:"Genre"`
}

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
	u, err := url.Parse(strings.TrimRight(rawURL, "/"))
	if err != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") {
		return nil, ErrInvalidServerURL
	}
	return &Client{baseURL: u, token: token, clientID: clientID, httpClient: &http.Client{Timeout: 20 * time.Second}}, nil
}

// Libraries returns the supported libraries exposed by Plex.
func (c *Client) Libraries(ctx context.Context) ([]Library, error) {
	var response librariesResponse
	if err := c.getJSON(ctx, "/library/sections", nil, &response); err != nil {
		return nil, err
	}
	libraries := make([]Library, 0, len(response.MediaContainer.Directories))
	for _, library := range response.MediaContainer.Directories {
		if library.Type == "movie" || library.Type == "show" {
			libraries = append(libraries, library)
		}
	}
	return libraries, nil
}

// Items returns the media items in a Plex library.
func (c *Client) Items(ctx context.Context, library Library) ([]Item, error) {
	if library.Key == "" || strings.ContainsAny(library.Key, "/?") || (library.Type != "movie" && library.Type != "show") {
		return nil, ErrInvalidLibrary
	}
	mediaType := "1"
	if library.Type == "show" {
		mediaType = "2"
	}
	var response itemsResponse
	query := url.Values{"type": {mediaType}, "X-Plex-Container-Start": {"0"}, "X-Plex-Container-Size": {"50000"}}
	if err := c.getJSON(ctx, "/library/sections/"+library.Key+"/all", query, &response); err != nil {
		return nil, err
	}
	items := make([]Item, 0, len(response.MediaContainer.Metadata))
	for _, item := range response.MediaContainer.Metadata {
		genres := make([]string, 0, len(item.Genres))
		for _, genre := range item.Genres {
			genres = append(genres, genre.Tag)
		}
		viewed := item.ViewCount > 0
		if library.Type == "show" {
			viewed = item.LeafCount > 0 && item.ViewedLeafCount >= item.LeafCount
		}
		itemType := item.Type
		if itemType == "" {
			itemType = library.Type
		}
		items = append(items, Item{
			RatingKey: item.RatingKey, Library: library.Key, Type: itemType, GUID: item.GUID, Title: item.Title,
			Year: item.Year, Summary: item.Summary, Duration: item.Duration, Rating: item.Rating,
			Thumb: item.Thumb, Genres: genres, Viewed: viewed,
		})
	}
	return items, nil
}

// Poster retrieves a poster image from Plex.
func (c *Client) Poster(ctx context.Context, path string) (*http.Response, error) {
	if !strings.HasPrefix(path, "/") || strings.Contains(path, "//") {
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
	defer response.Body.Close()
	if err := json.NewDecoder(io.LimitReader(response.Body, 64<<20)).Decode(target); err != nil {
		return fmt.Errorf("%w: %w", ErrServerDecode, err)
	}
	return nil
}

// do sends an authenticated request to Plex.
func (c *Client) do(ctx context.Context, path string, query url.Values) (*http.Response, error) {
	u := *c.baseURL
	u.Path = strings.TrimRight(u.Path, "/") + path
	u.RawQuery = query.Encode()
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
		defer response.Body.Close()
		body, _ := io.ReadAll(io.LimitReader(response.Body, 1024))
		return nil, fmt.Errorf("%w: %s %s: %s", ErrServerResponse, req.Method, response.Status, strconv.Quote(strings.TrimSpace(string(body))))
	}
	return response, nil
}

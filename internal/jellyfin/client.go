package jellyfin

import (
	"bytes"
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/gi8lino/screendeck/internal/media"
)

const maxCatalogItems = 50_000

// Client performs authenticated read-only requests against a Jellyfin server.
type Client struct {
	// baseURL is the parsed Jellyfin server URL.
	baseURL *url.URL
	// accessToken authenticates requests to the Jellyfin server.
	accessToken string
	// userID identifies the configured Jellyfin user.
	userID string
	// deviceID identifies this ScreenDeck installation to Jellyfin.
	deviceID string
	// version identifies the ScreenDeck version sent to Jellyfin.
	version string
	// httpClient executes Jellyfin HTTP requests.
	httpClient *http.Client
}

// systemInfo contains public Jellyfin server identity fields.
type systemInfo struct {
	// ServerName is Jellyfin's friendly server name.
	ServerName string `json:"ServerName"`
	// ServerID is Jellyfin's stable server identifier.
	ServerID string `json:"Id"`
	// Version is the Jellyfin server version.
	Version string `json:"Version"`
}

// authenticationRequest is the Jellyfin username/password login request.
type authenticationRequest struct {
	// Username is the Jellyfin username.
	Username string `json:"Username"`
	// Password is the Jellyfin password used for this login request.
	Password string `json:"Pw"`
}

// authenticationResponse is the subset of Jellyfin authentication state ScreenDeck persists.
type authenticationResponse struct {
	// AccessToken is the token returned after successful authentication.
	AccessToken string `json:"AccessToken"`
	// ServerID identifies the server that issued the token.
	ServerID string `json:"ServerId"`
	// User contains the authenticated Jellyfin user.
	User jellyfinUser `json:"User"`
}

// jellyfinUser contains the authenticated Jellyfin user identity.
type jellyfinUser struct {
	// ID is the stable Jellyfin user identifier.
	ID string `json:"Id"`
	// Name is the Jellyfin user display name.
	Name string `json:"Name"`
}

// queryResult models Jellyfin endpoints that return a list of BaseItemDto objects.
type queryResult struct {
	// Items contains returned Jellyfin base items.
	Items []baseItem `json:"Items"`
	// TotalRecordCount is the total number of matching Jellyfin items.
	TotalRecordCount int `json:"TotalRecordCount"`
}

// baseItem contains Jellyfin metadata used by ScreenDeck.
type baseItem struct {
	// ID is Jellyfin's stable item identifier.
	ID string `json:"Id"`
	// Name is the display title.
	Name string `json:"Name"`
	// Type identifies the Jellyfin item type.
	Type string `json:"Type"`
	// CollectionType identifies the Jellyfin library collection type.
	CollectionType string `json:"CollectionType"`
	// ProductionYear is the release year.
	ProductionYear int `json:"ProductionYear"`
	// Overview is the item summary.
	Overview string `json:"Overview"`
	// RunTimeTicks is the Jellyfin duration in 100-nanosecond ticks.
	RunTimeTicks int64 `json:"RunTimeTicks"`
	// CommunityRating is Jellyfin's community rating.
	CommunityRating float64 `json:"CommunityRating"`
	// Genres contains Jellyfin genre names.
	Genres []string `json:"Genres"`
	// DateCreated is the provider added-at timestamp.
	DateCreated string `json:"DateCreated"`
	// ProviderIDs contains external provider identifiers.
	ProviderIDs map[string]string `json:"ProviderIds"`
	// ImageTags contains available Jellyfin image tags.
	ImageTags map[string]string `json:"ImageTags"`
	// UserData contains configured-user watch state.
	UserData userData `json:"UserData"`
}

// userData contains the watched state attached to a Jellyfin item for the configured user.
type userData struct {
	// Played reports whether the configured Jellyfin user watched the item.
	Played bool `json:"Played"`
}

// NewClient creates an authenticated Jellyfin catalog client.
func NewClient(
	rawURL string,
	accessToken string,
	userID string,
	deviceID string,
	version string,
) (*Client, error) {
	if accessToken == "" || userID == "" || deviceID == "" {
		return nil, ErrInvalidClientConfig
	}
	baseURL, err := parseHTTPURL(rawURL)
	if err != nil {
		return nil, ErrInvalidServerURL
	}
	version = cmp.Or(version, "dev")
	return &Client{
		baseURL:     baseURL,
		accessToken: accessToken,
		userID:      userID,
		deviceID:    deviceID,
		version:     version,
		httpClient:  &http.Client{Timeout: 20 * time.Second},
	}, nil
}

// Libraries returns movie and TV libraries visible to the configured Jellyfin user.
func (c *Client) Libraries(ctx context.Context) ([]media.Library, error) {
	var response queryResult
	path := "/Users/" + url.PathEscape(c.userID) + "/Views"
	if err := c.getJSON(ctx, path, nil, &response); err != nil {
		return nil, err
	}

	libraries := make([]media.Library, 0, len(response.Items))
	for _, item := range response.Items {
		libraryType := libraryType(item.CollectionType)
		if libraryType == "" || item.ID == "" || strings.TrimSpace(item.Name) == "" {
			continue
		}
		libraries = append(libraries, media.Library{
			Key:   item.ID,
			Title: item.Name,
			Type:  libraryType,
		})
	}
	return libraries, nil
}

// Items returns top-level movies or series from a Jellyfin library.
func (c *Client) Items(ctx context.Context, library media.Library) ([]media.Item, error) {
	if !validLibrary(library) {
		return nil, ErrInvalidLibrary
	}

	includeType := "Movie"
	if library.Type == "show" {
		includeType = "Series"
	}
	query := url.Values{
		"UserId":           {c.userID},
		"ParentId":         {library.Key},
		"Recursive":        {"true"},
		"IncludeItemTypes": {includeType},
		"Fields":           {"Overview,Genres,DateCreated,ProviderIds"},
		"EnableUserData":   {"true"},
		"EnableImages":     {"true"},
		"Limit":            {strconv.Itoa(maxCatalogItems)},
	}

	var response queryResult
	if err := c.getJSON(ctx, "/Items", query, &response); err != nil {
		return nil, err
	}

	items := make([]media.Item, 0, len(response.Items))
	for _, item := range response.Items {
		converted := itemFromBaseItem(library, item)
		if converted.ID == "" {
			continue
		}
		items = append(items, converted)
	}
	return items, nil
}

// Poster retrieves a Jellyfin primary image using an opaque item-id reference.
func (c *Client) Poster(ctx context.Context, reference string) (*http.Response, error) {
	if !validItemID(reference) {
		return nil, ErrInvalidPosterReference
	}
	query := url.Values{
		"MaxWidth": {"720"},
		"Quality":  {"90"},
	}
	return c.do(ctx, http.MethodGet, "/Items/"+url.PathEscape(reference)+"/Images/Primary", query, nil)
}

// PublicSystemInfo retrieves public identity information from a Jellyfin server.
func PublicSystemInfo(
	ctx context.Context,
	rawURL string,
	deviceID string,
	version string,
) (name, serverID string, err error) {
	baseURL, err := parseHTTPURL(rawURL)
	if err != nil {
		return "", "", ErrInvalidServerURL
	}
	client := &Client{
		baseURL:    baseURL,
		deviceID:   deviceID,
		version:    version,
		httpClient: &http.Client{Timeout: 20 * time.Second},
	}
	var info systemInfo
	if err := client.getJSON(ctx, "/System/Info/Public", nil, &info); err != nil {
		return "", "", err
	}
	return info.ServerName, info.ServerID, nil
}

// Authenticate verifies Jellyfin credentials and returns the access token and user identity.
func Authenticate(
	ctx context.Context,
	rawURL string,
	username string,
	password string,
	deviceID string,
	version string,
) (accessToken, userID, userName, serverID string, err error) {
	baseURL, err := parseHTTPURL(rawURL)
	if err != nil {
		return "", "", "", "", ErrInvalidServerURL
	}
	client := &Client{
		baseURL:    baseURL,
		deviceID:   deviceID,
		version:    version,
		httpClient: &http.Client{Timeout: 20 * time.Second},
	}

	body, err := json.Marshal(authenticationRequest{Username: username, Password: password})
	if err != nil {
		return "", "", "", "", err
	}
	response, err := client.do(ctx, http.MethodPost, "/Users/AuthenticateByName", nil, bytes.NewReader(body))
	if err != nil {
		if errorsIsStatus(err, http.StatusUnauthorized) {
			return "", "", "", "", ErrAuthenticationFailed
		}
		return "", "", "", "", err
	}
	defer response.Body.Close() // nolint:errcheck

	var auth authenticationResponse
	if err := json.NewDecoder(io.LimitReader(response.Body, 4<<20)).Decode(&auth); err != nil {
		return "", "", "", "", fmt.Errorf("%w: %w", ErrServerDecode, err)
	}
	if auth.AccessToken == "" || auth.User.ID == "" {
		return "", "", "", "", ErrAuthenticationFailed
	}
	return auth.AccessToken, auth.User.ID, auth.User.Name, auth.ServerID, nil
}

// httpStatusError records a non-success Jellyfin HTTP response without leaking its body.
type httpStatusError struct {
	// status is the upstream HTTP response status.
	status int
}

// Error returns the upstream HTTP status as an error message.
func (e httpStatusError) Error() string { return fmt.Sprintf("HTTP status %d", e.status) }

// errorsIsStatus reports whether an error contains the expected Jellyfin HTTP status.
func errorsIsStatus(err error, status int) bool {
	statusErr, ok := errors.AsType[httpStatusError](err)
	return ok && statusErr.status == status
}

// getJSON retrieves and decodes one Jellyfin JSON response.
func (c *Client) getJSON(ctx context.Context, path string, query url.Values, target any) error {
	response, err := c.do(ctx, http.MethodGet, path, query, nil)
	if err != nil {
		return err
	}
	defer response.Body.Close() // nolint:errcheck
	if err := json.NewDecoder(io.LimitReader(response.Body, 64<<20)).Decode(target); err != nil {
		return fmt.Errorf("%w: %w", ErrServerDecode, err)
	}
	return nil
}

// do performs one Jellyfin request with ScreenDeck client identity headers.
func (c *Client) do(
	ctx context.Context,
	method string,
	path string,
	query url.Values,
	body io.Reader,
) (*http.Response, error) {
	u := c.baseURL.JoinPath(path)

	if query != nil {
		u.RawQuery = query.Encode()
	}

	request, err := http.NewRequestWithContext(ctx, method, u.String(), body)
	if err != nil {
		return nil, err
	}
	request.Header.Set("Accept", "application/json")
	request.Header.Set("Authorization", authorizationHeader(c.deviceID, c.version, c.accessToken))
	if body != nil {
		request.Header.Set("Content-Type", "application/json")
	}

	response, err := c.httpClient.Do(request)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrServerContact, err)
	}
	if response.StatusCode >= 200 && response.StatusCode < 300 {
		return response, nil
	}
	response.Body.Close() // nolint:errcheck
	return nil, fmt.Errorf("%w: %w", ErrServerResponse, httpStatusError{status: response.StatusCode})
}

// authorizationHeader builds Jellyfin's MediaBrowser authorization header.
func authorizationHeader(deviceID, version, token string) string {
	fields := []string{
		`Client="ScreenDeck"`,
		`Device="ScreenDeck"`,
		`DeviceId="` + headerValue(deviceID) + `"`,
		`Version="` + headerValue(version) + `"`,
	}
	if token != "" {
		fields = append(fields, `Token="`+headerValue(token)+`"`)
	}
	return "MediaBrowser " + strings.Join(fields, ", ")
}

// headerValue escapes a value embedded in a quoted MediaBrowser authorization field.
func headerValue(value string) string {
	return strings.NewReplacer(`\`, `\\`, `"`, `\"`).Replace(value)
}

// parseHTTPURL parses an absolute Jellyfin HTTP or HTTPS URL and removes trailing slashes.
func parseHTTPURL(rawURL string) (*url.URL, error) {
	parsed, err := url.Parse(strings.TrimRight(strings.TrimSpace(rawURL), "/"))
	if err != nil || parsed.Host == "" || parsed.User != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		return nil, ErrInvalidServerURL
	}
	parsed.RawQuery = ""
	parsed.Fragment = ""
	return parsed, nil
}

// validLibrary reports whether a Jellyfin library can be queried safely.
func validLibrary(library media.Library) bool {
	return validItemID(library.Key) && (library.Type == "movie" || library.Type == "show")
}

// validItemID reports whether a provider item identifier is safe to place in one URL path segment.
func validItemID(value string) bool {
	value = strings.TrimSpace(value)
	if value == "" {
		return false
	}
	for _, character := range value {
		if (character >= 'a' && character <= 'z') ||
			(character >= 'A' && character <= 'Z') ||
			(character >= '0' && character <= '9') ||
			character == '-' || character == '_' {
			continue
		}
		return false
	}
	return true
}

// libraryType maps Jellyfin collection types to ScreenDeck media types.
func libraryType(collectionType string) string {
	switch strings.ToLower(strings.TrimSpace(collectionType)) {
	case "movies":
		return "movie"
	case "tvshows":
		return "show"
	default:
		return ""
	}
}

// itemFromBaseItem converts a Jellyfin BaseItemDto into provider-neutral metadata.
func itemFromBaseItem(library media.Library, item baseItem) media.Item {
	itemType := "movie"
	if strings.EqualFold(item.Type, "Series") || library.Type == "show" {
		itemType = "show"
	}
	addedAt := int64(0)
	if item.DateCreated != "" {
		if created, err := time.Parse(time.RFC3339Nano, item.DateCreated); err == nil {
			addedAt = created.Unix()
		}
	}

	poster := ""
	if item.ImageTags["Primary"] != "" {
		poster = item.ID
	}

	return media.Item{
		ID:         item.ID,
		LibraryKey: library.Key,
		Type:       itemType,
		GUID:       providerGUID(item.ProviderIDs, item.ID),
		Title:      item.Name,
		Year:       item.ProductionYear,
		Summary:    item.Overview,
		Duration:   int(item.RunTimeTicks / 10000),
		Rating:     item.CommunityRating,
		Poster:     poster,
		Genres:     slices.Clone(item.Genres),
		Viewed:     item.UserData.Played,
		AddedAt:    addedAt,
	}
}

// providerGUID returns a stable external identifier when Jellyfin provides one.
func providerGUID(ids map[string]string, itemID string) string {
	for _, wanted := range []string{"Imdb", "Tmdb", "Tvdb"} {
		for provider, rawValue := range ids {
			if !strings.EqualFold(provider, wanted) {
				continue
			}
			if value := strings.TrimSpace(rawValue); value != "" {
				return strings.ToLower(wanted) + "://" + value
			}
		}
	}
	if itemID == "" {
		return ""
	}
	return "jellyfin://" + itemID
}

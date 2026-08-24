package jellyfin

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gi8lino/screendeck/internal/media"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPublicSystemInfo(t *testing.T) {
	t.Parallel()

	t.Run("loads server identity", func(t *testing.T) {
		t.Parallel()

		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			assert.Equal(t, "/System/Info/Public", r.URL.Path)
			assert.Contains(t, r.Header.Get("Authorization"), `Client="ScreenDeck"`)
			assert.Contains(t, r.Header.Get("Authorization"), `DeviceId="device"`)
			assert.NotContains(t, r.Header.Get("Authorization"), "Token=")
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprint(w, `{"ServerName":"Home Jellyfin","Id":"server-1","Version":"10.11.0"}`) // nolint:errcheck
		}))
		defer server.Close()

		name, serverID, err := PublicSystemInfo(t.Context(), server.URL, "device", "test")
		require.NoError(t, err)
		assert.Equal(t, "Home Jellyfin", name)
		assert.Equal(t, "server-1", serverID)
	})
}

func TestAuthenticate(t *testing.T) {
	t.Parallel()

	t.Run("returns token and user identity", func(t *testing.T) {
		t.Parallel()

		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			assert.Equal(t, http.MethodPost, r.Method)
			assert.Equal(t, "/Users/AuthenticateByName", r.URL.Path)
			assert.Contains(t, r.Header.Get("Authorization"), `Client="ScreenDeck"`)
			assert.NotContains(t, r.Header.Get("Authorization"), "Token=")

			var input authenticationRequest
			require.NoError(t, json.NewDecoder(r.Body).Decode(&input))
			assert.Equal(t, "alice", input.Username)
			assert.Equal(t, "secret", input.Password)

			w.Header().Set("Content-Type", "application/json")
			fmt.Fprint(w, `{"AccessToken":"access-token","ServerId":"server-1","User":{"Id":"user-1","Name":"Alice"}}`) // nolint:errcheck
		}))
		defer server.Close()

		token, userID, userName, serverID, err := Authenticate(
			t.Context(),
			server.URL,
			"alice",
			"secret",
			"device",
			"test",
		)
		require.NoError(t, err)
		assert.Equal(t, "access-token", token)
		assert.Equal(t, "user-1", userID)
		assert.Equal(t, "Alice", userName)
		assert.Equal(t, "server-1", serverID)
	})

	t.Run("maps unauthorized response", func(t *testing.T) {
		t.Parallel()

		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
		}))
		defer server.Close()

		_, _, _, _, err := Authenticate(t.Context(), server.URL, "alice", "bad", "device", "test")
		assert.ErrorIs(t, err, ErrAuthenticationFailed)
	})
}

func TestClientLibraries(t *testing.T) {
	t.Parallel()

	t.Run("loads supported libraries", func(t *testing.T) {
		t.Parallel()

		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			assert.Equal(t, "/Users/user-1/Views", r.URL.Path)
			assert.Contains(t, r.Header.Get("Authorization"), `Token="access-token"`)
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprint(w, `{"Items":[{"Id":"movies","Name":"Films","CollectionType":"movies"},{"Id":"shows","Name":"TV","CollectionType":"tvshows"},{"Id":"music","Name":"Music","CollectionType":"music"}]}`) // nolint:errcheck
		}))
		defer server.Close()

		client, err := NewClient(server.URL, "access-token", "user-1", "device", "test")
		require.NoError(t, err)
		libraries, err := client.Libraries(t.Context())
		require.NoError(t, err)
		require.Len(t, libraries, 2)
		assert.Equal(t, media.Library{Key: "movies", Title: "Films", Type: "movie"}, libraries[0])
		assert.Equal(t, media.Library{Key: "shows", Title: "TV", Type: "show"}, libraries[1])
	})
}

func TestClientItems(t *testing.T) {
	t.Parallel()

	t.Run("maps movie metadata", func(t *testing.T) {
		t.Parallel()

		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			assert.Equal(t, "/Items", r.URL.Path)
			assert.Equal(t, "user-1", r.URL.Query().Get("UserId"))
			assert.Equal(t, "movies", r.URL.Query().Get("ParentId"))
			assert.Equal(t, "Movie", r.URL.Query().Get("IncludeItemTypes"))
			assert.Equal(t, "true", r.URL.Query().Get("EnableUserData"))
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprint(w, `{"Items":[{"Id":"item-1","Name":"Arrival","Type":"Movie","ProductionYear":2016,"Overview":"First contact.","RunTimeTicks":69600000000,"CommunityRating":7.9,"Genres":["Drama","Science Fiction"],"DateCreated":"2024-01-02T03:04:05Z","ProviderIds":{"Imdb":"tt2543164"},"ImageTags":{"Primary":"tag"},"UserData":{"Played":true}}]}`) // nolint:errcheck
		}))
		defer server.Close()

		client, err := NewClient(server.URL, "access-token", "user-1", "device", "test")
		require.NoError(t, err)
		items, err := client.Items(t.Context(), media.Library{Key: "movies", Title: "Films", Type: "movie"})
		require.NoError(t, err)
		require.Len(t, items, 1)
		item := items[0]
		assert.Equal(t, "item-1", item.ID)
		assert.Equal(t, "movies", item.LibraryKey)
		assert.Equal(t, "movie", item.Type)
		assert.Equal(t, "imdb://tt2543164", item.GUID)
		assert.Equal(t, "Arrival", item.Title)
		assert.Equal(t, 6_960_000, item.Duration)
		assert.Equal(t, "item-1", item.Poster)
		assert.True(t, item.Viewed)
		assert.NotZero(t, item.AddedAt)
	})

	t.Run("queries series for show library", func(t *testing.T) {
		t.Parallel()

		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			assert.Equal(t, "Series", r.URL.Query().Get("IncludeItemTypes"))
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprint(w, `{"Items":[{"Id":"series-1","Name":"Severance","Type":"Series"}]}`) // nolint:errcheck
		}))
		defer server.Close()

		client, err := NewClient(server.URL, "access-token", "user-1", "device", "test")
		require.NoError(t, err)
		items, err := client.Items(t.Context(), media.Library{Key: "shows", Type: "show"})
		require.NoError(t, err)
		require.Len(t, items, 1)
		assert.Equal(t, "show", items[0].Type)
		assert.Equal(t, "jellyfin://series-1", items[0].GUID)
	})

	t.Run("rejects invalid library", func(t *testing.T) {
		t.Parallel()

		client, err := NewClient("http://jellyfin.test", "access-token", "user-1", "device", "test")
		require.NoError(t, err)
		_, err = client.Items(t.Context(), media.Library{Key: "../movies", Type: "movie"})
		assert.ErrorIs(t, err, ErrInvalidLibrary)
	})
}

func TestClientPoster(t *testing.T) {
	t.Parallel()

	t.Run("loads primary image", func(t *testing.T) {
		t.Parallel()

		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			assert.Equal(t, "/Items/item-1/Images/Primary", r.URL.Path)
			assert.Equal(t, "720", r.URL.Query().Get("MaxWidth"))
			assert.Equal(t, "90", r.URL.Query().Get("Quality"))
			assert.Contains(t, r.Header.Get("Authorization"), `Token="access-token"`)
			w.Header().Set("Content-Type", "image/jpeg")
			fmt.Fprint(w, "poster") // nolint:errcheck
		}))
		defer server.Close()

		client, err := NewClient(server.URL, "access-token", "user-1", "device", "test")
		require.NoError(t, err)
		response, err := client.Poster(t.Context(), "item-1")
		require.NoError(t, err)
		defer response.Body.Close() // nolint:errcheck
		body, err := io.ReadAll(response.Body)
		require.NoError(t, err)
		assert.Equal(t, "poster", string(body))
	})

	t.Run("rejects invalid reference", func(t *testing.T) {
		t.Parallel()

		client, err := NewClient("http://jellyfin.test", "access-token", "user-1", "device", "test")
		require.NoError(t, err)
		_, err = client.Poster(t.Context(), "../poster")
		assert.ErrorIs(t, err, ErrInvalidPosterReference)
	})
}

func TestAuthorizationHeader(t *testing.T) {
	t.Parallel()

	t.Run("includes client identity and token", func(t *testing.T) {
		t.Parallel()

		header := authorizationHeader(`device"id`, "v1", "token")
		assert.True(t, strings.HasPrefix(header, "MediaBrowser "))
		assert.Contains(t, header, `Client="ScreenDeck"`)
		assert.Contains(t, header, `DeviceId="device\"id"`)
		assert.Contains(t, header, `Version="v1"`)
		assert.Contains(t, header, `Token="token"`)
	})
}

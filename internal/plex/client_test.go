package plex

import (
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestLibraries verifies supported Plex libraries are loaded with authenticated requests.
func TestLibraries(t *testing.T) {
	t.Run("loads supported libraries", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			assert.Equal(t, "token", r.Header.Get("X-Plex-Token"))
			assert.Equal(t, "screendeck-go", r.Header.Get("X-Plex-Client-Identifier"))
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprint(w, `{"MediaContainer":{"Directory":[{"key":"1","title":"Films","type":"movie"},{"key":"2","title":"TV","type":"show"},{"key":"3","title":"Music","type":"artist"}]}}`) // nolint:errcheck
		}))
		defer server.Close()

		client, err := New(server.URL, "token")
		require.NoError(t, err)
		libraries, err := client.Libraries(t.Context())
		require.NoError(t, err)
		require.Len(t, libraries, 2)
		assert.Equal(t, "Films", libraries[0].Title)
		assert.Equal(t, "show", libraries[1].Type)
	})
}

// TestItems verifies movie and show metadata conversion and library validation.
func TestItems(t *testing.T) {
	t.Run("loads movie metadata", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			assert.Equal(t, "/library/sections/1/all", r.URL.Path)
			assert.Equal(t, "1", r.URL.Query().Get("type"))
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprint(w, `{"MediaContainer":{"Metadata":[{"ratingKey":"42","guid":"plex://movie/42","title":"Arrival","year":2016,"summary":"First contact.","duration":6960000,"rating":7.9,"thumb":"/poster/42","viewCount":1,"addedAt":1700000000,"Genre":[{"tag":"Science Fiction"}]}]}}`) // nolint:errcheck
		}))
		defer server.Close()

		client, err := New(server.URL, "token")
		require.NoError(t, err)
		items, err := client.Items(t.Context(), Library{Key: "1", Title: "Films", Type: "movie"})
		require.NoError(t, err)
		require.Len(t, items, 1)
		assert.Equal(t, "Arrival", items[0].Title)
		assert.True(t, items[0].Viewed)
		assert.Equal(t, []string{"Science Fiction"}, items[0].Genres)
		assert.Equal(t, int64(1700000000), items[0].AddedAt)
	})

	t.Run("loads show metadata", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			assert.Equal(t, "/library/sections/2/all", r.URL.Path)
			assert.Equal(t, "2", r.URL.Query().Get("type"))
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprint(w, `{"MediaContainer":{"Metadata":[{"ratingKey":"84","type":"show","guid":"plex://show/84","title":"Severance","year":2022,"thumb":"/poster/84","leafCount":19,"viewedLeafCount":19,"addedAt":1700000100,"Genre":[{"tag":"Drama"}]}]}}`) // nolint:errcheck
		}))
		defer server.Close()

		client, err := New(server.URL, "token")
		require.NoError(t, err)
		items, err := client.Items(t.Context(), Library{Key: "2", Title: "TV", Type: "show"})
		require.NoError(t, err)
		require.Len(t, items, 1)
		assert.Equal(t, "show", items[0].Type)
		assert.True(t, items[0].Viewed)
		assert.Equal(t, int64(1700000100), items[0].AddedAt)
	})

	t.Run("rejects unsafe library key", func(t *testing.T) {
		client, err := New("http://plex.test", "token")
		require.NoError(t, err)

		_, err = client.Items(t.Context(), Library{Key: "../secret", Type: "movie"})
		require.ErrorIs(t, err, ErrInvalidLibrary)
	})

	t.Run("rejects unsupported library type", func(t *testing.T) {
		client, err := New("http://plex.test", "token")
		require.NoError(t, err)

		_, err = client.Items(t.Context(), Library{Key: "1", Type: "artist"})
		require.ErrorIs(t, err, ErrInvalidLibrary)
	})
}

// TestPoster verifies poster requests and path validation.
func TestPoster(t *testing.T) {
	t.Run("uses authenticated Plex headers", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			assert.Equal(t, "/poster/42", r.URL.Path)
			assert.Equal(t, "token", r.Header.Get("X-Plex-Token"))
			assert.Equal(t, "screendeck-go", r.Header.Get("X-Plex-Client-Identifier"))
			w.Header().Set("Content-Type", "image/jpeg")
			w.WriteHeader(http.StatusOK)
			w.Write([]byte("poster")) // nolint:errcheck
		}))
		defer server.Close()

		client, err := New(server.URL, "token")
		require.NoError(t, err)
		response, err := client.Poster(t.Context(), "/poster/42")
		require.NoError(t, err)
		defer response.Body.Close() // nolint:errcheck

		body, err := io.ReadAll(response.Body)
		require.NoError(t, err)
		assert.Equal(t, "poster", string(body))
	})

	t.Run("rejects relative path", func(t *testing.T) {
		client, err := New("http://plex.test", "token")
		require.NoError(t, err)

		_, err = client.Poster(t.Context(), "poster/42")
		require.ErrorIs(t, err, ErrInvalidPosterPath)
	})

	t.Run("rejects protocol relative path", func(t *testing.T) {
		client, err := New("http://plex.test", "token")
		require.NoError(t, err)

		_, err = client.Poster(t.Context(), "//example.test/poster")
		require.ErrorIs(t, err, ErrInvalidPosterPath)
	})
}

// TestItemFromMetadata verifies raw Plex metadata normalization.
func TestItemFromMetadata(t *testing.T) {
	t.Run("movie", func(t *testing.T) {
		item := itemFromMetadata(
			Library{Key: "1", Type: "movie"},
			metadataItem{
				RatingKey: "42",
				GUID:      "plex://movie/42",
				Title:     "Arrival",
				Year:      2016,
				Summary:   "First contact.",
				Duration:  6_960_000,
				Rating:    7.9,
				Thumb:     "/poster/42",
				ViewCount: 1,
				AddedAt:   1_700_000_000,
				Genres:    []metadataGenre{{Tag: "Drama"}, {Tag: "Science Fiction"}},
			},
		)

		assert.Equal(t, "42", item.RatingKey)
		assert.Equal(t, "1", item.Library)
		assert.Equal(t, "movie", item.Type)
		assert.True(t, item.Viewed)
		assert.Equal(t, []string{"Drama", "Science Fiction"}, item.Genres)
	})

	t.Run("partially watched show", func(t *testing.T) {
		item := itemFromMetadata(
			Library{Key: "2", Type: "show"},
			metadataItem{
				RatingKey:       "84",
				Title:           "Severance",
				Type:            "show",
				LeafCount:       10,
				ViewedLeafCount: 9,
			},
		)

		assert.Equal(t, "show", item.Type)
		assert.False(t, item.Viewed)
	})

	t.Run("fully watched show", func(t *testing.T) {
		item := itemFromMetadata(
			Library{Key: "2", Type: "show"},
			metadataItem{
				RatingKey:       "84",
				Title:           "Severance",
				LeafCount:       10,
				ViewedLeafCount: 10,
			},
		)

		assert.Equal(t, "show", item.Type)
		assert.True(t, item.Viewed)
	})
}

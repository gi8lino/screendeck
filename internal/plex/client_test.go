package plex

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestClientLoadsLibrariesAndMovies verifies catalog loading from Plex.
func TestClientLoadsLibrariesAndMovies(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Plex-Token") != "token" {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/library/sections":
			fmt.Fprint(w, `{"MediaContainer":{"Directory":[{"key":"1","title":"Films","type":"movie"},{"key":"2","title":"TV","type":"show"}]}}`) // nolint:errcheck
		case "/library/sections/1/all":
			fmt.Fprint(w, `{"MediaContainer":{"Metadata":[{"ratingKey":"42","guid":"plex://movie/42","title":"Arrival","year":2016,"summary":"First contact.","duration":6960000,"rating":7.9,"thumb":"/poster/42","viewCount":1,"addedAt":1700000000,"Genre":[{"tag":"Science Fiction"}]}]}}`) // nolint:errcheck
		case "/library/sections/2/all":
			fmt.Fprint(w, `{"MediaContainer":{"Metadata":[{"ratingKey":"84","type":"show","guid":"plex://show/84","title":"Severance","year":2022,"thumb":"/poster/84","leafCount":19,"viewedLeafCount":19,"addedAt":1700000100,"Genre":[{"tag":"Drama"}]}]}}`) // nolint:errcheck
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	client, err := New(server.URL, "token")
	require.NoError(t, err)
	libraries, err := client.Libraries(context.Background())
	require.NoError(t, err)
	require.Len(t, libraries, 2)
	assert.Equal(t, "Films", libraries[0].Title)
	assert.Equal(t, "show", libraries[1].Type)

	movies, err := client.Items(context.Background(), libraries[0])
	require.NoError(t, err)
	require.Len(t, movies, 1)
	assert.Equal(t, "Arrival", movies[0].Title)
	assert.True(t, movies[0].Viewed)
	assert.Len(t, movies[0].Genres, 1)
	assert.Equal(t, int64(1700000000), movies[0].AddedAt)

	shows, err := client.Items(context.Background(), libraries[1])
	require.NoError(t, err)
	require.Len(t, shows, 1)
	assert.Equal(t, "show", shows[0].Type)
	assert.True(t, shows[0].Viewed)
	assert.Equal(t, int64(1700000100), shows[0].AddedAt)
}

// TestClientRejectsInvalidLibrary verifies unsafe library definitions are rejected.
func TestClientRejectsInvalidLibrary(t *testing.T) {
	client, err := New("http://plex.test", "token")
	require.NoError(t, err)

	_, err = client.Items(context.Background(), Library{Key: "../secret", Type: "movie"})
	require.ErrorIs(t, err, ErrInvalidLibrary)

	_, err = client.Items(context.Background(), Library{Key: "1", Type: "artist"})
	require.ErrorIs(t, err, ErrInvalidLibrary)
}

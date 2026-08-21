package plex

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestPoster verifies poster requests use authenticated Plex headers and reject unsafe paths.
func TestPoster(t *testing.T) {
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

	response, err := client.Poster(context.Background(), "/poster/42")
	require.NoError(t, err)
	defer response.Body.Close() // nolint:errcheck

	body, err := io.ReadAll(response.Body)
	require.NoError(t, err)
	assert.Equal(t, "poster", string(body))

	_, err = client.Poster(context.Background(), "poster/42")
	require.ErrorIs(t, err, ErrInvalidPosterPath)

	_, err = client.Poster(context.Background(), "//example.test/poster")
	require.ErrorIs(t, err, ErrInvalidPosterPath)
}

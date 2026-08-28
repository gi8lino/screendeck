package handler

import (
	"bytes"
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gi8lino/screendeck/internal/media"
	"github.com/gi8lino/screendeck/internal/room"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type fakeLibraryReader struct {
	libraries []media.Library
	err       error
}

func (f fakeLibraryReader) Libraries(context.Context) ([]media.Library, error) {
	return f.libraries, f.err
}

type fakeCatalogOptionsReader struct {
	libraryKeys []string
	options     room.CatalogOptions
	err         error
}

func (f *fakeCatalogOptionsReader) Options(_ context.Context, libraryKeys []string) (room.CatalogOptions, error) {
	f.libraryKeys = append([]string(nil), libraryKeys...)
	return f.options, f.err
}

// TestLibraries verifies library responses are sorted and service errors are mapped.
func TestLibraries(t *testing.T) {
	t.Parallel()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	t.Run("sorts libraries", func(t *testing.T) {
		t.Parallel()
		response := httptest.NewRecorder()
		request := httptest.NewRequest(http.MethodGet, "/api/libraries", nil)

		Libraries(fakeLibraryReader{libraries: []media.Library{
			{Key: "shows", Title: "Zulu", Type: "show"},
			{Key: "movies-b", Title: "Beta", Type: "movie"},
			{Key: "movies-a", Title: "Alpha", Type: "movie"},
		}}, logger).ServeHTTP(response, request)

		assert.Equal(t, http.StatusOK, response.Code)
		assert.JSONEq(t, `[
			{"key":"movies-a","title":"Alpha","type":"movie"},
			{"key":"movies-b","title":"Beta","type":"movie"},
			{"key":"shows","title":"Zulu","type":"show"}
		]`, response.Body.String())
	})

	t.Run("maps service error", func(t *testing.T) {
		t.Parallel()
		response := httptest.NewRecorder()
		request := httptest.NewRequest(http.MethodGet, "/api/libraries", nil)

		Libraries(fakeLibraryReader{err: media.ErrNotConfigured}, logger).ServeHTTP(response, request)

		assert.Equal(t, http.StatusServiceUnavailable, response.Code)
		assert.JSONEq(t, `{"error":"media server is not configured"}`, response.Body.String())
	})
}

// TestCatalogOptions verifies selected library keys are decoded and forwarded.
func TestCatalogOptions(t *testing.T) {
	t.Parallel()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	t.Run("returns options", func(t *testing.T) {
		t.Parallel()
		rooms := &fakeCatalogOptionsReader{options: room.CatalogOptions{
			Genres:  []string{"Comedy", "Drama"},
			MinYear: 1999,
			MaxYear: 2026,
		}}
		response := httptest.NewRecorder()
		request := httptest.NewRequest(
			http.MethodPost,
			"/api/catalog/options",
			bytes.NewBufferString(`{"libraryKeys":["movies","shows"]}`),
		)

		CatalogOptions(rooms, logger).ServeHTTP(response, request)

		assert.Equal(t, http.StatusOK, response.Code)
		assert.Equal(t, []string{"movies", "shows"}, rooms.libraryKeys)
		assert.JSONEq(t, `{"genres":["Comedy","Drama"],"minYear":1999,"maxYear":2026}`, response.Body.String())
	})

	t.Run("rejects malformed JSON", func(t *testing.T) {
		t.Parallel()
		rooms := &fakeCatalogOptionsReader{}
		response := httptest.NewRecorder()
		request := httptest.NewRequest(
			http.MethodPost,
			"/api/catalog/options",
			bytes.NewBufferString(`{"libraryKeys":`),
		)

		CatalogOptions(rooms, logger).ServeHTTP(response, request)

		assert.Equal(t, http.StatusBadRequest, response.Code)
		assert.Nil(t, rooms.libraryKeys)
	})

	t.Run("maps service error", func(t *testing.T) {
		t.Parallel()
		rooms := &fakeCatalogOptionsReader{err: room.ErrNotFound}
		response := httptest.NewRecorder()
		request := httptest.NewRequest(
			http.MethodPost,
			"/api/catalog/options",
			bytes.NewBufferString(`{"libraryKeys":["missing"]}`),
		)

		CatalogOptions(rooms, logger).ServeHTTP(response, request)

		require.Equal(t, http.StatusNotFound, response.Code)
		assert.JSONEq(t, `{"error":"not found"}`, response.Body.String())
	})
}

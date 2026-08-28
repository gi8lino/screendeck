package handler

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gi8lino/screendeck/internal/room"
	"github.com/stretchr/testify/assert"
)

type fakePosterReader struct {
	itemID   string
	response *room.PosterResponse
	err      error
}

func (f *fakePosterReader) Poster(_ context.Context, itemID string) (*room.PosterResponse, error) {
	f.itemID = itemID
	return f.response, f.err
}

// TestPoster verifies poster proxy headers, content, and error handling.
func TestPoster(t *testing.T) {
	t.Parallel()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	t.Run("preserves image content type", func(t *testing.T) {
		t.Parallel()
		rooms := &fakePosterReader{response: &room.PosterResponse{
			Body:   io.NopCloser(strings.NewReader("poster-data")),
			Header: http.Header{"Content-Type": []string{"image/png"}},
		}}
		request := httptest.NewRequest(http.MethodGet, "/api/posters/item-1", nil)
		request.SetPathValue("itemID", "item-1")
		response := httptest.NewRecorder()

		Poster(rooms, logger).ServeHTTP(response, request)

		assert.Equal(t, http.StatusOK, response.Code)
		assert.Equal(t, "item-1", rooms.itemID)
		assert.Equal(t, "image/png", response.Header().Get("Content-Type"))
		assert.Equal(t, "private, max-age=86400", response.Header().Get("Cache-Control"))
		assert.Equal(t, "poster-data", response.Body.String())
	})

	t.Run("falls back to JPEG content type", func(t *testing.T) {
		t.Parallel()
		rooms := &fakePosterReader{response: &room.PosterResponse{
			Body:   io.NopCloser(strings.NewReader("poster-data")),
			Header: http.Header{"Content-Type": []string{"application/octet-stream"}},
		}}
		request := httptest.NewRequest(http.MethodGet, "/api/posters/item-1", nil)
		request.SetPathValue("itemID", "item-1")
		response := httptest.NewRecorder()

		Poster(rooms, logger).ServeHTTP(response, request)

		assert.Equal(t, "image/jpeg", response.Header().Get("Content-Type"))
	})

	t.Run("maps service error", func(t *testing.T) {
		t.Parallel()
		rooms := &fakePosterReader{err: room.ErrNotFound}
		request := httptest.NewRequest(http.MethodGet, "/api/posters/missing", nil)
		request.SetPathValue("itemID", "missing")
		response := httptest.NewRecorder()

		Poster(rooms, logger).ServeHTTP(response, request)

		assert.Equal(t, http.StatusNotFound, response.Code)
		assert.JSONEq(t, `{"error":"not found"}`, response.Body.String())
	})
}

package room

import (
	"context"
	"io"
	"net/http"

	"github.com/gi8lino/screendeck/internal/media"
)

// Poster retrieves the poster associated with a stored media item.
func (s *Service) Poster(
	ctx context.Context,
	itemID string,
) (*PosterResponse, error) {
	if s.catalog == nil {
		return nil, media.ErrNotConfigured
	}

	path, err := s.store.ItemPoster(ctx, itemID)
	if err != nil {
		return nil, err
	}

	response, err := s.catalog.Poster(ctx, path)
	if err != nil {
		return nil, err
	}

	return &PosterResponse{
		Body:   response.Body,
		Header: response.Header,
	}, nil
}

// PosterResponse exposes only the poster response fields required by handlers.
type PosterResponse struct {
	// Body is the proxied poster response body.
	Body io.ReadCloser
	// Header contains poster response headers from the active media provider.
	Header http.Header
}

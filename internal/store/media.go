package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/gi8lino/screendeck/internal/media"
)

var _ media.ProviderStore = (*Store)(nil) // Ensure Store implements media provider persistence.

// LoadMediaProvider returns the active media provider.
func (s *Store) LoadMediaProvider(ctx context.Context) (media.ProviderID, error) {
	const query = `SELECT provider FROM media_provider WHERE id = 1`
	var provider media.ProviderID
	if err := s.db.QueryRowContext(ctx, query).Scan(&provider); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return "", media.ErrProviderNotFound
		}
		return "", err
	}
	if provider != media.ProviderPlex && provider != media.ProviderJellyfin {
		return "", media.ErrUnknownProvider
	}
	return provider, nil
}

// SaveMediaProvider persists the active media provider singleton.
func (s *Store) SaveMediaProvider(ctx context.Context, provider media.ProviderID) error {
	if provider != media.ProviderPlex && provider != media.ProviderJellyfin {
		return media.ErrUnknownProvider
	}
	const query = `
INSERT INTO media_provider (
  id,
  provider,
  updated_at
) VALUES (
  1, ?, ?
)
ON CONFLICT (id) DO UPDATE SET
  provider = excluded.provider,
  updated_at = excluded.updated_at
`
	if _, err := s.db.ExecContext(ctx, query, provider, time.Now().Unix()); err != nil {
		return fmt.Errorf("save media provider: %w", err)
	}
	return nil
}

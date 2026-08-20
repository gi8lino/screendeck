package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/gi8lino/screendeck/internal/plex"
)

// SaveLibrary replaces cached metadata for a Plex library.
func (s *Store) SaveLibrary(ctx context.Context, library plex.Library, items []plex.Item) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback() // nolint:errcheck

	const saveLibraryQuery = `
INSERT INTO libraries (
  key,
  title,
  synced_at
) VALUES (
  ?, ?, ?
)
ON CONFLICT (key) DO UPDATE SET
  title = excluded.title,
  synced_at = excluded.synced_at
`
	if _, err := tx.ExecContext(ctx, saveLibraryQuery, library.Key, library.Title, time.Now().Unix()); err != nil {
		return fmt.Errorf("save library: %w", err)
	}

	const saveItemQuery = `
INSERT INTO media_items (
  rating_key,
  library_key,
  media_type,
  guid,
  title,
  year,
  summary,
  duration,
  rating,
  thumb,
  genres,
  viewed,
  added_at
) VALUES (
  ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?
)
ON CONFLICT (rating_key) DO UPDATE SET
  library_key = excluded.library_key,
  media_type = excluded.media_type,
  guid = excluded.guid,
  title = excluded.title,
  year = excluded.year,
  summary = excluded.summary,
  duration = excluded.duration,
  rating = excluded.rating,
  thumb = excluded.thumb,
  genres = excluded.genres,
  viewed = excluded.viewed,
  added_at = excluded.added_at
`
	stmt, err := tx.PrepareContext(ctx, saveItemQuery)
	if err != nil {
		return err
	}
	defer stmt.Close() // nolint:errcheck

	for _, item := range items {
		genres, err := json.Marshal(item.Genres)
		if err != nil {
			return fmt.Errorf("encode genres for item %q: %w", item.Title, err)
		}
		if _, err := stmt.ExecContext(
			ctx,
			item.RatingKey,
			item.Library,
			item.Type,
			item.GUID,
			item.Title,
			item.Year,
			item.Summary,
			item.Duration,
			item.Rating,
			item.Thumb,
			string(genres),
			item.Viewed,
			item.AddedAt,
		); err != nil {
			return fmt.Errorf("save item %q: %w", item.Title, err)
		}
	}
	return tx.Commit()
}

// ItemPoster returns the poster path for a stored media item.
func (s *Store) ItemPoster(ctx context.Context, ratingKey string) (string, error) {
	const query = `
SELECT thumb
FROM media_items
WHERE rating_key = ?
`
	var thumb string
	if err := s.db.QueryRowContext(ctx, query, ratingKey).Scan(&thumb); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return "", ErrNotFound
		}
		return "", err
	}
	if strings.TrimSpace(thumb) == "" {
		return "", ErrNotFound
	}
	return thumb, nil
}

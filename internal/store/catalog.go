package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/gi8lino/screendeck/internal/media"
	roomdomain "github.com/gi8lino/screendeck/internal/room"
)

// SaveLibrary replaces cached metadata for a media library.
func (s *Store) SaveLibrary(ctx context.Context, library media.Library, items []media.Item) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback() // nolint:errcheck

	if err := saveLibraryTx(ctx, tx, library); err != nil {
		return err
	}
	if err := saveLibraryItemsTx(ctx, tx, items); err != nil {
		return err
	}

	return tx.Commit()
}

// saveLibraryTx persists the library metadata inside an existing transaction.
func saveLibraryTx(ctx context.Context, tx *sql.Tx, library media.Library) error {
	const query = `
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
	if _, err := tx.ExecContext(ctx, query, library.Key, library.Title, time.Now().Unix()); err != nil {
		return fmt.Errorf("save library: %w", err)
	}
	return nil
}

// saveLibraryItemsTx persists all media items inside an existing transaction.
func saveLibraryItemsTx(ctx context.Context, tx *sql.Tx, items []media.Item) error {
	const query = `
INSERT INTO media_items (
  id,
  library_key,
  media_type,
  guid,
  title,
  year,
  summary,
  duration,
  rating,
  poster,
  genres,
  viewed,
  added_at
) VALUES (
  ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?
)
ON CONFLICT (id) DO UPDATE SET
  library_key = excluded.library_key,
  media_type = excluded.media_type,
  guid = excluded.guid,
  title = excluded.title,
  year = excluded.year,
  summary = excluded.summary,
  duration = excluded.duration,
  rating = excluded.rating,
  poster = excluded.poster,
  genres = excluded.genres,
  viewed = excluded.viewed,
  added_at = excluded.added_at
`
	stmt, err := tx.PrepareContext(ctx, query)
	if err != nil {
		return err
	}
	defer stmt.Close() // nolint:errcheck

	for _, item := range items {
		if err := saveLibraryItem(ctx, stmt, item); err != nil {
			return err
		}
	}

	return nil
}

// saveLibraryItem persists one media item using a prepared statement.
func saveLibraryItem(ctx context.Context, stmt *sql.Stmt, item media.Item) error {
	genres, err := json.Marshal(item.Genres)
	if err != nil {
		return fmt.Errorf("encode genres for item %q: %w", item.Title, err)
	}

	if _, err := stmt.ExecContext(
		ctx,
		item.ID,
		item.LibraryKey,
		item.Type,
		item.GUID,
		item.Title,
		item.Year,
		item.Summary,
		item.Duration,
		item.Rating,
		item.Poster,
		string(genres),
		item.Viewed,
		item.AddedAt,
	); err != nil {
		return fmt.Errorf("save item %q: %w", item.Title, err)
	}

	return nil
}

// ItemPoster returns the poster path for a stored media item.
func (s *Store) ItemPoster(ctx context.Context, itemID string) (string, error) {
	const query = `
SELECT poster
FROM media_items
WHERE id = ?
`
	var poster string
	if err := s.db.QueryRowContext(ctx, query, itemID).Scan(&poster); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return "", roomdomain.ErrNotFound
		}
		return "", err
	}
	if strings.TrimSpace(poster) == "" {
		return "", roomdomain.ErrNotFound
	}

	return poster, nil
}

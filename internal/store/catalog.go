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

func (s *Store) SaveLibrary(ctx context.Context, library plex.Library, items []plex.Item) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `INSERT INTO libraries(key,title,synced_at) VALUES(?,?,?)
ON CONFLICT(key) DO UPDATE SET title=excluded.title,synced_at=excluded.synced_at`, library.Key, library.Title, time.Now().Unix()); err != nil {
		return fmt.Errorf("save library: %w", err)
	}
	stmt, err := tx.PrepareContext(ctx, `INSERT INTO media_items
(rating_key,library_key,media_type,guid,title,year,summary,duration,rating,thumb,genres,viewed,added_at)
VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?) ON CONFLICT(rating_key) DO UPDATE SET
library_key=excluded.library_key,media_type=excluded.media_type,guid=excluded.guid,title=excluded.title,year=excluded.year,
summary=excluded.summary,duration=excluded.duration,rating=excluded.rating,thumb=excluded.thumb,
genres=excluded.genres,viewed=excluded.viewed,added_at=excluded.added_at`)
	if err != nil {
		return err
	}
	defer stmt.Close()
	for _, item := range items {
		genres, _ := json.Marshal(item.Genres)
		if _, err := stmt.ExecContext(ctx, item.RatingKey, item.Library, item.Type, item.GUID, item.Title, item.Year,
			item.Summary, item.Duration, item.Rating, item.Thumb, string(genres), item.Viewed, item.AddedAt); err != nil {
			return fmt.Errorf("save item %q: %w", item.Title, err)
		}
	}
	return tx.Commit()
}

func (s *Store) ItemPoster(ctx context.Context, ratingKey string) (string, error) {
	var thumb string
	if err := s.db.QueryRowContext(ctx, `SELECT thumb FROM media_items WHERE rating_key=?`, ratingKey).Scan(&thumb); err != nil {
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

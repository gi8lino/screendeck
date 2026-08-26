package store

import (
	"context"
	"encoding/json"

	"github.com/gi8lino/screendeck/internal/media"
)

// nextItems returns a participant's next unvoted media items in deck order.
func (s *Store) nextItems(ctx context.Context, code, participantID string, limit int) ([]media.Item, error) {
	const query = `
SELECT
  m.id,
  m.library_key,
  m.media_type,
  m.guid,
  m.title,
  m.year,
  m.summary,
  m.duration,
  m.rating,
  m.genres,
  m.viewed,
  m.added_at
FROM participants p
JOIN room_items rm
  ON rm.room_code = p.room_code
JOIN media_items m
  ON m.id = rm.item_id
LEFT JOIN item_votes v
  ON v.room_code = rm.room_code
 AND v.item_id = rm.item_id
 AND v.participant_id = p.id
WHERE p.id = ?
  AND p.room_code = ?
  AND v.item_id IS NULL
  AND (
    json_array_length(p.genres) = 0
    OR (
      p.genre_mode = 'all'
      AND NOT EXISTS (
        SELECT 1
        FROM json_each(p.genres) pg
        WHERE NOT EXISTS (
          SELECT 1
          FROM json_each(m.genres) mg
          WHERE lower(trim(CAST(mg.value AS TEXT))) = lower(trim(CAST(pg.value AS TEXT)))
        )
      )
    )
    OR (
      p.genre_mode <> 'all'
      AND EXISTS (
        SELECT 1
        FROM json_each(m.genres) mg
        JOIN json_each(p.genres) pg
          ON lower(trim(CAST(mg.value AS TEXT))) = lower(trim(CAST(pg.value AS TEXT)))
      )
    )
  )
ORDER BY rm.position
LIMIT ?
`
	rows, err := s.db.QueryContext(ctx, query, participantID, code, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close() // nolint:errcheck

	items := make([]media.Item, 0, limit)
	for rows.Next() {
		item, scanErr := scanItem(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		items = append(items, *item)
	}
	return items, rows.Err()
}

// matchItems returns media liked by every active participant.
func (s *Store) matchItems(ctx context.Context, code string) ([]media.Item, error) {
	const query = `
SELECT
  m.id,
  m.library_key,
  m.media_type,
  m.guid,
  m.title,
  m.year,
  m.summary,
  m.duration,
  m.rating,
  m.genres,
  m.viewed,
  m.added_at
FROM item_matches x
JOIN media_items m
  ON m.id = x.item_id
WHERE x.room_code = ?
ORDER BY x.matched_at DESC, m.title
`
	rows, err := s.db.QueryContext(ctx, query, code)
	if err != nil {
		return nil, err
	}
	defer rows.Close() // nolint:errcheck

	var items []media.Item
	for rows.Next() {
		item, err := scanItem(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, *item)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return items, nil
}

// scanner abstracts database row scanning for media items.
type scanner interface {
	Scan(...any) error
}

// scanItem decodes a media item from a database row.
func scanItem(row scanner) (*media.Item, error) {
	var item media.Item
	var genres string
	if err := row.Scan(
		&item.ID,
		&item.LibraryKey,
		&item.Type,
		&item.GUID,
		&item.Title,
		&item.Year,
		&item.Summary,
		&item.Duration,
		&item.Rating,
		&genres,
		&item.Viewed,
		&item.AddedAt,
	); err != nil {
		return nil, err
	}
	decodeGenres(genres, &item.Genres)
	return &item, nil
}

// decodeGenres decodes a stored JSON genre list and falls back to an empty list.
func decodeGenres(raw string, target *[]string) {
	if err := json.Unmarshal([]byte(raw), target); err != nil {
		*target = []string{}
		return
	}
	if *target == nil {
		*target = []string{}
	}
}

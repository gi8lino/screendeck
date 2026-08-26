package store

import (
	"context"
	"database/sql"
	"errors"
	"time"

	roomdomain "github.com/gi8lino/screendeck/internal/room"
)

// ParticipantByToken authenticates and returns a room participant.
func (s *Store) ParticipantByToken(
	ctx context.Context,
	code string,
	tokenHash string,
) (roomdomain.Participant, error) {
	const query = `
SELECT
  id,
  name,
  genres,
  genre_mode
FROM participants
WHERE room_code = ?
  AND token_hash = ?
`
	var participant roomParticipant
	var genres string
	err := s.db.QueryRowContext(ctx, query, code, tokenHash).Scan(
		&participant.ID,
		&participant.Name,
		&genres,
		&participant.GenreMode,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return participant, roomdomain.ErrNotFound
	}
	if err != nil {
		return participant, err
	}
	decodeGenres(genres, &participant.Genres)
	return participant, nil
}

// RoomGenres returns the genres represented by the current room deck.
func (s *Store) RoomGenres(ctx context.Context, code string) ([]string, error) {
	if err := s.ensureActiveRoom(ctx, code); err != nil {
		return nil, err
	}

	const query = `
SELECT DISTINCT CAST(j.value AS TEXT)
FROM (
  SELECT
    room_code,
    item_id
  FROM room_item_pool
  WHERE room_code = ?

  UNION

  SELECT
    room_code,
    item_id
  FROM room_items
  WHERE room_code = ?
) rp
JOIN media_items m
  ON m.id = rp.item_id
JOIN json_each(m.genres) j
WHERE trim(CAST(j.value AS TEXT)) <> ''
ORDER BY CAST(j.value AS TEXT) COLLATE NOCASE
`
	rows, err := s.db.QueryContext(ctx, query, code, code)
	if err != nil {
		return nil, err
	}
	defer rows.Close() // nolint:errcheck

	var genres []string
	for rows.Next() {
		var genre string
		if err := rows.Scan(&genre); err != nil {
			return nil, err
		}
		genres = append(genres, genre)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return genres, nil
}

// ensureActiveRoom verifies that a room exists and has not expired.
func (s *Store) ensureActiveRoom(ctx context.Context, code string) error {
	const query = `
SELECT 1
FROM rooms
WHERE code = ?
  AND expires_at > ?
`
	var exists int
	if err := s.db.QueryRowContext(ctx, query, code, time.Now().Unix()).Scan(&exists); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return roomdomain.ErrNotFound
		}
		return err
	}
	return nil
}

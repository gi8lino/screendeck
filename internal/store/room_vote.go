package store

import (
	"context"
	"database/sql"
	"time"

	roomdomain "github.com/gi8lino/screendeck/internal/room"
)

// unanimousMatch reports whether every active participant liked an item.
func unanimousMatch(participants, likes int) bool {
	return participants > 1 && likes == participants
}

// setMatchStateTx persists whether an item is currently a unanimous room match.
func setMatchStateTx(
	ctx context.Context,
	tx *sql.Tx,
	code string,
	itemID string,
	matched bool,
) error {
	if !matched {
		const deleteMatchQuery = `
DELETE FROM item_matches
WHERE room_code = ?
  AND item_id = ?
`
		_, err := tx.ExecContext(ctx, deleteMatchQuery, code, itemID)
		return err
	}

	const insertMatchQuery = `
INSERT OR IGNORE INTO item_matches (
  room_code,
  item_id,
  matched_at
) VALUES (
  ?, ?, ?
)
`
	_, err := tx.ExecContext(ctx, insertMatchQuery, code, itemID, time.Now().Unix())
	return err
}

// Vote persists a participant vote and reports whether it produces a unanimous match.
func (s *Store) Vote(
	ctx context.Context,
	code string,
	participantID string,
	itemID string,
	liked bool,
) (matched bool, err error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return false, err
	}
	defer tx.Rollback() // nolint:errcheck

	if err := upsertVoteTx(ctx, tx, code, participantID, itemID, liked); err != nil {
		return false, err
	}

	participants, likes, err := voteMatchCountsTx(ctx, tx, code, itemID)
	if err != nil {
		return false, err
	}
	matched = unanimousMatch(participants, likes)
	if err := setMatchStateTx(ctx, tx, code, itemID, matched); err != nil {
		return false, err
	}

	if err := cancelNextRoundIfUnavailableTx(ctx, tx, code); err != nil {
		return false, err
	}
	if err := reconcileRoomPhaseTx(ctx, tx, code); err != nil {
		return false, err
	}

	return matched, tx.Commit()
}

// upsertVoteTx records a vote only when the participant can vote for the active room item.
func upsertVoteTx(
	ctx context.Context,
	tx *sql.Tx,
	code string,
	participantID string,
	itemID string,
	liked bool,
) error {
	const query = `
INSERT INTO item_votes (
  room_code,
  participant_id,
  item_id,
  liked,
  created_at
)
SELECT
  ?, ?, ?, ?, ?
WHERE EXISTS (
  SELECT 1
  FROM participants p
  JOIN media_items m
    ON m.id = ?
  WHERE p.id = ?
    AND p.room_code = ?
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
)
AND EXISTS (
  SELECT 1
  FROM room_items
  WHERE room_code = ?
    AND item_id = ?
)
ON CONFLICT (room_code, participant_id, item_id) DO UPDATE SET
  liked = excluded.liked,
  created_at = excluded.created_at
`
	result, err := tx.ExecContext(
		ctx,
		query,
		code,
		participantID,
		itemID,
		liked,
		time.Now().Unix(),
		itemID,
		participantID,
		code,
		code,
		itemID,
	)
	if err != nil {
		return err
	}

	count, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if count == 0 {
		return roomdomain.ErrNotFound
	}
	return nil
}

// voteMatchCountsTx returns active participant and positive-vote counts for an item.
func voteMatchCountsTx(
	ctx context.Context,
	tx *sql.Tx,
	code string,
	itemID string,
) (participants, likes int, err error) {
	const query = `
SELECT
  (
    SELECT COUNT(*)
    FROM participants
    WHERE room_code = ?
  ),
  (
    SELECT COUNT(*)
    FROM item_votes
    WHERE room_code = ?
      AND item_id = ?
      AND liked = 1
  )
`
	err = tx.QueryRowContext(ctx, query, code, code, itemID).Scan(&participants, &likes)
	return participants, likes, err
}

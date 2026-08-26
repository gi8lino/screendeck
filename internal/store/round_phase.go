package store

import (
	"context"
	"database/sql"

	roomdomain "github.com/gi8lino/screendeck/internal/room"
)

// cancelNextRoundIfUnavailableTx clears a request once fewer than two matches remain.
func cancelNextRoundIfUnavailableTx(ctx context.Context, tx *sql.Tx, code string) error {
	const query = `
SELECT COUNT(*)
FROM item_matches
WHERE room_code = ?
`
	var matches int
	if err := tx.QueryRowContext(ctx, query, code).Scan(&matches); err != nil {
		return err
	}
	if matches >= 2 {
		return nil
	}
	return cancelNextRoundRequestTx(ctx, tx, code)
}

// reconcileRoomPhaseTx derives the persistent room phase from readiness and round progress.
func reconcileRoomPhaseTx(ctx context.Context, tx *sql.Tx, code string) error {
	round, err := roomRoundTx(ctx, tx, code)
	if err != nil {
		return err
	}
	ready, err := roundReadyCountTx(ctx, tx, code, round)
	if err != nil {
		return err
	}

	var remaining int
	if err := roundRemainingQuery(ctx, tx, code, &remaining); err != nil {
		return err
	}
	matches, err := roomMatchCountTx(ctx, tx, code)
	if err != nil {
		return err
	}

	const updateQuery = `
UPDATE rooms
SET phase = ?
WHERE code = ?
`
	_, err = tx.ExecContext(ctx, updateQuery, deriveRoomPhase(ready, remaining, matches), code)
	return err
}

// roomRoundTx returns the current round without applying active-room filtering.
func roomRoundTx(ctx context.Context, tx *sql.Tx, code string) (int, error) {
	const query = `
SELECT round
FROM rooms
WHERE code = ?
`
	var round int
	if err := tx.QueryRowContext(ctx, query, code).Scan(&round); err != nil {
		return 0, err
	}
	return round, nil
}

// roomMatchCountTx returns the number of current unanimous room matches.
func roomMatchCountTx(ctx context.Context, tx *sql.Tx, code string) (int, error) {
	const query = `
SELECT COUNT(*)
FROM item_matches
WHERE room_code = ?
`
	var matches int
	if err := tx.QueryRowContext(ctx, query, code).Scan(&matches); err != nil {
		return 0, err
	}
	return matches, nil
}

// deriveRoomPhase derives a room phase from readiness, remaining votes, and current matches.
func deriveRoomPhase(ready, remaining, matches int) roomPhase {
	switch {
	case ready > 0:
		return roomdomain.PhaseNextRoundRequested
	case remaining == 0 && matches == 1:
		return roomdomain.PhaseFinished
	case remaining == 0:
		return roomdomain.PhaseRoundComplete
	default:
		return roomdomain.PhaseSwiping
	}
}

// roundRemaining returns the number of participant-title pairs still awaiting a vote.
func (s *Store) roundRemaining(ctx context.Context, code string) (int, error) {
	var remaining int
	err := roundRemainingQuery(ctx, s.db, code, &remaining)
	return remaining, err
}

// queryRower abstracts QueryRowContext for shared SQL helpers.
type queryRower interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

// roundRemainingQuery counts outstanding votes while respecting personal genres.
func roundRemainingQuery(ctx context.Context, db queryRower, code string, remaining *int) error {
	const query = `
SELECT COUNT(*)
FROM participants p
JOIN room_items rm
  ON rm.room_code = p.room_code
JOIN media_items m
  ON m.id = rm.item_id
LEFT JOIN item_votes v
  ON v.room_code = rm.room_code
 AND v.item_id = rm.item_id
 AND v.participant_id = p.id
WHERE p.room_code = ?
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
`
	return db.QueryRowContext(ctx, query, code).Scan(remaining)
}

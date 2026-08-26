package store

import (
	"context"
	"database/sql"
	"errors"
	"time"

	roomdomain "github.com/gi8lino/screendeck/internal/room"
)

// AddMoreTitles activates additional unused titles in the first round for the room host.
func (s *Store) AddMoreTitles(
	ctx context.Context,
	code string,
	participantID string,
	count int,
) (added, remaining int, err error) {
	if count <= 0 || count > 1000 {
		return 0, 0, errors.New("add-more count must be between 1 and 1000")
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, 0, err
	}
	defer tx.Rollback() // nolint:errcheck

	if err := validateAddMoreTitlesTx(ctx, tx, code, participantID); err != nil {
		return 0, 0, err
	}
	itemIDs, err := unusedPoolItemIDsTx(ctx, tx, code, count)
	if err != nil {
		return 0, 0, err
	}
	if len(itemIDs) == 0 {
		return 0, 0, errors.New("no more titles are available")
	}

	nextPosition, err := nextRoomItemPositionTx(ctx, tx, code)
	if err != nil {
		return 0, 0, err
	}
	if err := insertAdditionalRoomItemsTx(ctx, tx, code, itemIDs, nextPosition); err != nil {
		return 0, 0, err
	}
	if err := markPoolItemsUsedTx(ctx, tx, code, itemIDs); err != nil {
		return 0, 0, err
	}

	if err := cancelNextRoundRequestTx(ctx, tx, code); err != nil {
		return 0, 0, err
	}
	if err := reconcileRoomPhaseTx(ctx, tx, code); err != nil {
		return 0, 0, err
	}

	remaining, err = unusedPoolItemCountTx(ctx, tx, code)
	if err != nil {
		return 0, 0, err
	}
	if err := tx.Commit(); err != nil {
		return 0, 0, err
	}
	return len(itemIDs), remaining, nil
}

// validateAddMoreTitlesTx verifies that the participant may expand the room's first round.
func validateAddMoreTitlesTx(ctx context.Context, tx *sql.Tx, code, participantID string) error {
	const query = `
SELECT
  r.round,
  r.owner_id
FROM rooms r
JOIN participants p
  ON p.room_code = r.code
WHERE r.code = ?
  AND p.id = ?
  AND r.expires_at > ?
`
	var round int
	var ownerID string
	if err := tx.QueryRowContext(ctx, query, code, participantID, time.Now().Unix()).Scan(&round, &ownerID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return roomdomain.ErrNotFound
		}
		return err
	}
	if ownerID != participantID {
		return errors.New("only the room host can add more titles")
	}
	if round != 1 {
		return errors.New("more titles can only be added during the first round")
	}
	return nil
}

// unusedPoolItemIDsTx returns the next unused items from the original first-round pool.
func unusedPoolItemIDsTx(
	ctx context.Context,
	tx *sql.Tx,
	code string,
	count int,
) ([]string, error) {
	const query = `
SELECT item_id
FROM room_item_pool
WHERE room_code = ?
  AND used = 0
ORDER BY position
LIMIT ?
`
	rows, err := tx.QueryContext(ctx, query, code, count)
	if err != nil {
		return nil, err
	}
	defer rows.Close() // nolint:errcheck

	var itemIDs []string
	for rows.Next() {
		var itemID string
		if err := rows.Scan(&itemID); err != nil {
			return nil, err
		}
		itemIDs = append(itemIDs, itemID)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return itemIDs, nil
}

// nextRoomItemPositionTx returns the next position available in the active room deck.
func nextRoomItemPositionTx(ctx context.Context, tx *sql.Tx, code string) (int, error) {
	const query = `
SELECT COALESCE(MAX(position) + 1, 0)
FROM room_items
WHERE room_code = ?
`
	var position int
	if err := tx.QueryRowContext(ctx, query, code).Scan(&position); err != nil {
		return 0, err
	}
	return position, nil
}

// insertAdditionalRoomItemsTx appends unused pool items without duplicating active titles.
func insertAdditionalRoomItemsTx(
	ctx context.Context,
	tx *sql.Tx,
	code string,
	itemIDs []string,
	startPosition int,
) error {
	const query = `
INSERT OR IGNORE INTO room_items (
  room_code,
  item_id,
  position
) VALUES (
  ?, ?, ?
)
`
	stmt, err := tx.PrepareContext(ctx, query)
	if err != nil {
		return err
	}
	defer stmt.Close() // nolint:errcheck

	for offset, itemID := range itemIDs {
		if _, err := stmt.ExecContext(ctx, code, itemID, startPosition+offset); err != nil {
			return err
		}
	}
	return nil
}

// markPoolItemsUsedTx marks original pool items as active in the first-round deck.
func markPoolItemsUsedTx(ctx context.Context, tx *sql.Tx, code string, itemIDs []string) error {
	const query = `
UPDATE room_item_pool
SET used = 1
WHERE room_code = ?
  AND item_id = ?
`
	for _, itemID := range itemIDs {
		if _, err := tx.ExecContext(ctx, query, code, itemID); err != nil {
			return err
		}
	}
	return nil
}

// unusedPoolItemCountTx returns the number of unused items remaining in the first-round pool.
func unusedPoolItemCountTx(ctx context.Context, tx *sql.Tx, code string) (int, error) {
	const query = `
SELECT COUNT(*)
FROM room_item_pool
WHERE room_code = ?
  AND used = 0
`
	var remaining int
	if err := tx.QueryRowContext(ctx, query, code).Scan(&remaining); err != nil {
		return 0, err
	}
	return remaining, nil
}

// staleRoundReadinessRequest reports whether the client refers to a round that already advanced.
func staleRoundReadinessRequest(currentRound, expectedRound int) bool {
	return expectedRound > 0 && currentRound > expectedRound
}

// futureRoundReadinessRequest reports whether the client refers to a round ahead of the room.
func futureRoundReadinessRequest(currentRound, expectedRound int) bool {
	return expectedRound > 0 && currentRound < expectedRound
}

// roomParticipantCountTx returns the number of active participants in a room.
func roomParticipantCountTx(ctx context.Context, tx *sql.Tx, code string) (int, error) {
	const query = `
SELECT COUNT(*)
FROM participants
WHERE room_code = ?
`
	var count int
	if err := tx.QueryRowContext(ctx, query, code).Scan(&count); err != nil {
		return 0, err
	}
	return count, nil
}

// roomRoundSummaryTx returns the current deck size and active participant count.
func roomRoundSummaryTx(
	ctx context.Context,
	tx *sql.Tx,
	code string,
) (titles, participants int, err error) {
	const titlesQuery = `
SELECT COUNT(*)
FROM room_items
WHERE room_code = ?
`
	if err := tx.QueryRowContext(ctx, titlesQuery, code).Scan(&titles); err != nil {
		return 0, 0, err
	}
	participants, err = roomParticipantCountTx(ctx, tx, code)
	if err != nil {
		return 0, 0, err
	}
	return titles, participants, nil
}

// setRoundReadinessTx records or withdraws one participant's next-round readiness.
func setRoundReadinessTx(
	ctx context.Context,
	tx *sql.Tx,
	code string,
	round int,
	participantID string,
	ready bool,
) error {
	if !ready {
		const clearReadyQuery = `
DELETE FROM round_ready
WHERE room_code = ?
  AND round = ?
  AND participant_id = ?
`
		_, err := tx.ExecContext(ctx, clearReadyQuery, code, round, participantID)
		return err
	}

	const matchesQuery = `
SELECT COUNT(*)
FROM item_matches
WHERE room_code = ?
`
	var matches int
	if err := tx.QueryRowContext(ctx, matchesQuery, code).Scan(&matches); err != nil {
		return err
	}
	if matches < 2 {
		return errors.New("another round requires at least two matches")
	}

	const readyQuery = `
INSERT INTO round_ready (
  room_code,
  round,
  participant_id,
  created_at
) VALUES (
  ?, ?, ?, ?
)
ON CONFLICT (room_code, round, participant_id) DO NOTHING
`
	if _, err := tx.ExecContext(ctx, readyQuery, code, round, participantID, time.Now().Unix()); err != nil {
		return err
	}

	const requesterQuery = `
UPDATE rooms
SET next_round_requester_id = CASE
  WHEN next_round_requester_id = '' THEN ?
  ELSE next_round_requester_id
END
WHERE code = ?
`
	_, err := tx.ExecContext(ctx, requesterQuery, participantID, code)
	return err
}

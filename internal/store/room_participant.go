package store

import (
	"context"
	"database/sql"
	"errors"
	"time"

	roomdomain "github.com/gi8lino/screendeck/internal/room"
)

// RemoveParticipant removes a non-host participant when requested by the current room host.
func (s *Store) RemoveParticipant(
	ctx context.Context,
	code string,
	requesterTokenHash string,
	participantID string,
) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback() // nolint:errcheck

	requesterID, err := authenticateRoomHostTx(ctx, tx, code, requesterTokenHash)
	if err != nil {
		return err
	}
	if requesterID == participantID {
		return errors.New("the room host cannot remove themselves")
	}

	if err := removeParticipantTx(ctx, tx, code, participantID); err != nil {
		return err
	}
	return tx.Commit()
}

// authenticateRoomHostTx authenticates a room participant and verifies that they are the current host.
func authenticateRoomHostTx(
	ctx context.Context,
	tx *sql.Tx,
	code string,
	tokenHash string,
) (string, error) {
	const hostQuery = `
SELECT id
FROM participants p
JOIN rooms r
  ON r.code = p.room_code
WHERE p.room_code = ?
  AND p.token_hash = ?
  AND r.owner_id = p.id
`
	var requesterID string
	if err := tx.QueryRowContext(ctx, hostQuery, code, tokenHash).Scan(&requesterID); err == nil {
		return requesterID, nil
	} else if !errors.Is(err, sql.ErrNoRows) {
		return "", err
	}

	authenticated, err := roomTokenAuthenticatedTx(ctx, tx, code, tokenHash)
	if err != nil {
		return "", err
	}
	if !authenticated {
		return "", roomdomain.ErrNotFound
	}
	return "", roomdomain.ErrForbidden
}

// roomTokenAuthenticatedTx reports whether a participant token belongs to the room.
func roomTokenAuthenticatedTx(
	ctx context.Context,
	tx *sql.Tx,
	code string,
	tokenHash string,
) (authenticated bool, err error) {
	const query = `
SELECT COUNT(*)
FROM participants
WHERE room_code = ?
  AND token_hash = ?
`
	var count int
	if err := tx.QueryRowContext(ctx, query, code, tokenHash).Scan(&count); err != nil {
		return false, err
	}
	return count > 0, nil
}

// LeaveRoom removes an authenticated participant and transfers room ownership when necessary.
func (s *Store) LeaveRoom(ctx context.Context, code, tokenHash string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback() // nolint:errcheck

	const participantQuery = `
SELECT id
FROM participants
WHERE room_code = ?
  AND token_hash = ?
`
	var leavingID string
	if err := tx.QueryRowContext(ctx, participantQuery, code, tokenHash).Scan(&leavingID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return roomdomain.ErrNotFound
		}
		return err
	}
	if err := removeParticipantTx(ctx, tx, code, leavingID); err != nil {
		return err
	}
	return tx.Commit()
}

// removeParticipantTx deletes one participant and reconciles ownership, matches, and room phase.
func removeParticipantTx(ctx context.Context, tx *sql.Tx, code, participantID string) error {
	if err := ensureParticipantExistsTx(ctx, tx, code, participantID); err != nil {
		return err
	}
	if err := deleteParticipantTx(ctx, tx, code, participantID); err != nil {
		return err
	}
	if err := transferRoomOwnershipTx(ctx, tx, code, participantID); err != nil {
		return err
	}
	if err := completeDepartureMatchesTx(ctx, tx, code); err != nil {
		return err
	}
	if err := cancelNextRoundRequestTx(ctx, tx, code); err != nil {
		return err
	}
	return reconcileRoomPhaseTx(ctx, tx, code)
}

// ensureParticipantExistsTx verifies that a participant still belongs to the room.
func ensureParticipantExistsTx(ctx context.Context, tx *sql.Tx, code, participantID string) error {
	const query = `
SELECT COUNT(*)
FROM participants
WHERE room_code = ?
  AND id = ?
`
	var count int
	if err := tx.QueryRowContext(ctx, query, code, participantID).Scan(&count); err != nil {
		return err
	}
	if count == 0 {
		return roomdomain.ErrNotFound
	}
	return nil
}

// deleteParticipantTx removes a participant and cascades its dependent room records.
func deleteParticipantTx(ctx context.Context, tx *sql.Tx, code, participantID string) error {
	const query = `
DELETE FROM participants
WHERE room_code = ?
  AND id = ?
`
	_, err := tx.ExecContext(ctx, query, code, participantID)
	return err
}

// transferRoomOwnershipTx transfers ownership when the departing participant was the host.
func transferRoomOwnershipTx(ctx context.Context, tx *sql.Tx, code, participantID string) error {
	const query = `
UPDATE rooms
SET owner_id = COALESCE(
  (
    SELECT id
    FROM participants
    WHERE room_code = ?
    ORDER BY joined_at, id
    LIMIT 1
  ),
  ''
)
WHERE code = ?
  AND owner_id = ?
`
	_, err := tx.ExecContext(ctx, query, code, code, participantID)
	return err
}

// completeDepartureMatchesTx records likes that became unanimous after a participant left.
func completeDepartureMatchesTx(ctx context.Context, tx *sql.Tx, code string) error {
	// Completed matches are intentionally retained even if room membership later changes.
	const query = `
INSERT OR IGNORE INTO item_matches (
  room_code,
  item_id,
  matched_at
)
SELECT
  rm.room_code,
  rm.item_id,
  ?
FROM room_items rm
WHERE rm.room_code = ?
  AND (
    SELECT COUNT(*)
    FROM participants
    WHERE room_code = ?
  ) > 1
  AND (
    SELECT COUNT(*)
    FROM item_votes
    WHERE room_code = ?
      AND item_id = rm.item_id
      AND liked = 1
  ) = (
    SELECT COUNT(*)
    FROM participants
    WHERE room_code = ?
  )
`
	_, err := tx.ExecContext(ctx, query, time.Now().Unix(), code, code, code, code)
	return err
}

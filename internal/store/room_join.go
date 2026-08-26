package store

import (
	"context"
	"database/sql"
	"errors"
	"time"

	roomdomain "github.com/gi8lino/screendeck/internal/room"
)

// JoinRoom persists a participant in an active room.
func (s *Store) JoinRoom(
	ctx context.Context,
	code string,
	participant roomdomain.Participant,
	tokenHash string,
	membership roomdomain.MembershipCredential,
) error {
	normalizeParticipant(&participant)
	genres, err := encodeParticipantGenres(participant.Genres)
	if err != nil {
		return err
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback() // nolint:errcheck

	if err := joinParticipantTx(ctx, tx, code, participant, tokenHash, genres); err != nil {
		return err
	}
	if err := s.saveRoomMembershipTx(ctx, tx, code, participant.ID, membership); err != nil {
		return err
	}

	// Membership changed, so any pending next-round agreement must be renewed
	// by the new set of active participants.
	if err := cancelNextRoundRequestTx(ctx, tx, code); err != nil {
		return err
	}
	if err := invalidateNonUnanimousMatchesTx(ctx, tx, code); err != nil {
		return err
	}
	if err := reconcileRoomPhaseTx(ctx, tx, code); err != nil {
		return err
	}

	return tx.Commit()
}

// joinParticipantTx inserts a participant only when the target room is still active.
func joinParticipantTx(
	ctx context.Context,
	tx *sql.Tx,
	code string,
	participant roomParticipant,
	tokenHash string,
	genres string,
) error {
	const query = `
INSERT INTO participants (
  id,
  room_code,
  name,
  genres,
  genre_mode,
  token_hash,
  joined_at
)
SELECT
  ?,
  code,
  ?,
  ?,
  ?,
  ?,
  ?
FROM rooms
WHERE code = ?
  AND expires_at > ?
  AND locked = 0
`
	result, err := tx.ExecContext(
		ctx,
		query,
		participant.ID,
		participant.Name,
		genres,
		participant.GenreMode,
		tokenHash,
		time.Now().Unix(),
		code,
		time.Now().Unix(),
	)
	if err != nil {
		return err
	}

	count, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if count == 0 {
		return joinRoomUnavailable(ctx, tx, code)
	}
	return nil
}

// joinRoomUnavailable distinguishes a missing room from one closed to new participants.
func joinRoomUnavailable(ctx context.Context, tx *sql.Tx, code string) error {
	const query = `
SELECT locked
FROM rooms
WHERE code = ?
  AND expires_at > ?
`
	var locked bool
	if err := tx.QueryRowContext(ctx, query, code, time.Now().Unix()).Scan(&locked); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return roomdomain.ErrNotFound
		}
		return err
	}
	if locked {
		return roomdomain.ErrLocked
	}
	return roomdomain.ErrNotFound
}

// invalidateNonUnanimousMatchesTx removes matches invalidated by a newly joined participant.
func invalidateNonUnanimousMatchesTx(ctx context.Context, tx *sql.Tx, code string) error {
	const query = `
DELETE FROM item_matches
WHERE room_code = ?
  AND (
    SELECT COUNT(*)
    FROM item_votes
    WHERE room_code = ?
      AND item_id = item_matches.item_id
      AND liked = 1
  ) < (
    SELECT COUNT(*)
    FROM participants
    WHERE room_code = ?
  )
`
	_, err := tx.ExecContext(ctx, query, code, code, code)
	return err
}

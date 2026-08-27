package store

import (
	"cmp"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	roomdomain "github.com/gi8lino/screendeck/internal/room"
)

// CreateRoom persists a room, its owner, the active deck, and the original eligible item pool.
func (s *Store) CreateRoom(
	ctx context.Context,
	room roomdomain.Room,
	participant roomdomain.Participant,
	tokenHash string,
	itemIDs []string,
	poolIDs []string,
	membership roomdomain.MembershipCredential,
) error {
	normalizeRoomCreation(&room, &participant)
	participantGenres, err := encodeParticipantGenres(participant.Genres)
	if err != nil {
		return err
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback() // nolint:errcheck

	if err := insertRoomTx(ctx, tx, room); err != nil {
		return err
	}
	if err := insertParticipantTx(ctx, tx, room.Code, participant, tokenHash, participantGenres); err != nil {
		return err
	}
	if err := s.saveRoomMembershipTx(ctx, tx, room.Code, participant.ID, membership); err != nil {
		return err
	}
	if err := insertRoomItemsTx(ctx, tx, room.Code, itemIDs, 0); err != nil {
		return err
	}
	if err := insertRoomPoolTx(ctx, tx, room.Code, poolIDs, itemIDs); err != nil {
		return err
	}
	if err := reconcileRoomPhaseTx(ctx, tx, room.Code); err != nil {
		return err
	}

	return tx.Commit()
}

// normalizeRoomCreation applies default room and participant values before persistence.
func normalizeRoomCreation(room *roomRecord, participant *roomParticipant) {
	if room.Round <= 0 {
		room.Round = 1
	}
	room.Phase = cmp.Or(room.Phase, roomdomain.PhaseSwiping)
	room.OwnerID = cmp.Or(room.OwnerID, participant.ID)

	normalizeParticipant(participant)
}

// normalizeParticipant applies persistence defaults to participant state.
func normalizeParticipant(participant *roomParticipant) {
	if participant.Genres == nil {
		participant.Genres = []string{}
	}
	participant.GenreMode = cmp.Or(participant.GenreMode, "any")
}

// encodeParticipantGenres serializes participant genres for SQLite JSON queries.
func encodeParticipantGenres(genres []string) (string, error) {
	encoded, err := json.Marshal(genres)
	if err != nil {
		return "", fmt.Errorf("encode participant genres: %w", err)
	}
	return string(encoded), nil
}

// insertRoomTx inserts room metadata inside an existing transaction.
func insertRoomTx(ctx context.Context, tx *sql.Tx, room roomRecord) error {
	const query = `
INSERT INTO rooms (
  code,
  round,
  phase,
  owner_id,
  created_at,
  expires_at,
  locked
) VALUES (
  ?, ?, ?, ?, ?, ?, ?
)
`
	_, err := tx.ExecContext(
		ctx,
		query,
		room.Code,
		room.Round,
		room.Phase,
		room.OwnerID,
		room.CreatedAt.Unix(),
		room.ExpiresAt.Unix(),
		room.Locked,
	)
	return err
}

// insertParticipantTx inserts a participant inside an existing room transaction.
func insertParticipantTx(
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
) VALUES (
  ?, ?, ?, ?, ?, ?, ?
)
`
	_, err := tx.ExecContext(
		ctx,
		query,
		participant.ID,
		code,
		participant.Name,
		genres,
		participant.GenreMode,
		tokenHash,
		time.Now().Unix(),
	)
	return err
}

// insertRoomItemsTx inserts an ordered set of active room items at the supplied start position.
func insertRoomItemsTx(
	ctx context.Context,
	tx *sql.Tx,
	code string,
	itemIDs []string,
	startPosition int,
) error {
	const query = `
INSERT INTO room_items (
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

// insertRoomPoolTx inserts the original eligible item pool and marks active items as used.
func insertRoomPoolTx(
	ctx context.Context,
	tx *sql.Tx,
	code string,
	poolIDs []string,
	activeIDs []string,
) error {
	const query = `
INSERT INTO room_item_pool (
  room_code,
  item_id,
  position,
  used
) VALUES (
  ?, ?, ?, ?
)
`
	stmt, err := tx.PrepareContext(ctx, query)
	if err != nil {
		return err
	}
	defer stmt.Close() // nolint:errcheck

	active := make(map[string]struct{}, len(activeIDs))
	for _, itemID := range activeIDs {
		active[itemID] = struct{}{}
	}
	for position, itemID := range poolIDs {
		_, used := active[itemID]
		if _, err := stmt.ExecContext(ctx, code, itemID, position, used); err != nil {
			return err
		}
	}

	return nil
}

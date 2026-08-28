package store

import (
	"context"
	"database/sql"
	"errors"
	mathrand "math/rand/v2"
	"time"

	roomdomain "github.com/gi8lino/screendeck/internal/room"
)

// roundReadinessRequestState contains validated state for a next-round readiness update.
type roundReadinessRequestState struct {
	// round is the active room round.
	round int
	// titles is the active deck size for an already-advanced request.
	titles int
	// required is the number of participants required for consensus.
	required int
	// stale reports whether the client request refers to an already-completed round.
	stale bool
}

// SetRoundReady updates one participant's next-round readiness and advances once everyone agrees.
func (s *Store) SetRoundReady(
	ctx context.Context,
	code string,
	participantID string,
	expectedRound int,
	ready bool,
) (roomdomain.RoundResult, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return roomdomain.RoundResult{}, err
	}
	defer tx.Rollback() // nolint:errcheck

	request, err := prepareRoundReadinessTx(ctx, tx, code, participantID, expectedRound)
	if err != nil {
		return roomdomain.RoundResult{}, err
	}
	if request.stale {
		return roomdomain.RoundResult{
			Round:    request.round,
			Titles:   request.titles,
			Required: request.required,
			Advanced: true,
		}, nil
	}

	readyCount, err := recordRoundReadinessTx(ctx, tx, code, request.round, participantID, ready)
	if err != nil {
		return roomdomain.RoundResult{}, err
	}
	if readyCount == request.required {
		nextRound, nextTitles, err := advanceRoundTx(ctx, tx, code, request.round)
		if err != nil {
			return roomdomain.RoundResult{}, err
		}
		if err := tx.Commit(); err != nil {
			return roomdomain.RoundResult{}, err
		}
		return roomdomain.RoundResult{
			Round:    nextRound,
			Titles:   nextTitles,
			Ready:    request.required,
			Required: request.required,
			Advanced: true,
		}, nil
	}

	if err := reconcileRoomPhaseTx(ctx, tx, code); err != nil {
		return roomdomain.RoundResult{}, err
	}
	if err := tx.Commit(); err != nil {
		return roomdomain.RoundResult{}, err
	}
	return roomdomain.RoundResult{
		Round:    request.round,
		Ready:    readyCount,
		Required: request.required,
	}, nil
}

// prepareRoundReadinessTx validates the room, round, participant, and consensus size.
func prepareRoundReadinessTx(
	ctx context.Context,
	tx *sql.Tx,
	code string,
	participantID string,
	expectedRound int,
) (roundReadinessRequestState, error) {
	round, err := activeRoomRoundTx(ctx, tx, code)
	if err != nil {
		return roundReadinessRequestState{}, err
	}

	if staleRoundReadinessRequest(round, expectedRound) {
		titles, required, err := roomRoundSummaryTx(ctx, tx, code)
		if err != nil {
			return roundReadinessRequestState{}, err
		}
		return roundReadinessRequestState{
			round:    round,
			titles:   titles,
			required: required,
			stale:    true,
		}, nil
	}
	if futureRoundReadinessRequest(round, expectedRound) {
		return roundReadinessRequestState{}, roomdomain.InvalidInput("room round changed")
	}

	if err := ensureParticipantExistsTx(ctx, tx, code, participantID); err != nil {
		return roundReadinessRequestState{}, err
	}
	required, err := roomParticipantCountTx(ctx, tx, code)
	if err != nil {
		return roundReadinessRequestState{}, err
	}
	if required < 2 {
		return roundReadinessRequestState{}, roomdomain.InvalidInput("another round needs at least two participants")
	}

	return roundReadinessRequestState{round: round, required: required}, nil
}

// recordRoundReadinessTx persists one readiness value and returns the current consensus count.
func recordRoundReadinessTx(
	ctx context.Context,
	tx *sql.Tx,
	code string,
	round int,
	participantID string,
	ready bool,
) (int, error) {
	if err := setRoundReadinessTx(ctx, tx, code, round, participantID, ready); err != nil {
		return 0, err
	}

	readyCount, err := roundReadyCountTx(ctx, tx, code, round)
	if err != nil {
		return 0, err
	}
	if readyCount == 0 {
		if err := clearNextRoundRequesterTx(ctx, tx, code); err != nil {
			return 0, err
		}
	}
	return readyCount, nil
}

// activeRoomRoundTx returns the current round for an active room.
func activeRoomRoundTx(ctx context.Context, tx *sql.Tx, code string) (int, error) {
	const query = `
SELECT round
FROM rooms
WHERE code = ?
  AND expires_at > ?
`
	var round int
	if err := tx.QueryRowContext(ctx, query, code, time.Now().Unix()).Scan(&round); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return 0, roomdomain.ErrNotFound
		}
		return 0, err
	}
	return round, nil
}

// roundReadyCountTx returns the number of active participants ready for the supplied round.
func roundReadyCountTx(
	ctx context.Context,
	tx *sql.Tx,
	code string,
	round int,
) (int, error) {
	const query = `
SELECT COUNT(*)
FROM round_ready rr
JOIN participants p
  ON p.id = rr.participant_id
 AND p.room_code = rr.room_code
WHERE rr.room_code = ?
  AND rr.round = ?
`
	var count int
	if err := tx.QueryRowContext(ctx, query, code, round).Scan(&count); err != nil {
		return 0, err
	}
	return count, nil
}

// clearNextRoundRequesterTx clears the participant that initiated next-round consensus.
func clearNextRoundRequesterTx(ctx context.Context, tx *sql.Tx, code string) error {
	const query = `
UPDATE rooms
SET next_round_requester_id = ''
WHERE code = ?
`
	_, err := tx.ExecContext(ctx, query, code)
	return err
}

// advanceRoundTx snapshots the current matches and makes them the next shuffled deck.
func advanceRoundTx(
	ctx context.Context,
	tx *sql.Tx,
	code string,
	round int,
) (nextRound, titleCount int, err error) {
	itemIDs, err := matchItemIDsTx(ctx, tx, code)
	if err != nil {
		return 0, 0, err
	}
	if len(itemIDs) < 2 {
		return 0, 0, roomdomain.InvalidInput("another round requires at least two matches")
	}
	mathrand.Shuffle(len(itemIDs), func(i, j int) {
		itemIDs[i], itemIDs[j] = itemIDs[j], itemIDs[i]
	})

	if err := clearRoundTx(ctx, tx, code); err != nil {
		return 0, 0, err
	}
	if err := insertRoomItemsTx(ctx, tx, code, itemIDs, 0); err != nil {
		return 0, 0, err
	}

	nextRound = round + 1
	titleCount = len(itemIDs)
	if err := updateRoomRoundTx(ctx, tx, code, round, nextRound); err != nil {
		return 0, 0, err
	}
	return nextRound, titleCount, nil
}

// matchItemIDsTx returns the current room matches in database order.
func matchItemIDsTx(ctx context.Context, tx *sql.Tx, code string) ([]string, error) {
	const query = `
SELECT item_id
FROM item_matches
WHERE room_code = ?
`
	rows, err := tx.QueryContext(ctx, query, code)
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

// clearRoundTx removes votes, matches, active items, and readiness before advancing.
func clearRoundTx(ctx context.Context, tx *sql.Tx, code string) error {
	queries := []string{
		`
DELETE FROM item_votes
WHERE room_code = ?
`,
		`
DELETE FROM item_matches
WHERE room_code = ?
`,
		`
DELETE FROM room_items
WHERE room_code = ?
`,
		`
DELETE FROM round_ready
WHERE room_code = ?
`,
	}
	for _, query := range queries {
		if _, err := tx.ExecContext(ctx, query, code); err != nil {
			return err
		}
	}
	return nil
}

// updateRoomRoundTx advances a room to the supplied round and resets its phase state.
func updateRoomRoundTx(
	ctx context.Context,
	tx *sql.Tx,
	code string,
	currentRound int,
	nextRound int,
) error {
	const query = `
UPDATE rooms
SET round = ?,
    phase = ?,
    next_round_requester_id = ''
WHERE code = ?
  AND round = ?
`
	_, err := tx.ExecContext(ctx, query, nextRound, roomdomain.PhaseSwiping, code, currentRound)
	return err
}

// cancelNextRoundRequestTx clears every participant's pending next-round agreement.
func cancelNextRoundRequestTx(ctx context.Context, tx *sql.Tx, code string) error {
	const clearReadyQuery = `
DELETE FROM round_ready
WHERE room_code = ?
`
	if _, err := tx.ExecContext(ctx, clearReadyQuery, code); err != nil {
		return err
	}

	const clearRequesterQuery = `
UPDATE rooms
SET next_round_requester_id = ''
WHERE code = ?
`
	_, err := tx.ExecContext(ctx, clearRequesterQuery, code)
	return err
}

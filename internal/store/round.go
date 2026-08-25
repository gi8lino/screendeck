package store

import (
	"context"
	"database/sql"
	"errors"
	mathrand "math/rand/v2"
	"time"
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
			return ErrNotFound
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
) (round, titles, readyCount, required int, advanced bool, err error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, 0, 0, 0, false, err
	}
	defer tx.Rollback() // nolint:errcheck

	request, err := prepareRoundReadinessTx(ctx, tx, code, participantID, expectedRound)
	if err != nil {
		return 0, 0, 0, 0, false, err
	}
	if request.stale {
		return request.round, request.titles, 0, request.required, true, nil
	}

	readyCount, err = recordRoundReadinessTx(ctx, tx, code, request.round, participantID, ready)
	if err != nil {
		return 0, 0, 0, 0, false, err
	}
	if readyCount == request.required {
		nextRound, nextTitles, err := advanceRoundTx(ctx, tx, code, request.round)
		if err != nil {
			return 0, 0, 0, 0, false, err
		}
		if err := tx.Commit(); err != nil {
			return 0, 0, 0, 0, false, err
		}
		return nextRound, nextTitles, request.required, request.required, true, nil
	}

	if err := reconcileRoomPhaseTx(ctx, tx, code); err != nil {
		return 0, 0, 0, 0, false, err
	}
	if err := tx.Commit(); err != nil {
		return 0, 0, 0, 0, false, err
	}
	return request.round, 0, readyCount, request.required, false, nil
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
		return roundReadinessRequestState{}, errors.New("room round changed")
	}

	if err := ensureParticipantExistsTx(ctx, tx, code, participantID); err != nil {
		return roundReadinessRequestState{}, err
	}
	required, err := roomParticipantCountTx(ctx, tx, code)
	if err != nil {
		return roundReadinessRequestState{}, err
	}
	if required < 2 {
		return roundReadinessRequestState{}, errors.New("another round needs at least two participants")
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
			return 0, ErrNotFound
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
		return 0, 0, errors.New("another round requires at least two matches")
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
	_, err := tx.ExecContext(ctx, query, nextRound, RoomPhaseSwiping, code, currentRound)
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
	_, err = tx.ExecContext(ctx, updateQuery, roomPhase(ready, remaining, matches), code)
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

// roomPhase derives a room phase from readiness, remaining votes, and current matches.
func roomPhase(ready, remaining, matches int) RoomPhase {
	switch {
	case ready > 0:
		return RoomPhaseNextRoundRequested
	case remaining == 0 && matches == 1:
		return RoomPhaseFinished
	case remaining == 0:
		return RoomPhaseRoundComplete
	default:
		return RoomPhaseSwiping
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

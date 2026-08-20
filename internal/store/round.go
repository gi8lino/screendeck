package store

import (
	"context"
	"database/sql"
	"errors"
	mathrand "math/rand/v2"
	"time"
)

func (s *Store) AddMoreTitles(ctx context.Context, code, participantID string, count int) (added, remaining int, err error) {
	if count <= 0 || count > 1000 {
		return 0, 0, errors.New("add-more count must be between 1 and 1000")
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, 0, err
	}
	defer tx.Rollback()

	var round int
	var ownerID string
	if err := tx.QueryRowContext(ctx, `SELECT r.round,r.owner_id FROM rooms r JOIN participants p ON p.room_code=r.code WHERE r.code=? AND p.id=? AND r.expires_at>?`, code, participantID, time.Now().Unix()).Scan(&round, &ownerID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return 0, 0, ErrNotFound
		}
		return 0, 0, err
	}
	if ownerID != participantID {
		return 0, 0, errors.New("only the room host can add more titles")
	}
	if round != 1 {
		return 0, 0, errors.New("more titles can only be added during the first round")
	}

	rows, err := tx.QueryContext(ctx, `SELECT movie_id FROM room_pool WHERE room_code=? AND used=0 ORDER BY position LIMIT ?`, code, count)
	if err != nil {
		return 0, 0, err
	}
	var movieIDs []string
	for rows.Next() {
		var movieID string
		if err := rows.Scan(&movieID); err != nil {
			rows.Close()
			return 0, 0, err
		}
		movieIDs = append(movieIDs, movieID)
	}
	if err := rows.Close(); err != nil {
		return 0, 0, err
	}
	if len(movieIDs) == 0 {
		return 0, 0, errors.New("no more titles are available")
	}

	var nextPosition int
	if err := tx.QueryRowContext(ctx, `SELECT COALESCE(MAX(position)+1,0) FROM room_movies WHERE room_code=?`, code).Scan(&nextPosition); err != nil {
		return 0, 0, err
	}
	stmt, err := tx.PrepareContext(ctx, `INSERT OR IGNORE INTO room_movies(room_code,movie_id,position) VALUES(?,?,?)`)
	if err != nil {
		return 0, 0, err
	}
	for offset, movieID := range movieIDs {
		if _, err := stmt.ExecContext(ctx, code, movieID, nextPosition+offset); err != nil {
			stmt.Close()
			return 0, 0, err
		}
	}
	if err := stmt.Close(); err != nil {
		return 0, 0, err
	}
	for _, movieID := range movieIDs {
		if _, err := tx.ExecContext(ctx, `UPDATE room_pool SET used=1 WHERE room_code=? AND movie_id=?`, code, movieID); err != nil {
			return 0, 0, err
		}
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM round_ready WHERE room_code=?`, code); err != nil {
		return 0, 0, err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE rooms SET next_round_requester_id='' WHERE code=?`, code); err != nil {
		return 0, 0, err
	}
	if err := reconcileRoomPhaseTx(ctx, tx, code); err != nil {
		return 0, 0, err
	}
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM room_pool WHERE room_code=? AND used=0`, code).Scan(&remaining); err != nil {
		return 0, 0, err
	}
	if err := tx.Commit(); err != nil {
		return 0, 0, err
	}
	return len(movieIDs), remaining, nil
}

// SetRoundReady updates one participant's next-round readiness and advances once everyone agrees.
func (s *Store) SetRoundReady(ctx context.Context, code, participantID string, expectedRound int, ready bool) (round, titles, readyCount, required int, advanced bool, err error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, 0, 0, 0, false, err
	}
	defer tx.Rollback()

	if err := tx.QueryRowContext(ctx, `SELECT round FROM rooms WHERE code=? AND expires_at>?`, code, time.Now().Unix()).Scan(&round); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return 0, 0, 0, 0, false, ErrNotFound
		}
		return 0, 0, 0, 0, false, err
	}
	if expectedRound > 0 && round != expectedRound {
		if round > expectedRound {
			if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM room_movies WHERE room_code=?`, code).Scan(&titles); err != nil {
				return 0, 0, 0, 0, false, err
			}
			if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM participants WHERE room_code=?`, code).Scan(&required); err != nil {
				return 0, 0, 0, 0, false, err
			}
			return round, titles, 0, required, true, nil
		}
		return 0, 0, 0, 0, false, errors.New("room round changed")
	}

	var authenticated int
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM participants WHERE room_code=? AND id=?`, code, participantID).Scan(&authenticated); err != nil {
		return 0, 0, 0, 0, false, err
	}
	if authenticated == 0 {
		return 0, 0, 0, 0, false, ErrNotFound
	}
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM participants WHERE room_code=?`, code).Scan(&required); err != nil {
		return 0, 0, 0, 0, false, err
	}
	if required < 2 {
		return 0, 0, 0, 0, false, errors.New("another round needs at least two participants")
	}

	if ready {
		var matches int
		if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM matches WHERE room_code=?`, code).Scan(&matches); err != nil {
			return 0, 0, 0, 0, false, err
		}
		if matches < 2 {
			return 0, 0, 0, 0, false, errors.New("another round requires at least two matches")
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO round_ready(room_code,round,participant_id,created_at) VALUES(?,?,?,?)
ON CONFLICT(room_code,round,participant_id) DO NOTHING`, code, round, participantID, time.Now().Unix()); err != nil {
			return 0, 0, 0, 0, false, err
		}
		if _, err := tx.ExecContext(ctx, `UPDATE rooms SET next_round_requester_id=CASE WHEN next_round_requester_id='' THEN ? ELSE next_round_requester_id END WHERE code=?`, participantID, code); err != nil {
			return 0, 0, 0, 0, false, err
		}
	} else if _, err := tx.ExecContext(ctx, `DELETE FROM round_ready WHERE room_code=? AND round=? AND participant_id=?`, code, round, participantID); err != nil {
		return 0, 0, 0, 0, false, err
	}

	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM round_ready rr
JOIN participants p ON p.id=rr.participant_id AND p.room_code=rr.room_code
WHERE rr.room_code=? AND rr.round=?`, code, round).Scan(&readyCount); err != nil {
		return 0, 0, 0, 0, false, err
	}
	if readyCount == 0 {
		if _, err := tx.ExecContext(ctx, `UPDATE rooms SET next_round_requester_id='' WHERE code=?`, code); err != nil {
			return 0, 0, 0, 0, false, err
		}
	}
	if readyCount == required {
		nextRound, nextTitles, err := advanceRoundTx(ctx, tx, code, round)
		if err != nil {
			return 0, 0, 0, 0, false, err
		}
		if err := tx.Commit(); err != nil {
			return 0, 0, 0, 0, false, err
		}
		return nextRound, nextTitles, required, required, true, nil
	}

	if err := reconcileRoomPhaseTx(ctx, tx, code); err != nil {
		return 0, 0, 0, 0, false, err
	}
	if err := tx.Commit(); err != nil {
		return 0, 0, 0, 0, false, err
	}
	return round, 0, readyCount, required, false, nil
}

// advanceRoundTx snapshots the current matches and makes them the next shuffled deck.
func advanceRoundTx(ctx context.Context, tx *sql.Tx, code string, round int) (int, int, error) {
	rows, err := tx.QueryContext(ctx, `SELECT movie_id FROM matches WHERE room_code=?`, code)
	if err != nil {
		return 0, 0, err
	}
	var movieIDs []string
	for rows.Next() {
		var movieID string
		if err := rows.Scan(&movieID); err != nil {
			rows.Close()
			return 0, 0, err
		}
		movieIDs = append(movieIDs, movieID)
	}
	if err := rows.Close(); err != nil {
		return 0, 0, err
	}
	if len(movieIDs) < 2 {
		return 0, 0, errors.New("another round requires at least two matches")
	}

	mathrand.Shuffle(len(movieIDs), func(i, j int) { movieIDs[i], movieIDs[j] = movieIDs[j], movieIDs[i] })
	if _, err := tx.ExecContext(ctx, `DELETE FROM votes WHERE room_code=?`, code); err != nil {
		return 0, 0, err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM matches WHERE room_code=?`, code); err != nil {
		return 0, 0, err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM room_movies WHERE room_code=?`, code); err != nil {
		return 0, 0, err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM round_ready WHERE room_code=?`, code); err != nil {
		return 0, 0, err
	}

	stmt, err := tx.PrepareContext(ctx, `INSERT INTO room_movies(room_code,movie_id,position) VALUES(?,?,?)`)
	if err != nil {
		return 0, 0, err
	}
	for position, movieID := range movieIDs {
		if _, err := stmt.ExecContext(ctx, code, movieID, position); err != nil {
			stmt.Close()
			return 0, 0, err
		}
	}
	if err := stmt.Close(); err != nil {
		return 0, 0, err
	}

	nextRound := round + 1
	if _, err := tx.ExecContext(ctx, `UPDATE rooms SET round=?,phase=?,next_round_requester_id='' WHERE code=? AND round=?`, nextRound, RoomPhaseSwiping, code, round); err != nil {
		return 0, 0, err
	}
	return nextRound, len(movieIDs), nil
}

// cancelNextRoundRequestTx clears every participant's pending next-round agreement.
func cancelNextRoundRequestTx(ctx context.Context, tx *sql.Tx, code string) error {
	if _, err := tx.ExecContext(ctx, `DELETE FROM round_ready WHERE room_code=?`, code); err != nil {
		return err
	}
	_, err := tx.ExecContext(ctx, `UPDATE rooms SET next_round_requester_id='' WHERE code=?`, code)
	return err
}

// cancelNextRoundIfUnavailableTx clears a request once fewer than two matches remain.
func cancelNextRoundIfUnavailableTx(ctx context.Context, tx *sql.Tx, code string) error {
	var matches int
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM matches WHERE room_code=?`, code).Scan(&matches); err != nil {
		return err
	}
	if matches >= 2 {
		return nil
	}
	return cancelNextRoundRequestTx(ctx, tx, code)
}

// reconcileRoomPhaseTx derives the persistent room phase from readiness and round progress.
func reconcileRoomPhaseTx(ctx context.Context, tx *sql.Tx, code string) error {
	var round, ready, remaining, matches int
	if err := tx.QueryRowContext(ctx, `SELECT round FROM rooms WHERE code=?`, code).Scan(&round); err != nil {
		return err
	}
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM round_ready rr
JOIN participants p ON p.id=rr.participant_id AND p.room_code=rr.room_code
WHERE rr.room_code=? AND rr.round=?`, code, round).Scan(&ready); err != nil {
		return err
	}
	if err := roundRemainingQuery(ctx, tx, code, &remaining); err != nil {
		return err
	}
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM matches WHERE room_code=?`, code).Scan(&matches); err != nil {
		return err
	}

	phase := RoomPhaseSwiping
	switch {
	case ready > 0:
		phase = RoomPhaseNextRoundRequested
	case remaining == 0 && matches == 1:
		phase = RoomPhaseFinished
	case remaining == 0:
		phase = RoomPhaseRoundComplete
	}
	_, err := tx.ExecContext(ctx, `UPDATE rooms SET phase=? WHERE code=?`, phase, code)
	return err
}

// roundRemaining returns the number of participant/title pairs still awaiting a vote.
func (s *Store) roundRemaining(ctx context.Context, code string) (int, error) {
	var remaining int
	err := roundRemainingQuery(ctx, s.db, code, &remaining)
	return remaining, err
}

type queryRower interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

// roundRemainingQuery counts outstanding votes while respecting personal genres.
func roundRemainingQuery(ctx context.Context, db queryRower, code string, remaining *int) error {
	err := db.QueryRowContext(ctx, `SELECT COUNT(*)
FROM participants p
JOIN room_movies rm ON rm.room_code=p.room_code
JOIN movies m ON m.rating_key=rm.movie_id
LEFT JOIN votes v ON v.room_code=rm.room_code AND v.movie_id=rm.movie_id AND v.participant_id=p.id
WHERE p.room_code=? AND v.movie_id IS NULL
AND (json_array_length(p.genres)=0 OR (
  p.genre_mode='all' AND NOT EXISTS (
    SELECT 1 FROM json_each(p.genres) pg
    WHERE NOT EXISTS (
      SELECT 1 FROM json_each(m.genres) mg
      WHERE lower(trim(CAST(mg.value AS TEXT)))=lower(trim(CAST(pg.value AS TEXT)))
    )
  )
) OR (
  p.genre_mode<>'all' AND EXISTS (
    SELECT 1 FROM json_each(m.genres) mg JOIN json_each(p.genres) pg
      ON lower(trim(CAST(mg.value AS TEXT)))=lower(trim(CAST(pg.value AS TEXT)))
  )
))`, code).Scan(remaining)
	return err
}

package store

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/gi8lino/screendeck/internal/media"
	roomdomain "github.com/gi8lino/screendeck/internal/room"
)

// roomState returns the state of a room for one participant.
func (s *Store) RoomState(ctx context.Context, code, participantID string) (roomdomain.State, error) {
	room, err := s.loadRoom(ctx, code)
	if err != nil {
		return roomState{}, err
	}

	me, err := s.loadRoomParticipant(ctx, code, participantID, room)
	if err != nil {
		return roomState{}, err
	}
	participants, err := s.loadRoomParticipants(ctx, code, room)
	if err != nil {
		return roomState{}, err
	}

	upcoming, err := s.nextItems(ctx, code, participantID, posterLookaheadSize+1)
	if err != nil {
		return roomState{}, err
	}
	var candidate *media.Item
	var posterLookahead []string
	if len(upcoming) > 0 {
		candidate = &upcoming[0]
		posterLookahead = make([]string, 0, len(upcoming)-1)
		for _, item := range upcoming[1:] {
			posterLookahead = append(posterLookahead, item.ID)
		}
	}
	matches, err := s.matchItems(ctx, code)
	if err != nil {
		return roomState{}, err
	}

	nextRound, err := s.loadNextRoundState(ctx, code, room, participants, matches)
	if err != nil {
		return roomState{}, err
	}
	progress, err := s.loadProgress(ctx, code, participantID)
	if err != nil {
		return roomState{}, err
	}

	remaining, err := s.roundRemaining(ctx, code)
	if err != nil {
		return roomState{}, err
	}
	roundComplete := room.Phase == roomdomain.PhaseRoundComplete || room.Phase == roomdomain.PhaseFinished || remaining == 0

	winner, err := s.loadWinner(ctx, code, room, matches)
	if err != nil {
		return roomState{}, err
	}
	moreTitles, err := s.loadMoreTitlesState(ctx, code, room.Round)
	if err != nil {
		return roomState{}, err
	}

	return roomState{
		Room:            room,
		Me:              me,
		Participants:    participants,
		Candidate:       candidate,
		PosterLookahead: posterLookahead,
		Matches:         matches,
		Winner:          winner,
		Progress:        progress,
		NextRound:       nextRound,
		RoundComplete:   roundComplete,
		MoreTitles:      moreTitles,
	}, nil
}

// loadRoom returns active room metadata.
func (s *Store) loadRoom(ctx context.Context, code string) (roomRecord, error) {
	const query = `
SELECT
  code,
  round,
  phase,
  owner_id,
  created_at,
  expires_at,
  locked
FROM rooms
WHERE code = ?
  AND expires_at > ?
`
	var room roomRecord
	var created, expires int64
	err := s.db.QueryRowContext(ctx, query, code, time.Now().Unix()).Scan(
		&room.Code,
		&room.Round,
		&room.Phase,
		&room.OwnerID,
		&created,
		&expires,
		&room.Locked,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return roomRecord{}, roomdomain.ErrNotFound
	}
	if err != nil {
		return roomRecord{}, err
	}

	room.CreatedAt = time.Unix(created, 0).UTC()
	room.ExpiresAt = time.Unix(expires, 0).UTC()
	return room, nil
}

// SetRoomLocked changes whether a room accepts new participants when requested by its host.
func (s *Store) SetRoomLocked(ctx context.Context, code, tokenHash string, locked bool) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback() // nolint:errcheck

	if _, err := authenticateRoomHostTx(ctx, tx, code, tokenHash); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, "UPDATE rooms SET locked = ? WHERE code = ?", locked, code); err != nil {
		return err
	}
	return tx.Commit()
}

// loadRoomParticipant returns one participant with current-round readiness and host state.
func (s *Store) loadRoomParticipant(
	ctx context.Context,
	code string,
	participantID string,
	room roomRecord,
) (roomParticipant, error) {
	const query = `
SELECT
  p.id,
  p.name,
  p.genres,
  p.genre_mode,
  EXISTS (
    SELECT 1
    FROM round_ready rr
    WHERE rr.room_code = p.room_code
      AND rr.round = ?
      AND rr.participant_id = p.id
  )
FROM participants p
WHERE p.id = ?
  AND p.room_code = ?
`
	var participant roomParticipant
	var genres string
	if err := s.db.QueryRowContext(ctx, query, room.Round, participantID, code).Scan(
		&participant.ID,
		&participant.Name,
		&genres,
		&participant.GenreMode,
		&participant.ReadyForNextRound,
	); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return roomParticipant{}, roomdomain.ErrNotFound
		}
		return roomParticipant{}, err
	}

	decodeGenres(genres, &participant.Genres)
	participant.IsHost = participant.ID == room.OwnerID
	return participant, nil
}

// loadRoomParticipants returns all participants with current-round readiness and host state.
func (s *Store) loadRoomParticipants(
	ctx context.Context,
	code string,
	room roomRecord,
) ([]roomParticipant, error) {
	const query = `
SELECT
  p.id,
  p.name,
  p.genres,
  p.genre_mode,
  EXISTS (
    SELECT 1
    FROM round_ready rr
    WHERE rr.room_code = p.room_code
      AND rr.round = ?
      AND rr.participant_id = p.id
  )
FROM participants p
WHERE p.room_code = ?
ORDER BY p.joined_at
`
	rows, err := s.db.QueryContext(ctx, query, room.Round, code)
	if err != nil {
		return nil, err
	}
	defer rows.Close() // nolint:errcheck

	var participants []roomParticipant
	for rows.Next() {
		participant, err := scanParticipant(rows, room.OwnerID)
		if err != nil {
			return nil, err
		}
		participants = append(participants, participant)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return participants, nil
}

// scanParticipant decodes a participant row that includes current-round readiness.
func scanParticipant(row scanner, ownerID string) (roomParticipant, error) {
	var participant roomParticipant
	var genres string
	if err := row.Scan(
		&participant.ID,
		&participant.Name,
		&genres,
		&participant.GenreMode,
		&participant.ReadyForNextRound,
	); err != nil {
		return roomParticipant{}, err
	}

	decodeGenres(genres, &participant.Genres)
	participant.IsHost = participant.ID == ownerID
	return participant, nil
}

// loadNextRoundState returns current-round readiness and requester information.
func (s *Store) loadNextRoundState(
	ctx context.Context,
	code string,
	room roomRecord,
	participants []roomParticipant,
	matches []media.Item,
) (nextRoundState, error) {
	state := nextRoundState{Required: len(participants)}

	const readyQuery = `
SELECT COUNT(*)
FROM round_ready rr
JOIN participants p
  ON p.id = rr.participant_id
 AND p.room_code = rr.room_code
WHERE rr.room_code = ?
  AND rr.round = ?
`
	if err := s.db.QueryRowContext(ctx, readyQuery, code, room.Round).Scan(&state.Ready); err != nil {
		return nextRoundState{}, err
	}

	requester, err := s.loadNextRoundRequester(ctx, code, room.OwnerID)
	if err != nil {
		return nextRoundState{}, err
	}
	state.RequestedBy = requester
	state.Available = state.Required > 1 && len(matches) > 1
	return state, nil
}

// loadNextRoundRequester returns the participant that initiated next-round consensus when still present.
func (s *Store) loadNextRoundRequester(
	ctx context.Context,
	code string,
	ownerID string,
) (*roomParticipant, error) {
	const requesterIDQuery = `
SELECT next_round_requester_id
FROM rooms
WHERE code = ?
`
	var requesterID string
	if err := s.db.QueryRowContext(ctx, requesterIDQuery, code).Scan(&requesterID); err != nil {
		return nil, err
	}
	if requesterID == "" {
		return nil, nil
	}

	const requesterQuery = `
SELECT
  id,
  name,
  genre_mode
FROM participants
WHERE room_code = ?
  AND id = ?
`
	var requester roomParticipant
	if err := s.db.QueryRowContext(ctx, requesterQuery, code, requesterID).Scan(
		&requester.ID,
		&requester.Name,
		&requester.GenreMode,
	); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}

	requester.IsHost = requester.ID == ownerID
	return &requester, nil
}

// loadProgress returns participant-specific swipe progress for the active round.
func (s *Store) loadProgress(ctx context.Context, code, participantID string) (roomProgress, error) {
	const progressQuery = `
SELECT
  COUNT(v.item_id),
  COUNT(rm.item_id)
FROM participants p
JOIN room_items rm
  ON rm.room_code = p.room_code
JOIN media_items m
  ON m.id = rm.item_id
LEFT JOIN item_votes v
  ON v.room_code = rm.room_code
 AND v.item_id = rm.item_id
 AND v.participant_id = p.id
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
`
	var progress roomProgress
	if err := s.db.QueryRowContext(ctx, progressQuery, participantID, code).Scan(
		&progress.Voted,
		&progress.Total,
	); err != nil {
		return roomProgress{}, err
	}

	const roundTotalQuery = `
SELECT COUNT(*)
FROM room_items
WHERE room_code = ?
`
	if err := s.db.QueryRowContext(ctx, roundTotalQuery, code).Scan(&progress.RoundTotal); err != nil {
		return roomProgress{}, err
	}
	progress.FilteredOut = progress.RoundTotal - progress.Total
	return progress, nil
}

// loadWinner returns final winner details when the room has converged on one match.
func (s *Store) loadWinner(
	ctx context.Context,
	code string,
	room roomRecord,
	matches []media.Item,
) (*winnerState, error) {
	if room.Phase != roomdomain.PhaseFinished || len(matches) != 1 {
		return nil, nil
	}

	const query = `
SELECT
  p.id,
  p.name,
  p.genres,
  p.genre_mode
FROM participants p
JOIN item_votes v
  ON v.participant_id = p.id
 AND v.room_code = p.room_code
WHERE p.room_code = ?
  AND v.item_id = ?
  AND v.liked = 1
ORDER BY p.joined_at
`
	rows, err := s.db.QueryContext(ctx, query, code, matches[0].ID)
	if err != nil {
		return nil, err
	}
	defer rows.Close() // nolint:errcheck

	winner := &winnerState{Item: matches[0]}
	for rows.Next() {
		participant, err := scanWinnerSupporter(rows, room.OwnerID)
		if err != nil {
			return nil, err
		}
		winner.LikedBy = append(winner.LikedBy, participant)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return winner, nil
}

// scanWinnerSupporter decodes a participant row used by final winner details.
func scanWinnerSupporter(row scanner, ownerID string) (roomParticipant, error) {
	var participant roomParticipant
	var genres string
	if err := row.Scan(&participant.ID, &participant.Name, &genres, &participant.GenreMode); err != nil {
		return roomParticipant{}, err
	}

	decodeGenres(genres, &participant.Genres)
	participant.IsHost = participant.ID == ownerID
	return participant, nil
}

// loadMoreTitlesState returns the unused first-round pool state for the room host.
func (s *Store) loadMoreTitlesState(
	ctx context.Context,
	code string,
	round int,
) (moreTitlesState, error) {
	if round != 1 {
		return moreTitlesState{}, nil
	}

	const query = `
SELECT COUNT(*)
FROM room_item_pool
WHERE room_code = ?
  AND used = 0
`
	var state moreTitlesState
	if err := s.db.QueryRowContext(ctx, query, code).Scan(&state.Available); err != nil {
		return moreTitlesState{}, err
	}
	state.CanAdd = state.Available > 0
	return state, nil
}

package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/gi8lino/screendeck/internal/plex"
)

type RoomPhase string

const (
	RoomPhaseSwiping            RoomPhase = "swiping"
	RoomPhaseNextRoundRequested RoomPhase = "next_round_requested"
	RoomPhaseRoundComplete      RoomPhase = "round_complete"
	RoomPhaseFinished           RoomPhase = "finished"
)

type Room struct {
	Code      string    `json:"code"`
	Round     int       `json:"round"`
	Phase     RoomPhase `json:"phase"`
	OwnerID   string    `json:"ownerId"`
	CreatedAt time.Time `json:"createdAt"`
	ExpiresAt time.Time `json:"expiresAt"`
}

type Participant struct {
	ID                string   `json:"id"`
	Name              string   `json:"name"`
	Genres            []string `json:"genres"`
	GenreMode         string   `json:"genreMode"`
	IsHost            bool     `json:"isHost"`
	ReadyForNextRound bool     `json:"readyForNextRound"`
}

type MoreTitlesState struct {
	Available int  `json:"available"`
	CanAdd    bool `json:"canAdd"`
}

type WinnerState struct {
	Item    plex.Item     `json:"item"`
	LikedBy []Participant `json:"likedBy"`
}

type RoomState struct {
	Room          Room            `json:"room"`
	Me            Participant     `json:"me"`
	Participants  []Participant   `json:"participants"`
	Candidate     *plex.Item      `json:"candidate,omitempty"`
	Matches       []plex.Item     `json:"matches"`
	Winner        *WinnerState    `json:"winner,omitempty"`
	Progress      Progress        `json:"progress"`
	NextRound     NextRoundState  `json:"nextRound"`
	RoundComplete bool            `json:"roundComplete"`
	MoreTitles    MoreTitlesState `json:"moreTitles"`
}

type Progress struct {
	Voted       int `json:"voted"`
	Total       int `json:"total"`
	RoundTotal  int `json:"roundTotal"`
	FilteredOut int `json:"filteredOut"`
}

type NextRoundState struct {
	Ready       int          `json:"ready"`
	Required    int          `json:"required"`
	Available   bool         `json:"available"`
	RequestedBy *Participant `json:"requestedBy,omitempty"`
}

func (s *Store) CreateRoom(ctx context.Context, room Room, participant Participant, tokenHash string, movieIDs, poolIDs []string) error {
	if room.Round <= 0 {
		room.Round = 1
	}
	if room.Phase == "" {
		room.Phase = RoomPhaseSwiping
	}
	if room.OwnerID == "" {
		room.OwnerID = participant.ID
	}
	if participant.Genres == nil {
		participant.Genres = []string{}
	}
	if participant.GenreMode == "" {
		participant.GenreMode = "any"
	}
	participantGenres, err := json.Marshal(participant.Genres)
	if err != nil {
		return fmt.Errorf("encode participant genres: %w", err)
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `INSERT INTO rooms(code,round,phase,owner_id,created_at,expires_at) VALUES(?,?,?,?,?,?)`, room.Code, room.Round, room.Phase, room.OwnerID, room.CreatedAt.Unix(), room.ExpiresAt.Unix()); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO participants(id,room_code,name,genres,genre_mode,token_hash,joined_at) VALUES(?,?,?,?,?,?,?)`, participant.ID, room.Code, participant.Name, string(participantGenres), participant.GenreMode, tokenHash, time.Now().Unix()); err != nil {
		return err
	}
	stmt, err := tx.PrepareContext(ctx, `INSERT INTO room_movies(room_code,movie_id,position) VALUES(?,?,?)`)
	if err != nil {
		return err
	}
	defer stmt.Close()
	for position, movieID := range movieIDs {
		if _, err := stmt.ExecContext(ctx, room.Code, movieID, position); err != nil {
			return err
		}
	}
	poolStmt, err := tx.PrepareContext(ctx, `INSERT INTO room_pool(room_code,movie_id,position,used) VALUES(?,?,?,?)`)
	if err != nil {
		return err
	}
	used := make(map[string]struct{}, len(movieIDs))
	for _, movieID := range movieIDs {
		used[movieID] = struct{}{}
	}
	for position, movieID := range poolIDs {
		_, active := used[movieID]
		if _, err := poolStmt.ExecContext(ctx, room.Code, movieID, position, active); err != nil {
			poolStmt.Close()
			return err
		}
	}
	if err := poolStmt.Close(); err != nil {
		return err
	}
	if err := reconcileRoomPhaseTx(ctx, tx, room.Code); err != nil {
		return err
	}
	return tx.Commit()
}

// JoinRoom persists a participant in an active room.
func (s *Store) JoinRoom(ctx context.Context, code string, participant Participant, tokenHash string) error {
	if participant.Genres == nil {
		participant.Genres = []string{}
	}
	if participant.GenreMode == "" {
		participant.GenreMode = "any"
	}
	genres, err := json.Marshal(participant.Genres)
	if err != nil {
		return fmt.Errorf("encode participant genres: %w", err)
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	result, err := tx.ExecContext(ctx, `INSERT INTO participants(id,room_code,name,genres,genre_mode,token_hash,joined_at)
SELECT ?,code,?,?,?,?,? FROM rooms WHERE code=? AND expires_at>?`, participant.ID, participant.Name, string(genres), participant.GenreMode, tokenHash, time.Now().Unix(), code, time.Now().Unix())
	if err != nil {
		return err
	}
	if count, _ := result.RowsAffected(); count == 0 {
		return ErrNotFound
	}
	// Membership changed, so any pending next-round agreement must be renewed
	// by the new set of active participants.
	if err := cancelNextRoundRequestTx(ctx, tx, code); err != nil {
		return err
	}
	// A newly joined participant has not voted yet, so prior matches are no
	// longer unanimous until that participant also likes them.
	if _, err := tx.ExecContext(ctx, `DELETE FROM matches
WHERE room_code=? AND (SELECT COUNT(*) FROM votes WHERE room_code=? AND movie_id=matches.movie_id AND liked=1)
  < (SELECT COUNT(*) FROM participants WHERE room_code=?)`, code, code, code); err != nil {
		return err
	}
	if err := reconcileRoomPhaseTx(ctx, tx, code); err != nil {
		return err
	}
	return tx.Commit()
}

// ParticipantByToken authenticates and returns a room participant.
func (s *Store) ParticipantByToken(ctx context.Context, code, tokenHash string) (Participant, error) {
	var participant Participant
	var genres string
	err := s.db.QueryRowContext(ctx, `SELECT id,name,genres,genre_mode FROM participants WHERE room_code=? AND token_hash=?`, code, tokenHash).Scan(&participant.ID, &participant.Name, &genres, &participant.GenreMode)
	if errors.Is(err, sql.ErrNoRows) {
		return participant, ErrNotFound
	}
	if err != nil {
		return participant, err
	}
	decodeGenres(genres, &participant.Genres)
	return participant, nil
}

// RoomGenres returns the genres represented by the current room deck.
func (s *Store) RoomGenres(ctx context.Context, code string) ([]string, error) {
	var exists int
	if err := s.db.QueryRowContext(ctx, `SELECT 1 FROM rooms WHERE code=? AND expires_at>?`, code, time.Now().Unix()).Scan(&exists); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	rows, err := s.db.QueryContext(ctx, `SELECT DISTINCT CAST(j.value AS TEXT)
FROM (
  SELECT room_code,movie_id FROM room_pool WHERE room_code=?
  UNION
  SELECT room_code,movie_id FROM room_movies WHERE room_code=?
) rp
JOIN movies m ON m.rating_key=rp.movie_id
JOIN json_each(m.genres) j
WHERE trim(CAST(j.value AS TEXT))<>''
ORDER BY CAST(j.value AS TEXT) COLLATE NOCASE`, code, code)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var genres []string
	for rows.Next() {
		var genre string
		if err := rows.Scan(&genre); err != nil {
			return nil, err
		}
		genres = append(genres, genre)
	}
	return genres, rows.Err()
}

// RoomState returns the state of a room for one participant.
func (s *Store) RoomState(ctx context.Context, code, participantID string) (RoomState, error) {
	var state RoomState
	var created, expires int64
	err := s.db.QueryRowContext(ctx, `SELECT code,round,phase,owner_id,created_at,expires_at FROM rooms WHERE code=? AND expires_at>?`, code, time.Now().Unix()).Scan(&state.Room.Code, &state.Room.Round, &state.Room.Phase, &state.Room.OwnerID, &created, &expires)
	if errors.Is(err, sql.ErrNoRows) {
		return state, ErrNotFound
	}
	if err != nil {
		return state, err
	}
	state.Room.CreatedAt, state.Room.ExpiresAt = time.Unix(created, 0).UTC(), time.Unix(expires, 0).UTC()
	var meGenres string
	if err := s.db.QueryRowContext(ctx, `SELECT p.id,p.name,p.genres,p.genre_mode,EXISTS(
  SELECT 1 FROM round_ready rr WHERE rr.room_code=p.room_code AND rr.round=? AND rr.participant_id=p.id
) FROM participants p WHERE p.id=? AND p.room_code=?`, state.Room.Round, participantID, code).Scan(&state.Me.ID, &state.Me.Name, &meGenres, &state.Me.GenreMode, &state.Me.ReadyForNextRound); err != nil {
		return state, ErrNotFound
	}
	decodeGenres(meGenres, &state.Me.Genres)
	state.Me.IsHost = state.Me.ID == state.Room.OwnerID
	rows, err := s.db.QueryContext(ctx, `SELECT p.id,p.name,p.genres,p.genre_mode,EXISTS(
  SELECT 1 FROM round_ready rr WHERE rr.room_code=p.room_code AND rr.round=? AND rr.participant_id=p.id
) FROM participants p WHERE p.room_code=? ORDER BY p.joined_at`, state.Room.Round, code)
	if err != nil {
		return state, err
	}
	for rows.Next() {
		var p Participant
		var genres string
		if err := rows.Scan(&p.ID, &p.Name, &genres, &p.GenreMode, &p.ReadyForNextRound); err != nil {
			rows.Close()
			return state, err
		}
		decodeGenres(genres, &p.Genres)
		p.IsHost = p.ID == state.Room.OwnerID
		state.Participants = append(state.Participants, p)
	}
	if err := rows.Close(); err != nil {
		return state, err
	}

	state.Candidate, err = s.nextMovie(ctx, code, participantID)
	if err != nil {
		return state, err
	}
	state.Matches, err = s.matchMovies(ctx, code)
	if err != nil {
		return state, err
	}
	state.NextRound.Required = len(state.Participants)
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM round_ready rr
JOIN participants p ON p.id=rr.participant_id AND p.room_code=rr.room_code
WHERE rr.room_code=? AND rr.round=?`, code, state.Room.Round).Scan(&state.NextRound.Ready); err != nil {
		return state, err
	}
	var requester Participant
	var requesterID string
	if err := s.db.QueryRowContext(ctx, `SELECT next_round_requester_id FROM rooms WHERE code=?`, code).Scan(&requesterID); err != nil {
		return state, err
	}
	if requesterID != "" {
		if err := s.db.QueryRowContext(ctx, `SELECT id,name,genre_mode FROM participants WHERE room_code=? AND id=?`, code, requesterID).Scan(&requester.ID, &requester.Name, &requester.GenreMode); err == nil {
			requester.IsHost = requester.ID == state.Room.OwnerID
			state.NextRound.RequestedBy = &requester
		} else if !errors.Is(err, sql.ErrNoRows) {
			return state, err
		}
	}
	state.NextRound.Available = state.NextRound.Required > 1 && len(state.Matches) > 1
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(v.movie_id),COUNT(rm.movie_id)
FROM participants p
JOIN room_movies rm ON rm.room_code=p.room_code
JOIN movies m ON m.rating_key=rm.movie_id
LEFT JOIN votes v ON v.room_code=rm.room_code AND v.movie_id=rm.movie_id AND v.participant_id=p.id
WHERE p.id=? AND p.room_code=?
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
))`, participantID, code).Scan(&state.Progress.Voted, &state.Progress.Total); err != nil {
		return state, err
	}
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM room_movies WHERE room_code=?`, code).Scan(&state.Progress.RoundTotal); err != nil {
		return state, err
	}
	state.Progress.FilteredOut = state.Progress.RoundTotal - state.Progress.Total
	remaining, err := s.roundRemaining(ctx, code)
	if err != nil {
		return state, err
	}
	state.RoundComplete = state.Room.Phase == RoomPhaseRoundComplete || state.Room.Phase == RoomPhaseFinished
	if remaining == 0 && !state.RoundComplete {
		state.RoundComplete = true
	}
	if state.Room.Phase == RoomPhaseFinished && len(state.Matches) == 1 {
		winner := &WinnerState{Item: state.Matches[0]}
		rows, err := s.db.QueryContext(ctx, `SELECT p.id,p.name,p.genres,p.genre_mode
FROM participants p JOIN votes v ON v.participant_id=p.id AND v.room_code=p.room_code
WHERE p.room_code=? AND v.movie_id=? AND v.liked=1 ORDER BY p.joined_at`, code, state.Matches[0].RatingKey)
		if err != nil {
			return state, err
		}
		for rows.Next() {
			var participant Participant
			var genres string
			if err := rows.Scan(&participant.ID, &participant.Name, &genres, &participant.GenreMode); err != nil {
				rows.Close()
				return state, err
			}
			decodeGenres(genres, &participant.Genres)
			participant.IsHost = participant.ID == state.Room.OwnerID
			winner.LikedBy = append(winner.LikedBy, participant)
		}
		if err := rows.Close(); err != nil {
			return state, err
		}
		state.Winner = winner
	}
	if state.Room.Round == 1 {
		if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM room_pool WHERE room_code=? AND used=0`, code).Scan(&state.MoreTitles.Available); err != nil {
			return state, err
		}
		state.MoreTitles.CanAdd = state.MoreTitles.Available > 0
	}
	return state, nil
}

// nextMovie returns the next unvoted media item for a participant.
func (s *Store) nextMovie(ctx context.Context, code, participantID string) (*plex.Item, error) {
	row := s.db.QueryRowContext(ctx, `SELECT m.rating_key,m.library_key,m.media_type,m.guid,m.title,m.year,m.summary,m.duration,m.rating,m.genres,m.viewed,m.added_at
FROM participants p
JOIN room_movies rm ON rm.room_code=p.room_code
JOIN movies m ON m.rating_key=rm.movie_id
LEFT JOIN votes v ON v.room_code=rm.room_code AND v.movie_id=rm.movie_id AND v.participant_id=p.id
WHERE p.id=? AND p.room_code=? AND v.movie_id IS NULL
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
))
ORDER BY rm.position LIMIT 1`, participantID, code)
	movie, err := scanMovie(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return movie, err
}

// matchMovies returns media liked by every active participant.
func (s *Store) matchMovies(ctx context.Context, code string) ([]plex.Item, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT m.rating_key,m.library_key,m.media_type,m.guid,m.title,m.year,m.summary,m.duration,m.rating,m.genres,m.viewed,m.added_at
FROM matches x JOIN movies m ON m.rating_key=x.movie_id WHERE x.room_code=? ORDER BY x.matched_at DESC,m.title`, code)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var movies []plex.Item
	for rows.Next() {
		movie, err := scanMovie(rows)
		if err != nil {
			return nil, err
		}
		movies = append(movies, *movie)
	}
	return movies, rows.Err()
}

type scanner interface{ Scan(...any) error }

// scanMovie decodes a media item from a database row.
func scanMovie(row scanner) (*plex.Item, error) {
	var movie plex.Item
	var genres string
	if err := row.Scan(&movie.RatingKey, &movie.Library, &movie.Type, &movie.GUID, &movie.Title, &movie.Year, &movie.Summary, &movie.Duration, &movie.Rating, &genres, &movie.Viewed, &movie.AddedAt); err != nil {
		return nil, err
	}
	decodeGenres(genres, &movie.Genres)
	return &movie, nil
}

// decodeGenres decodes a stored JSON genre list and falls back to an empty list.
func decodeGenres(raw string, target *[]string) {
	if json.Unmarshal([]byte(raw), target) != nil || *target == nil {
		*target = []string{}
	}
}

// Vote persists a participant vote and reports a unanimous match.
func (s *Store) Vote(ctx context.Context, code, participantID, movieID string, liked bool) (bool, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return false, err
	}
	defer tx.Rollback()
	result, err := tx.ExecContext(ctx, `INSERT INTO votes(room_code,participant_id,movie_id,liked,created_at)
SELECT ?,?,?,?,? WHERE EXISTS(SELECT 1 FROM participants p JOIN movies m ON m.rating_key=? WHERE p.id=? AND p.room_code=?
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
)))
AND EXISTS(SELECT 1 FROM room_movies WHERE room_code=? AND movie_id=?)
ON CONFLICT(room_code,participant_id,movie_id) DO UPDATE SET liked=excluded.liked,created_at=excluded.created_at`,
		code, participantID, movieID, liked, time.Now().Unix(), movieID, participantID, code, code, movieID)
	if err != nil {
		return false, err
	}
	if count, _ := result.RowsAffected(); count == 0 {
		return false, ErrNotFound
	}
	var participants, likes int
	if err := tx.QueryRowContext(ctx, `SELECT
(SELECT COUNT(*) FROM participants WHERE room_code=?),
(SELECT COUNT(*) FROM votes WHERE room_code=? AND movie_id=? AND liked=1)`, code, code, movieID).Scan(&participants, &likes); err != nil {
		return false, err
	}
	matched := participants > 1 && likes == participants
	if matched {
		if _, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO matches(room_code,movie_id,matched_at) VALUES(?,?,?)`, code, movieID, time.Now().Unix()); err != nil {
			return false, err
		}
	} else if _, err := tx.ExecContext(ctx, `DELETE FROM matches WHERE room_code=? AND movie_id=?`, code, movieID); err != nil {
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

func (s *Store) LeaveRoom(ctx context.Context, code, tokenHash string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var leavingID string
	if err := tx.QueryRowContext(ctx, `SELECT id FROM participants WHERE room_code=? AND token_hash=?`, code, tokenHash).Scan(&leavingID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ErrNotFound
		}
		return err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM participants WHERE room_code=? AND id=?`, code, leavingID); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE rooms SET owner_id=COALESCE((
  SELECT id FROM participants WHERE room_code=? ORDER BY joined_at,id LIMIT 1
),'') WHERE code=? AND owner_id=?`, code, code, leavingID); err != nil {
		return err
	}
	// A departure can make previously cast likes unanimous. Completed matches
	// are intentionally retained even if the room membership later changes.
	_, err = tx.ExecContext(ctx, `INSERT OR IGNORE INTO matches(room_code,movie_id,matched_at)
SELECT rm.room_code,rm.movie_id,? FROM room_movies rm
WHERE rm.room_code=?
AND (SELECT COUNT(*) FROM participants WHERE room_code=?)>1
AND (SELECT COUNT(*) FROM votes WHERE room_code=? AND movie_id=rm.movie_id AND liked=1)
  =(SELECT COUNT(*) FROM participants WHERE room_code=?)`, time.Now().Unix(), code, code, code, code)
	if err != nil {
		return err
	}
	if err := cancelNextRoundRequestTx(ctx, tx, code); err != nil {
		return err
	}
	if err := reconcileRoomPhaseTx(ctx, tx, code); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) DeleteExpired(ctx context.Context) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM rooms WHERE expires_at<=?`, time.Now().Unix())
	return err
}

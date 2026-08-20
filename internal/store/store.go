package store

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/ed25519"
	"crypto/rand"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	mathrand "math/rand/v2"
	"os"
	"path/filepath"
	"strings"
	"time"

	_ "modernc.org/sqlite"

	"github.com/gi8lino/screendeck/internal/plex"
)

var ErrNotFound = errors.New("not found")

type Store struct {
	db     *sql.DB
	cipher cipher.AEAD
}

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
	CreatedAt time.Time `json:"createdAt"`
	ExpiresAt time.Time `json:"expiresAt"`
}

type Participant struct {
	ID                string   `json:"id"`
	Name              string   `json:"name"`
	Genres            []string `json:"genres"`
	GenreMode         string   `json:"genreMode"`
	ReadyForNextRound bool     `json:"readyForNextRound"`
}

type MoreTitlesState struct {
	Available int  `json:"available"`
	CanAdd    bool `json:"canAdd"`
}

type RoomState struct {
	Room          Room            `json:"room"`
	Me            Participant     `json:"me"`
	Participants  []Participant   `json:"participants"`
	Candidate     *plex.Item      `json:"candidate,omitempty"`
	Matches       []plex.Item     `json:"matches"`
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

// Open opens the database and applies its schema migrations.
func Open(path string, configuredKeyPath ...string) (*Store, error) {
	if path != ":memory:" {
		if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
			return nil, fmt.Errorf("create database directory: %w", err)
		}
	}
	db, err := sql.Open("sqlite", path+"?_pragma=foreign_keys(1)&_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)")
	if err != nil {
		return nil, fmt.Errorf("open database: %w", err)
	}
	db.SetMaxOpenConns(1)
	keyPath := ""
	if len(configuredKeyPath) > 0 {
		keyPath = configuredKeyPath[0]
	}
	key, err := loadEncryptionKey(path, keyPath)
	if err != nil {
		db.Close()
		return nil, err
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		db.Close()
		return nil, err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		db.Close()
		return nil, err
	}
	store := &Store{db: db, cipher: aead}
	if err := store.migrate(context.Background()); err != nil {
		db.Close()
		return nil, err
	}
	return store, nil
}

// Close closes the underlying database.
func (s *Store) Close() error { return s.db.Close() }

// loadEncryptionKey loads or creates the database encryption key.
func loadEncryptionKey(databasePath, configuredPath string) ([]byte, error) {
	if databasePath == ":memory:" {
		key := make([]byte, 32)
		_, err := rand.Read(key)
		return key, err
	}
	keyPath := configuredPath
	if keyPath == "" {
		keyPath = filepath.Join(filepath.Dir(databasePath), "auth.key")
	}
	if err := os.MkdirAll(filepath.Dir(keyPath), 0o750); err != nil {
		return nil, fmt.Errorf("create authentication key directory: %w", err)
	}
	key, err := os.ReadFile(keyPath)
	if err == nil {
		if len(key) != 32 {
			return nil, errors.New("authentication key must contain exactly 32 bytes")
		}
		return key, nil
	}
	if !errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("read authentication key: %w", err)
	}
	key = make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		return nil, err
	}
	file, err := os.OpenFile(keyPath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if errors.Is(err, os.ErrExist) {
		return loadEncryptionKey(databasePath, keyPath)
	}
	if err != nil {
		return nil, fmt.Errorf("create authentication key: %w", err)
	}
	if _, err := file.Write(key); err != nil {
		file.Close()
		return nil, fmt.Errorf("write authentication key: %w", err)
	}
	if err := file.Close(); err != nil {
		return nil, err
	}
	return key, nil
}

// seal encrypts sensitive data for persistent storage.
func (s *Store) seal(value []byte) ([]byte, error) {
	nonce := make([]byte, s.cipher.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return nil, err
	}
	return s.cipher.Seal(nonce, nonce, value, nil), nil
}

// open decrypts sensitive data loaded from persistent storage.
func (s *Store) open(value []byte) ([]byte, error) {
	if len(value) < s.cipher.NonceSize() {
		return nil, errors.New("encrypted authentication value is truncated")
	}
	nonce := value[:s.cipher.NonceSize()]
	return s.cipher.Open(nil, nonce, value[s.cipher.NonceSize():], nil)
}

// SavePlexAuth encrypts and persists Plex authentication state.
func (s *Store) SavePlexAuth(ctx context.Context, state plex.AuthState) error {
	privateKey, err := s.seal(state.PrivateKey)
	if err != nil {
		return err
	}
	userToken, err := s.seal([]byte(state.UserToken))
	if err != nil {
		return err
	}
	serverToken, err := s.seal([]byte(state.ServerToken))
	if err != nil {
		return err
	}
	_, err = s.db.ExecContext(ctx, `INSERT INTO plex_auth
(id,client_id,key_id,private_key,user_token,token_expires_at,server_id,server_name,server_url,server_token,updated_at)
VALUES(1,?,?,?,?,?,?,?,?,?,?) ON CONFLICT(id) DO UPDATE SET
client_id=excluded.client_id,key_id=excluded.key_id,private_key=excluded.private_key,user_token=excluded.user_token,
token_expires_at=excluded.token_expires_at,server_id=excluded.server_id,server_name=excluded.server_name,
server_url=excluded.server_url,server_token=excluded.server_token,updated_at=excluded.updated_at`,
		state.ClientID, state.KeyID, privateKey, userToken, state.TokenExpiresAt.Unix(), state.ServerID,
		state.ServerName, state.ServerURL, serverToken, time.Now().Unix())
	if err != nil {
		return fmt.Errorf("save Plex authentication: %w", err)
	}
	return nil
}

// LoadPlexAuth loads and decrypts persisted Plex authentication state.
func (s *Store) LoadPlexAuth(ctx context.Context) (plex.AuthState, error) {
	var state plex.AuthState
	var privateKey, userToken, serverToken []byte
	var expires int64
	err := s.db.QueryRowContext(ctx, `SELECT client_id,key_id,private_key,user_token,token_expires_at,server_id,server_name,server_url,server_token FROM plex_auth WHERE id=1`).
		Scan(&state.ClientID, &state.KeyID, &privateKey, &userToken, &expires, &state.ServerID, &state.ServerName, &state.ServerURL, &serverToken)
	if errors.Is(err, sql.ErrNoRows) {
		return state, plex.ErrAuthNotFound
	}
	if err != nil {
		return state, err
	}
	plainPrivateKey, err := s.open(privateKey)
	if err != nil {
		return state, fmt.Errorf("decrypt Plex private key: %w", err)
	}
	if len(plainPrivateKey) != 0 && len(plainPrivateKey) != ed25519.PrivateKeySize {
		return state, errors.New("stored Plex private key has an invalid size")
	}
	plainUserToken, err := s.open(userToken)
	if err != nil {
		return state, fmt.Errorf("decrypt Plex user token: %w", err)
	}
	plainServerToken, err := s.open(serverToken)
	if err != nil {
		return state, fmt.Errorf("decrypt Plex server token: %w", err)
	}
	if len(plainPrivateKey) == ed25519.PrivateKeySize {
		state.Method = plex.AuthMethodJWT
		state.PrivateKey = ed25519.PrivateKey(plainPrivateKey)
	} else {
		state.Method = plex.AuthMethodLegacy
	}
	state.UserToken = string(plainUserToken)
	state.ServerToken = string(plainServerToken)
	state.TokenExpiresAt = time.Unix(expires, 0).UTC()
	return state, nil
}

// migrate creates and upgrades the database schema.
func (s *Store) migrate(ctx context.Context) error {
	const schema = `
CREATE TABLE IF NOT EXISTS libraries (
  key TEXT PRIMARY KEY,
  title TEXT NOT NULL,
  synced_at INTEGER NOT NULL
);
CREATE TABLE IF NOT EXISTS movies (
  rating_key TEXT PRIMARY KEY,
  library_key TEXT NOT NULL,
  media_type TEXT NOT NULL DEFAULT 'movie',
  guid TEXT NOT NULL DEFAULT '',
  title TEXT NOT NULL,
  year INTEGER NOT NULL DEFAULT 0,
  summary TEXT NOT NULL DEFAULT '',
  duration INTEGER NOT NULL DEFAULT 0,
  rating REAL NOT NULL DEFAULT 0,
  thumb TEXT NOT NULL DEFAULT '',
  genres TEXT NOT NULL DEFAULT '[]',
  viewed INTEGER NOT NULL DEFAULT 0,
  added_at INTEGER NOT NULL DEFAULT 0
);
CREATE INDEX IF NOT EXISTS movies_library_idx ON movies(library_key);
CREATE TABLE IF NOT EXISTS rooms (
  code TEXT PRIMARY KEY,
  round INTEGER NOT NULL DEFAULT 1,
  phase TEXT NOT NULL DEFAULT 'swiping',
  next_round_requester_id TEXT NOT NULL DEFAULT '',
  created_at INTEGER NOT NULL,
  expires_at INTEGER NOT NULL
);
CREATE TABLE IF NOT EXISTS room_movies (
  room_code TEXT NOT NULL REFERENCES rooms(code) ON DELETE CASCADE,
  movie_id TEXT NOT NULL REFERENCES movies(rating_key),
  position INTEGER NOT NULL,
  PRIMARY KEY(room_code, movie_id)
);
CREATE INDEX IF NOT EXISTS room_movies_order_idx ON room_movies(room_code, position);
CREATE TABLE IF NOT EXISTS room_pool (
  room_code TEXT NOT NULL REFERENCES rooms(code) ON DELETE CASCADE,
  movie_id TEXT NOT NULL REFERENCES movies(rating_key),
  position INTEGER NOT NULL,
  used INTEGER NOT NULL DEFAULT 0,
  PRIMARY KEY(room_code, movie_id)
);
CREATE INDEX IF NOT EXISTS room_pool_order_idx ON room_pool(room_code, used, position);
CREATE TABLE IF NOT EXISTS participants (
  id TEXT PRIMARY KEY,
  room_code TEXT NOT NULL REFERENCES rooms(code) ON DELETE CASCADE,
  name TEXT NOT NULL,
  genres TEXT NOT NULL DEFAULT '[]',
  genre_mode TEXT NOT NULL DEFAULT 'any',
  token_hash TEXT NOT NULL UNIQUE,
  joined_at INTEGER NOT NULL
);
CREATE INDEX IF NOT EXISTS participants_room_idx ON participants(room_code);
CREATE TABLE IF NOT EXISTS round_ready (
  room_code TEXT NOT NULL REFERENCES rooms(code) ON DELETE CASCADE,
  round INTEGER NOT NULL,
  participant_id TEXT NOT NULL REFERENCES participants(id) ON DELETE CASCADE,
  created_at INTEGER NOT NULL,
  PRIMARY KEY(room_code, round, participant_id)
);
CREATE TABLE IF NOT EXISTS votes (
  room_code TEXT NOT NULL REFERENCES rooms(code) ON DELETE CASCADE,
  participant_id TEXT NOT NULL REFERENCES participants(id) ON DELETE CASCADE,
  movie_id TEXT NOT NULL REFERENCES movies(rating_key),
  liked INTEGER NOT NULL,
  created_at INTEGER NOT NULL,
  PRIMARY KEY(room_code, participant_id, movie_id)
);
CREATE TABLE IF NOT EXISTS matches (
  room_code TEXT NOT NULL REFERENCES rooms(code) ON DELETE CASCADE,
  movie_id TEXT NOT NULL REFERENCES movies(rating_key),
  matched_at INTEGER NOT NULL,
  PRIMARY KEY(room_code, movie_id)
);
CREATE TABLE IF NOT EXISTS plex_auth (
  id INTEGER PRIMARY KEY CHECK(id=1),
  client_id TEXT NOT NULL,
  key_id TEXT NOT NULL,
  private_key BLOB NOT NULL,
  user_token BLOB NOT NULL,
  token_expires_at INTEGER NOT NULL,
  server_id TEXT NOT NULL,
  server_name TEXT NOT NULL,
  server_url TEXT NOT NULL,
  server_token BLOB NOT NULL,
  updated_at INTEGER NOT NULL
);`
	if _, err := s.db.ExecContext(ctx, schema); err != nil {
		return fmt.Errorf("migrate database: %w", err)
	}
	if err := s.ensureColumn(ctx, "movies", "media_type", "TEXT NOT NULL DEFAULT 'movie'"); err != nil {
		return err
	}
	if err := s.ensureColumn(ctx, "movies", "added_at", "INTEGER NOT NULL DEFAULT 0"); err != nil {
		return err
	}
	if err := s.ensureColumn(ctx, "rooms", "round", "INTEGER NOT NULL DEFAULT 1"); err != nil {
		return err
	}
	if err := s.ensureColumn(ctx, "rooms", "phase", "TEXT NOT NULL DEFAULT 'swiping'"); err != nil {
		return err
	}
	if err := s.ensureColumn(ctx, "rooms", "next_round_requester_id", "TEXT NOT NULL DEFAULT ''"); err != nil {
		return err
	}
	if err := s.ensureColumn(ctx, "participants", "genres", "TEXT NOT NULL DEFAULT '[]'"); err != nil {
		return err
	}
	if err := s.ensureColumn(ctx, "participants", "genre_mode", "TEXT NOT NULL DEFAULT 'any'"); err != nil {
		return err
	}
	return nil
}

// ensureColumn adds a database column when it is absent.
func (s *Store) ensureColumn(ctx context.Context, table, column, definition string) error {
	rows, err := s.db.QueryContext(ctx, `PRAGMA table_info(`+table+`)`)
	if err != nil {
		return err
	}
	found := false
	for rows.Next() {
		var cid int
		var name, dataType string
		var notNull, primaryKey int
		var defaultValue any
		if err := rows.Scan(&cid, &name, &dataType, &notNull, &defaultValue, &primaryKey); err != nil {
			rows.Close()
			return err
		}
		if name == column {
			found = true
		}
	}
	if err := rows.Close(); err != nil {
		return err
	}
	if found {
		return nil
	}
	if _, err := s.db.ExecContext(ctx, `ALTER TABLE `+table+` ADD COLUMN `+column+` `+definition); err != nil {
		return fmt.Errorf("add %s.%s: %w", table, column, err)
	}
	return nil
}

// SaveLibrary replaces cached metadata for a Plex library.
func (s *Store) SaveLibrary(ctx context.Context, library plex.Library, items []plex.Item) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `INSERT INTO libraries(key,title,synced_at) VALUES(?,?,?)
ON CONFLICT(key) DO UPDATE SET title=excluded.title,synced_at=excluded.synced_at`, library.Key, library.Title, time.Now().Unix()); err != nil {
		return fmt.Errorf("save library: %w", err)
	}
	stmt, err := tx.PrepareContext(ctx, `INSERT INTO movies
(rating_key,library_key,media_type,guid,title,year,summary,duration,rating,thumb,genres,viewed,added_at)
VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?) ON CONFLICT(rating_key) DO UPDATE SET
library_key=excluded.library_key,media_type=excluded.media_type,guid=excluded.guid,title=excluded.title,year=excluded.year,
summary=excluded.summary,duration=excluded.duration,rating=excluded.rating,thumb=excluded.thumb,
genres=excluded.genres,viewed=excluded.viewed,added_at=excluded.added_at`)
	if err != nil {
		return err
	}
	defer stmt.Close()
	for _, item := range items {
		genres, _ := json.Marshal(item.Genres)
		if _, err := stmt.ExecContext(ctx, item.RatingKey, item.Library, item.Type, item.GUID, item.Title, item.Year,
			item.Summary, item.Duration, item.Rating, item.Thumb, string(genres), item.Viewed, item.AddedAt); err != nil {
			return fmt.Errorf("save item %q: %w", item.Title, err)
		}
	}
	return tx.Commit()
}

// CreateRoom persists a room, its owner, and eligible media items.
func (s *Store) CreateRoom(ctx context.Context, room Room, participant Participant, tokenHash string, movieIDs, poolIDs []string) error {
	if room.Round <= 0 {
		room.Round = 1
	}
	if room.Phase == "" {
		room.Phase = RoomPhaseSwiping
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
	if _, err := tx.ExecContext(ctx, `INSERT INTO rooms(code,round,phase,created_at,expires_at) VALUES(?,?,?,?,?)`, room.Code, room.Round, room.Phase, room.CreatedAt.Unix(), room.ExpiresAt.Unix()); err != nil {
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
	err := s.db.QueryRowContext(ctx, `SELECT code,round,phase,created_at,expires_at FROM rooms WHERE code=? AND expires_at>?`, code, time.Now().Unix()).Scan(&state.Room.Code, &state.Room.Round, &state.Room.Phase, &created, &expires)
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

// AddMoreTitles activates unused titles from the original first-round pool.
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
	if err := tx.QueryRowContext(ctx, `SELECT r.round FROM rooms r JOIN participants p ON p.room_code=r.code WHERE r.code=? AND p.id=? AND r.expires_at>?`, code, participantID, time.Now().Unix()).Scan(&round); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return 0, 0, ErrNotFound
		}
		return 0, 0, err
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

// LeaveRoom deactivates an authenticated room participant.
func (s *Store) LeaveRoom(ctx context.Context, code, tokenHash string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	result, err := tx.ExecContext(ctx, `DELETE FROM participants WHERE room_code=? AND token_hash=?`, code, tokenHash)
	if err != nil {
		return err
	}
	if count, _ := result.RowsAffected(); count == 0 {
		return ErrNotFound
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

// MoviePoster returns the poster path for a stored media item.
func (s *Store) MoviePoster(ctx context.Context, ratingKey string) (string, error) {
	var thumb string
	if err := s.db.QueryRowContext(ctx, `SELECT thumb FROM movies WHERE rating_key=?`, ratingKey).Scan(&thumb); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return "", ErrNotFound
		}
		return "", err
	}
	if strings.TrimSpace(thumb) == "" {
		return "", ErrNotFound
	}
	return thumb, nil
}

// DeleteExpired removes rooms past their expiration time.
func (s *Store) DeleteExpired(ctx context.Context) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM rooms WHERE expires_at<=?`, time.Now().Unix())
	return err
}

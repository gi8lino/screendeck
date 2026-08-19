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

type Room struct {
	Code      string    `json:"code"`
	CreatedAt time.Time `json:"createdAt"`
	ExpiresAt time.Time `json:"expiresAt"`
}

type Participant struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type RoomState struct {
	Room         Room          `json:"room"`
	Me           Participant   `json:"me"`
	Participants []Participant `json:"participants"`
	Candidate    *plex.Item    `json:"candidate,omitempty"`
	Matches      []plex.Item   `json:"matches"`
	Progress     Progress      `json:"progress"`
}

type Progress struct {
	Voted int `json:"voted"`
	Total int `json:"total"`
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
  viewed INTEGER NOT NULL DEFAULT 0
);
CREATE INDEX IF NOT EXISTS movies_library_idx ON movies(library_key);
CREATE TABLE IF NOT EXISTS rooms (
  code TEXT PRIMARY KEY,
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
CREATE TABLE IF NOT EXISTS participants (
  id TEXT PRIMARY KEY,
  room_code TEXT NOT NULL REFERENCES rooms(code) ON DELETE CASCADE,
  name TEXT NOT NULL,
  token_hash TEXT NOT NULL UNIQUE,
  joined_at INTEGER NOT NULL
);
CREATE INDEX IF NOT EXISTS participants_room_idx ON participants(room_code);
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
(rating_key,library_key,media_type,guid,title,year,summary,duration,rating,thumb,genres,viewed)
VALUES(?,?,?,?,?,?,?,?,?,?,?,?) ON CONFLICT(rating_key) DO UPDATE SET
library_key=excluded.library_key,media_type=excluded.media_type,guid=excluded.guid,title=excluded.title,year=excluded.year,
summary=excluded.summary,duration=excluded.duration,rating=excluded.rating,thumb=excluded.thumb,
genres=excluded.genres,viewed=excluded.viewed`)
	if err != nil {
		return err
	}
	defer stmt.Close()
	for _, item := range items {
		genres, _ := json.Marshal(item.Genres)
		if _, err := stmt.ExecContext(ctx, item.RatingKey, item.Library, item.Type, item.GUID, item.Title, item.Year,
			item.Summary, item.Duration, item.Rating, item.Thumb, string(genres), item.Viewed); err != nil {
			return fmt.Errorf("save item %q: %w", item.Title, err)
		}
	}
	return tx.Commit()
}

// CreateRoom persists a room, its owner, and eligible media items.
func (s *Store) CreateRoom(ctx context.Context, room Room, participant Participant, tokenHash string, movieIDs []string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `INSERT INTO rooms(code,created_at,expires_at) VALUES(?,?,?)`, room.Code, room.CreatedAt.Unix(), room.ExpiresAt.Unix()); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO participants(id,room_code,name,token_hash,joined_at) VALUES(?,?,?,?,?)`, participant.ID, room.Code, participant.Name, tokenHash, time.Now().Unix()); err != nil {
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
	return tx.Commit()
}

// JoinRoom persists a participant in an active room.
func (s *Store) JoinRoom(ctx context.Context, code string, participant Participant, tokenHash string) error {
	result, err := s.db.ExecContext(ctx, `INSERT INTO participants(id,room_code,name,token_hash,joined_at)
SELECT ?,code,?,?,? FROM rooms WHERE code=? AND expires_at>?`, participant.ID, participant.Name, tokenHash, time.Now().Unix(), code, time.Now().Unix())
	if err != nil {
		return err
	}
	if count, _ := result.RowsAffected(); count == 0 {
		return ErrNotFound
	}
	return nil
}

// ParticipantByToken authenticates and returns a room participant.
func (s *Store) ParticipantByToken(ctx context.Context, code, tokenHash string) (Participant, error) {
	var participant Participant
	err := s.db.QueryRowContext(ctx, `SELECT id,name FROM participants WHERE room_code=? AND token_hash=?`, code, tokenHash).Scan(&participant.ID, &participant.Name)
	if errors.Is(err, sql.ErrNoRows) {
		return participant, ErrNotFound
	}
	return participant, err
}

// RoomState returns the state of a room for one participant.
func (s *Store) RoomState(ctx context.Context, code, participantID string) (RoomState, error) {
	var state RoomState
	var created, expires int64
	err := s.db.QueryRowContext(ctx, `SELECT code,created_at,expires_at FROM rooms WHERE code=? AND expires_at>?`, code, time.Now().Unix()).Scan(&state.Room.Code, &created, &expires)
	if errors.Is(err, sql.ErrNoRows) {
		return state, ErrNotFound
	}
	if err != nil {
		return state, err
	}
	state.Room.CreatedAt, state.Room.ExpiresAt = time.Unix(created, 0).UTC(), time.Unix(expires, 0).UTC()
	if err := s.db.QueryRowContext(ctx, `SELECT id,name FROM participants WHERE id=? AND room_code=?`, participantID, code).Scan(&state.Me.ID, &state.Me.Name); err != nil {
		return state, ErrNotFound
	}
	rows, err := s.db.QueryContext(ctx, `SELECT id,name FROM participants WHERE room_code=? ORDER BY joined_at`, code)
	if err != nil {
		return state, err
	}
	for rows.Next() {
		var p Participant
		if err := rows.Scan(&p.ID, &p.Name); err != nil {
			rows.Close()
			return state, err
		}
		state.Participants = append(state.Participants, p)
	}
	rows.Close()

	state.Candidate, err = s.nextMovie(ctx, code, participantID)
	if err != nil {
		return state, err
	}
	state.Matches, err = s.matchMovies(ctx, code)
	if err != nil {
		return state, err
	}
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*), (SELECT COUNT(*) FROM room_movies WHERE room_code=?) FROM votes WHERE room_code=? AND participant_id=?`, code, code, participantID).Scan(&state.Progress.Voted, &state.Progress.Total); err != nil {
		return state, err
	}
	return state, nil
}

// nextMovie returns the next unvoted media item for a participant.
func (s *Store) nextMovie(ctx context.Context, code, participantID string) (*plex.Item, error) {
	row := s.db.QueryRowContext(ctx, `SELECT m.rating_key,m.library_key,m.media_type,m.guid,m.title,m.year,m.summary,m.duration,m.rating,m.genres,m.viewed
FROM room_movies rm JOIN movies m ON m.rating_key=rm.movie_id
LEFT JOIN votes v ON v.room_code=rm.room_code AND v.movie_id=rm.movie_id AND v.participant_id=?
WHERE rm.room_code=? AND v.movie_id IS NULL ORDER BY rm.position LIMIT 1`, participantID, code)
	movie, err := scanMovie(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return movie, err
}

// matchMovies returns media liked by every active participant.
func (s *Store) matchMovies(ctx context.Context, code string) ([]plex.Item, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT m.rating_key,m.library_key,m.media_type,m.guid,m.title,m.year,m.summary,m.duration,m.rating,m.genres,m.viewed
FROM matches x JOIN movies m ON m.rating_key=x.movie_id WHERE x.room_code=? ORDER BY x.matched_at DESC`, code)
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
	if err := row.Scan(&movie.RatingKey, &movie.Library, &movie.Type, &movie.GUID, &movie.Title, &movie.Year, &movie.Summary, &movie.Duration, &movie.Rating, &genres, &movie.Viewed); err != nil {
		return nil, err
	}
	_ = json.Unmarshal([]byte(genres), &movie.Genres)
	return &movie, nil
}

// Vote persists a participant vote and reports a unanimous match.
func (s *Store) Vote(ctx context.Context, code, participantID, movieID string, liked bool) (bool, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return false, err
	}
	defer tx.Rollback()
	result, err := tx.ExecContext(ctx, `INSERT INTO votes(room_code,participant_id,movie_id,liked,created_at)
SELECT ?,?,?,?,? WHERE EXISTS(SELECT 1 FROM participants WHERE id=? AND room_code=?)
AND EXISTS(SELECT 1 FROM room_movies WHERE room_code=? AND movie_id=?)
ON CONFLICT(room_code,participant_id,movie_id) DO UPDATE SET liked=excluded.liked,created_at=excluded.created_at`,
		code, participantID, movieID, liked, time.Now().Unix(), participantID, code, code, movieID)
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
	}
	return matched, tx.Commit()
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

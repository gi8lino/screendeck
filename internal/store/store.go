package store

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	_ "modernc.org/sqlite"
)

// ErrNotFound indicates that a requested persisted entity does not exist.
var ErrNotFound = errors.New("not found")

// ErrForbidden indicates that the authenticated caller is not allowed to perform an operation.
var ErrForbidden = errors.New("forbidden")

// Store owns the SQLite database and the cipher used for stored secrets.
type Store struct {
	// db is the underlying SQLite connection pool.
	db *sql.DB
	// cipher encrypts and decrypts persisted Plex secrets.
	cipher cipher.AEAD
}

// Open opens the SQLite database, prepares encryption, and applies schema migrations.
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
		db.Close() // nolint:errcheck
		return nil, err
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		db.Close() // nolint:errcheck
		return nil, err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		db.Close() // nolint:errcheck
		return nil, err
	}
	store := &Store{db: db, cipher: aead}
	if err := store.migrate(context.Background()); err != nil {
		db.Close() // nolint:errcheck
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
		file.Close() // nolint:errcheck
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

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

const (
	encryptionKeySize    = 32
	inMemoryDatabasePath = ":memory:"
)

var (
	// ErrNotFound indicates that a requested persisted entity does not exist.
	ErrNotFound = errors.New("not found")
	// ErrForbidden indicates that the authenticated caller is not allowed to perform an operation.
	ErrForbidden = errors.New("forbidden")
	// ErrMembershipConflict indicates that a browser identity is already linked to another participant in a room.
	ErrMembershipConflict = errors.New("browser identity already linked to another room participant")
	// ErrRoomLocked indicates that a room is not accepting new participants.
	ErrRoomLocked = errors.New("room is locked")
)

// Store owns the SQLite database and the cipher used for stored secrets.
type Store struct {
	// db is the underlying SQLite connection pool.
	db *sql.DB
	// cipher encrypts and decrypts persisted media-provider and session secrets.
	cipher cipher.AEAD
}

// Ping verifies the database connection.
func (s *Store) Ping(ctx context.Context) error {
	return s.db.PingContext(ctx)
}

// Open opens the SQLite database, prepares encryption, and applies schema migrations.
func Open(path string, configuredKeyPath ...string) (*Store, error) {
	db, err := openSQLite(path)
	if err != nil {
		return nil, err
	}

	keyPath := optionalKeyPath(configuredKeyPath)
	key, err := loadEncryptionKey(path, keyPath)
	if err != nil {
		db.Close() // nolint:errcheck
		return nil, err
	}

	aead, err := newEncryptionCipher(key)
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

// openSQLite opens the configured SQLite database and applies connection-level settings.
func openSQLite(path string) (*sql.DB, error) {
	if path != inMemoryDatabasePath {
		if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
			return nil, fmt.Errorf("create database directory: %w", err)
		}
	}

	// Enforce relationships, wait briefly for concurrent writers, and let readers proceed during writes.
	dsn := path + "?_pragma=foreign_keys(1)&_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)"
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("open database: %w", err)
	}
	// Keep :memory: databases coherent and serialize SQLite writes through one connection.
	db.SetMaxOpenConns(1)
	return db, nil
}

// optionalKeyPath returns the first optional encryption-key path.
func optionalKeyPath(configured []string) string {
	if len(configured) == 0 {
		return ""
	}
	return configured[0]
}

// newEncryptionCipher constructs the AES-GCM cipher used for stored secrets.
func newEncryptionCipher(key []byte) (cipher.AEAD, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}

	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	return aead, nil
}

// Close closes the underlying database.
func (s *Store) Close() error { return s.db.Close() }

// loadEncryptionKey loads or creates the database encryption key.
func loadEncryptionKey(databasePath, configuredPath string) ([]byte, error) {
	if databasePath == inMemoryDatabasePath {
		// An in-memory database must not leave its otherwise unusable encryption key on disk.
		return randomEncryptionKey()
	}

	keyPath := configuredPath
	if keyPath == "" {
		keyPath = filepath.Join(filepath.Dir(databasePath), "auth.key")
	}

	if err := os.MkdirAll(filepath.Dir(keyPath), 0o750); err != nil {
		return nil, fmt.Errorf("create authentication key directory: %w", err)
	}

	key, err := readEncryptionKey(keyPath)
	if err == nil {
		return key, nil
	}
	if !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}

	return createEncryptionKey(databasePath, keyPath)
}

// readEncryptionKey reads and validates an existing encryption-key file.
func readEncryptionKey(keyPath string) ([]byte, error) {
	key, err := os.ReadFile(keyPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, err
		}
		return nil, fmt.Errorf("read authentication key: %w", err)
	}
	if len(key) != encryptionKeySize {
		return nil, errors.New("authentication key must contain exactly 32 bytes")
	}
	return key, nil
}

// createEncryptionKey creates a new encryption-key file without replacing an existing key.
func createEncryptionKey(databasePath, keyPath string) ([]byte, error) {
	key, err := randomEncryptionKey()
	if err != nil {
		return nil, err
	}

	// O_EXCL prevents concurrent processes from silently replacing each other's key.
	file, err := os.OpenFile(keyPath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if errors.Is(err, os.ErrExist) {
		// Another process won the creation race, so use the key it persisted.
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

// randomEncryptionKey creates a new random AES-256 key.
func randomEncryptionKey() ([]byte, error) {
	key := make([]byte, encryptionKeySize) // Allocate the exact 32 bytes required for an AES-256 key.
	if _, err := rand.Read(key); err != nil {
		return nil, err
	}
	return key, nil
}

// seal encrypts sensitive data for persistent storage.
func (s *Store) seal(value []byte) ([]byte, error) {
	nonce := make([]byte, s.cipher.NonceSize()) // GCM requires a fresh nonce of this exact size for every encryption.
	if _, err := rand.Read(nonce); err != nil {
		return nil, err
	}
	// Using nonce as dst prefixes it to the authenticated ciphertext and tag for storage.
	return s.cipher.Seal(nonce, nonce, value, nil), nil
}

// open decrypts sensitive data loaded from persistent storage.
func (s *Store) open(value []byte) ([]byte, error) {
	if len(value) < s.cipher.NonceSize() {
		return nil, errors.New("encrypted authentication value is truncated")
	}

	nonceSize := s.cipher.NonceSize() // Locate the boundary in the stored nonce || ciphertext layout.
	nonce := value[:nonceSize]        // Recover the nonce prefix written by seal.
	// Authenticate and decrypt the remaining ciphertext and tag into a new plaintext buffer.
	return s.cipher.Open(nil, nonce, value[nonceSize:], nil)
}

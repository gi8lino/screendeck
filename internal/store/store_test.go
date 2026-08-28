package store

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestOpenSQLiteConnectionPool verifies in-memory and persistent databases use appropriate pool sizes.
func TestOpenSQLiteConnectionPool(t *testing.T) {
	t.Run("in-memory database uses one connection", func(t *testing.T) {
		db, err := openSQLite(inMemoryDatabasePath)
		require.NoError(t, err)
		defer db.Close() // nolint:errcheck

		assert.Equal(t, 1, db.Stats().MaxOpenConnections)
	})

	t.Run("persistent database uses a small WAL pool", func(t *testing.T) {
		db, err := openSQLite(filepath.Join(t.TempDir(), "screendeck.db"))
		require.NoError(t, err)
		defer db.Close() // nolint:errcheck

		assert.Equal(t, persistentMaxOpenConns, db.Stats().MaxOpenConnections)
	})
}

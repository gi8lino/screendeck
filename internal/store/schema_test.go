package store

import (
	"database/sql"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestSchemaVersion verifies SQLite user_version is returned by the schema helper.
func TestSchemaVersion(t *testing.T) {
	t.Run("reads user version", func(t *testing.T) {
		raw, err := sql.Open("sqlite", ":memory:")
		require.NoError(t, err)
		defer raw.Close() // nolint:errcheck
		_, err = raw.ExecContext(t.Context(), "PRAGMA user_version = 7")
		require.NoError(t, err)

		database := &Store{db: raw}
		version, err := database.schemaVersion(t.Context())
		require.NoError(t, err)
		assert.Equal(t, 7, version)
	})
}

// TestSchemaEmpty verifies SQLite user tables determine whether a schema is empty.
func TestSchemaEmpty(t *testing.T) {
	t.Run("empty database", func(t *testing.T) {
		raw, err := sql.Open("sqlite", ":memory:")
		require.NoError(t, err)
		defer raw.Close() // nolint:errcheck

		database := &Store{db: raw}
		empty, err := database.schemaEmpty(t.Context())
		require.NoError(t, err)
		assert.True(t, empty)
	})

	t.Run("database with user table", func(t *testing.T) {
		raw, err := sql.Open("sqlite", ":memory:")
		require.NoError(t, err)
		defer raw.Close() // nolint:errcheck
		_, err = raw.ExecContext(t.Context(), "CREATE TABLE example (id INTEGER PRIMARY KEY)")
		require.NoError(t, err)

		database := &Store{db: raw}
		empty, err := database.schemaEmpty(t.Context())
		require.NoError(t, err)
		assert.False(t, empty)
	})
}

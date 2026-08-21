package store

import (
	"database/sql"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestInitSchemaCreatesVersionedDatabase verifies fresh databases receive the current schema version.
func TestInitSchemaCreatesVersionedDatabase(t *testing.T) {
	database, err := Open(":memory:")
	require.NoError(t, err)
	defer database.Close() // nolint:errcheck

	var version int
	require.NoError(t, database.db.QueryRow("PRAGMA user_version").Scan(&version))
	assert.Equal(t, schemaVersion, version)
}

// TestInitSchemaRejectsUnversionedDatabase verifies pre-versioned schemas are not modified automatically.
func TestInitSchemaRejectsUnversionedDatabase(t *testing.T) {
	directory := t.TempDir()
	databasePath := filepath.Join(directory, "unversioned.db")
	raw, err := sql.Open("sqlite", databasePath)
	require.NoError(t, err)
	_, err = raw.Exec("CREATE TABLE old_schema (id INTEGER PRIMARY KEY)")
	require.NoError(t, err)
	require.NoError(t, raw.Close())

	_, err = Open(databasePath, filepath.Join(directory, "auth.key"))
	require.ErrorContains(t, err, "database schema is unversioned")
}

// TestInitSchemaRejectsUnsupportedVersion verifies unknown schema versions fail instead of being changed.
func TestInitSchemaRejectsUnsupportedVersion(t *testing.T) {
	directory := t.TempDir()
	databasePath := filepath.Join(directory, "future.db")
	raw, err := sql.Open("sqlite", databasePath)
	require.NoError(t, err)
	_, err = raw.Exec("PRAGMA user_version = 2")
	require.NoError(t, err)
	require.NoError(t, raw.Close())

	_, err = Open(databasePath, filepath.Join(directory, "auth.key"))
	require.ErrorContains(t, err, "unsupported database schema version 2")
}

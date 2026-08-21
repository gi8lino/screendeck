package store

import (
	"database/sql"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestMigrateCreatesVersionedDatabase verifies fresh databases run all embedded migrations.
func TestMigrateCreatesVersionedDatabase(t *testing.T) {
	database, err := Open(":memory:")
	require.NoError(t, err)
	defer database.Close() // nolint:errcheck

	migrations, err := loadMigrations()
	require.NoError(t, err)

	version, err := database.schemaVersion(t.Context())
	require.NoError(t, err)
	assert.Equal(t, latestMigrationVersion(migrations), version)

	var tableName string
	err = database.db.QueryRow("SELECT name FROM sqlite_master WHERE type='table' AND name='rooms'").Scan(&tableName)
	require.NoError(t, err)
	assert.Equal(t, "rooms", tableName)
}

// TestMigrateRejectsUnversionedDatabase verifies pre-versioned schemas are not modified automatically.
func TestMigrateRejectsUnversionedDatabase(t *testing.T) {
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

// TestMigrateRejectsNewerVersion verifies databases from newer releases fail safely.
func TestMigrateRejectsNewerVersion(t *testing.T) {
	directory := t.TempDir()
	databasePath := filepath.Join(directory, "future.db")
	raw, err := sql.Open("sqlite", databasePath)
	require.NoError(t, err)
	_, err = raw.Exec("PRAGMA user_version = 999")
	require.NoError(t, err)
	require.NoError(t, raw.Close())

	_, err = Open(databasePath, filepath.Join(directory, "auth.key"))
	require.ErrorContains(t, err, "database schema version 999 is newer than supported version")
}

// TestLoadMigrations verifies embedded migrations are ordered and contiguous.
func TestLoadMigrations(t *testing.T) {
	migrations, err := loadMigrations()
	require.NoError(t, err)
	require.Len(t, migrations, 1)
	assert.Equal(t, 1, migrations[0].version)
	assert.Equal(t, "migrations/001_initial.sql", migrations[0].name)
	assert.NotEmpty(t, migrations[0].statement)
}

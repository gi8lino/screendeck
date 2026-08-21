package store

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestMigrateReopensCurrentDatabase verifies a current database is not migrated again.
func TestMigrateReopensCurrentDatabase(t *testing.T) {
	directory := t.TempDir()
	databasePath := filepath.Join(directory, "screendeck.db")
	keyPath := filepath.Join(directory, "auth.key")

	database, err := Open(databasePath, keyPath)
	require.NoError(t, err)
	_, err = database.db.ExecContext(
		t.Context(),
		"INSERT INTO libraries (key, title, synced_at) VALUES (?, ?, ?)",
		"1",
		"Films",
		123,
	)
	require.NoError(t, err)
	require.NoError(t, database.Close())

	reopened, err := Open(databasePath, keyPath)
	require.NoError(t, err)
	defer reopened.Close() // nolint:errcheck

	migrations, err := loadMigrations()
	require.NoError(t, err)
	version, err := reopened.schemaVersion(t.Context())
	require.NoError(t, err)
	assert.Equal(t, latestMigrationVersion(migrations), version)

	var title string
	err = reopened.db.QueryRowContext(
		t.Context(),
		"SELECT title FROM libraries WHERE key = ?",
		"1",
	).Scan(&title)
	require.NoError(t, err)
	assert.Equal(t, "Films", title)
}

// TestApplyMigrationAdvancesVersion verifies a successful migration changes the schema and version together.
func TestApplyMigrationAdvancesVersion(t *testing.T) {
	database, err := Open(":memory:")
	require.NoError(t, err)
	defer database.Close() // nolint:errcheck

	migrations, err := loadMigrations()
	require.NoError(t, err)
	currentVersion := latestMigrationVersion(migrations)
	nextVersion := currentVersion + 1

	err = database.applyMigration(t.Context(), migration{
		version: nextVersion,
		name:    "test_add_migration_probe.sql",
		statement: `
CREATE TABLE migration_probe (
  id INTEGER PRIMARY KEY,
  value TEXT NOT NULL
);
`,
	})
	require.NoError(t, err)

	version, err := database.schemaVersion(t.Context())
	require.NoError(t, err)
	assert.Equal(t, nextVersion, version)

	var tableName string
	err = database.db.QueryRowContext(
		t.Context(),
		"SELECT name FROM sqlite_master WHERE type = 'table' AND name = 'migration_probe'",
	).Scan(&tableName)
	require.NoError(t, err)
	assert.Equal(t, "migration_probe", tableName)
}

// TestApplyMigrationRollsBackOnFailure verifies a failed migration leaves neither schema changes nor a new version behind.
func TestApplyMigrationRollsBackOnFailure(t *testing.T) {
	database, err := Open(":memory:")
	require.NoError(t, err)
	defer database.Close() // nolint:errcheck

	migrations, err := loadMigrations()
	require.NoError(t, err)
	currentVersion := latestMigrationVersion(migrations)

	err = database.applyMigration(t.Context(), migration{
		version: currentVersion + 1,
		name:    "test_failing_migration.sql",
		statement: `
CREATE TABLE migration_probe (
  id INTEGER PRIMARY KEY
);

INSERT INTO missing_migration_table (id)
VALUES (1);
`,
	})
	require.Error(t, err)

	version, err := database.schemaVersion(t.Context())
	require.NoError(t, err)
	assert.Equal(t, currentVersion, version)

	var tableCount int
	err = database.db.QueryRowContext(
		t.Context(),
		"SELECT COUNT(*) FROM sqlite_master WHERE type = 'table' AND name = 'migration_probe'",
	).Scan(&tableCount)
	require.NoError(t, err)
	assert.Zero(t, tableCount)
}

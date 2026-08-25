package store

import (
	"database/sql"
	"path/filepath"
	"testing"

	"github.com/gi8lino/screendeck/internal/media"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMigrate(t *testing.T) {
	t.Parallel()

	t.Run("creates versioned database", func(t *testing.T) {
		t.Parallel()

		database, err := Open(":memory:", "")
		require.NoError(t, err)
		defer database.Close() // nolint:errcheck

		migrations, err := loadMigrations()
		require.NoError(t, err)

		version, err := database.schemaVersion(t.Context())
		require.NoError(t, err)
		assert.Equal(t, latestMigrationVersion(migrations), version)

		var tableName string
		err = database.db.QueryRowContext(
			t.Context(),
			"SELECT name FROM sqlite_master WHERE type = 'table' AND name = 'rooms'",
		).Scan(&tableName)
		require.NoError(t, err)
		assert.Equal(t, "rooms", tableName)
	})

	t.Run("reopens current database without replaying migrations", func(t *testing.T) {
		t.Parallel()

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
	})

	t.Run("marks migrated Plex database as active provider", func(t *testing.T) {
		t.Parallel()

		directory := t.TempDir()
		databasePath := filepath.Join(directory, "plex-v1.db")
		keyPath := filepath.Join(directory, "auth.key")
		migrations, err := loadMigrations()
		require.NoError(t, err)
		require.GreaterOrEqual(t, len(migrations), 2)

		raw, err := sql.Open("sqlite", databasePath)
		require.NoError(t, err)
		_, err = raw.ExecContext(t.Context(), migrations[0].statement)
		require.NoError(t, err)
		_, err = raw.ExecContext(t.Context(), `
INSERT INTO plex_auth (
  id, auth_method, client_id, key_id, private_key, user_token, token_expires_at,
  server_id, server_name, server_url, server_token, updated_at
) VALUES (
  1, 'standard', 'client', '', X'01', X'02', 0,
  'server', 'Home Plex', 'http://plex.test', X'03', 1
)
`)
		require.NoError(t, err)
		_, err = raw.ExecContext(t.Context(), "PRAGMA user_version = 1")
		require.NoError(t, err)
		require.NoError(t, raw.Close())

		database, err := Open(databasePath, keyPath)
		require.NoError(t, err)
		defer database.Close() // nolint:errcheck

		provider, err := database.LoadMediaProvider(t.Context())
		require.NoError(t, err)
		assert.Equal(t, media.ProviderPlex, provider)
	})

	t.Run("rejects unversioned database", func(t *testing.T) {
		t.Parallel()

		directory := t.TempDir()
		databasePath := filepath.Join(directory, "unversioned.db")
		raw, err := sql.Open("sqlite", databasePath)
		require.NoError(t, err)
		_, err = raw.ExecContext(t.Context(), "CREATE TABLE old_schema (id INTEGER PRIMARY KEY)")
		require.NoError(t, err)
		require.NoError(t, raw.Close())

		_, err = Open(databasePath, filepath.Join(directory, "auth.key"))
		require.ErrorContains(t, err, "database schema is unversioned")
	})

	t.Run("rejects newer database", func(t *testing.T) {
		t.Parallel()

		directory := t.TempDir()
		databasePath := filepath.Join(directory, "future.db")
		raw, err := sql.Open("sqlite", databasePath)
		require.NoError(t, err)
		_, err = raw.ExecContext(t.Context(), "PRAGMA user_version = 999")
		require.NoError(t, err)
		require.NoError(t, raw.Close())

		_, err = Open(databasePath, filepath.Join(directory, "auth.key"))
		require.ErrorContains(t, err, "database schema version 999 is newer than supported version")
	})
}

func TestLoadMigrations(t *testing.T) {
	t.Parallel()

	t.Run("loads ordered migrations", func(t *testing.T) {
		t.Parallel()

		migrations, err := loadMigrations()
		require.NoError(t, err)
		require.Len(t, migrations, 3)
		assert.Equal(t, 1, migrations[0].version)
		assert.Equal(t, "migrations/001_initial.sql", migrations[0].name)
		assert.NotEmpty(t, migrations[0].statement)
		assert.Equal(t, 2, migrations[1].version)
		assert.Equal(t, "migrations/002_media_providers.sql", migrations[1].name)
		assert.NotEmpty(t, migrations[1].statement)
		assert.Equal(t, 3, migrations[2].version)
		assert.Equal(t, "migrations/003_room_lock.sql", migrations[2].name)
		assert.NotEmpty(t, migrations[2].statement)
	})
}

func TestMigrationVersion(t *testing.T) {
	t.Parallel()

	t.Run("initial migration", func(t *testing.T) {
		t.Parallel()

		version, err := migrationVersion("001_initial.sql")
		require.NoError(t, err)
		assert.Equal(t, 1, version)
	})

	t.Run("higher version", func(t *testing.T) {
		t.Parallel()

		version, err := migrationVersion("042_add_indexes.sql")
		require.NoError(t, err)
		assert.Equal(t, 42, version)
	})

	t.Run("missing prefix", func(t *testing.T) {
		t.Parallel()

		_, err := migrationVersion("initial.sql")
		require.Error(t, err)
	})

	t.Run("zero version", func(t *testing.T) {
		t.Parallel()

		_, err := migrationVersion("000_initial.sql")
		require.Error(t, err)
	})

	t.Run("non numeric prefix", func(t *testing.T) {
		t.Parallel()

		_, err := migrationVersion("abc_initial.sql")
		require.Error(t, err)
	})
}

func TestValidateMigrationSequence(t *testing.T) {
	t.Parallel()

	t.Run("contiguous", func(t *testing.T) {
		t.Parallel()

		err := validateMigrationSequence([]migration{
			{version: 1, name: "migrations/001_initial.sql"},
			{version: 2, name: "migrations/002_add_room_settings.sql"},
			{version: 3, name: "migrations/003_add_indexes.sql"},
		})
		require.NoError(t, err)
	})

	t.Run("empty", func(t *testing.T) {
		t.Parallel()

		err := validateMigrationSequence(nil)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "no database migrations")
	})

	t.Run("gap", func(t *testing.T) {
		t.Parallel()

		err := validateMigrationSequence([]migration{
			{version: 1, name: "migrations/001_initial.sql"},
			{version: 3, name: "migrations/003_add_indexes.sql"},
		})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "expected version 2")
	})
}

func TestApplyMigration(t *testing.T) {
	t.Parallel()

	t.Run("advances version", func(t *testing.T) {
		t.Parallel()

		database, err := Open(":memory:", "")
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
	})

	t.Run("rolls back on failure", func(t *testing.T) {
		t.Parallel()

		database, err := Open(":memory:", "")
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
	})
}

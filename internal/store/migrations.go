package store

import (
	"context"
	"embed"
	"errors"
	"fmt"
	"io/fs"
	"sort"
	"strconv"
	"strings"
)

// migration contains one ordered SQLite schema change.
type migration struct {
	// version is the schema version produced by this migration.
	version int
	// name is the embedded migration file name.
	name string
	// statement contains the SQL executed by the migration.
	statement string
}

// migrationFiles contains all forward-only SQLite migrations.
//
//go:embed migrations/*.sql
var migrationFiles embed.FS

// migrate upgrades a fresh or versioned database to the current schema.
func (s *Store) migrate(ctx context.Context) error {
	migrations, err := loadMigrations()
	if err != nil {
		return err
	}
	currentVersion := latestMigrationVersion(migrations)

	version, err := s.schemaVersion(ctx)
	if err != nil {
		return err
	}
	if version > currentVersion {
		return fmt.Errorf(
			"database schema version %d is newer than supported version %d",
			version,
			currentVersion,
		)
	}

	if version == 0 {
		empty, err := s.schemaEmpty(ctx)
		if err != nil {
			return err
		}
		if !empty {
			return errors.New("database schema is unversioned; recreate the database")
		}
	}

	for _, migration := range migrations {
		if migration.version <= version {
			continue
		}
		if err := s.applyMigration(ctx, migration); err != nil {
			return err
		}
		version = migration.version
	}

	if version != currentVersion {
		return fmt.Errorf(
			"database schema version %d did not reach current version %d",
			version,
			currentVersion,
		)
	}
	return nil
}

// loadMigrations loads and validates embedded migration files in version order.
func loadMigrations() ([]migration, error) {
	entries, err := fs.ReadDir(migrationFiles, "migrations")
	if err != nil {
		return nil, fmt.Errorf("read database migrations: %w", err)
	}

	migrations := make([]migration, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".sql") {
			continue
		}

		versionText, _, ok := strings.Cut(entry.Name(), "_")
		if !ok {
			return nil, fmt.Errorf("invalid database migration name %q", entry.Name())
		}
		version, err := strconv.Atoi(versionText)
		if err != nil || version <= 0 {
			return nil, fmt.Errorf("invalid database migration version %q", versionText)
		}

		name := "migrations/" + entry.Name()
		statement, err := migrationFiles.ReadFile(name)
		if err != nil {
			return nil, fmt.Errorf("read database migration %q: %w", name, err)
		}
		migrations = append(migrations, migration{
			version:   version,
			name:      name,
			statement: string(statement),
		})
	}

	sort.Slice(migrations, func(i, j int) bool {
		return migrations[i].version < migrations[j].version
	})
	for index, migration := range migrations {
		expected := index + 1
		if migration.version != expected {
			return nil, fmt.Errorf(
				"database migrations must be contiguous: expected version %d, found %d in %q",
				expected,
				migration.version,
				migration.name,
			)
		}
	}
	if len(migrations) == 0 {
		return nil, errors.New("no database migrations found")
	}
	return migrations, nil
}

// latestMigrationVersion returns the newest schema version in the migration set.
func latestMigrationVersion(migrations []migration) int {
	if len(migrations) == 0 {
		return 0
	}
	return migrations[len(migrations)-1].version
}

// applyMigration executes one migration and records its version atomically.
func (s *Store) applyMigration(ctx context.Context, migration migration) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin database migration %d: %w", migration.version, err)
	}
	defer tx.Rollback() // nolint:errcheck

	if _, err := tx.ExecContext(ctx, migration.statement); err != nil {
		return fmt.Errorf("apply database migration %d (%s): %w", migration.version, migration.name, err)
	}
	if _, err := tx.ExecContext(ctx, fmt.Sprintf("PRAGMA user_version = %d", migration.version)); err != nil {
		return fmt.Errorf("record database migration %d: %w", migration.version, err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit database migration %d: %w", migration.version, err)
	}
	return nil
}

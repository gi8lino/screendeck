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

	if err := s.validateMigrationSource(ctx, version, currentVersion); err != nil {
		return err
	}

	version, err = s.applyPendingMigrations(ctx, migrations, version)
	if err != nil {
		return err
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

// validateMigrationSource verifies that the current database can be upgraded safely.
func (s *Store) validateMigrationSource(ctx context.Context, version, currentVersion int) error {
	if version > currentVersion {
		return fmt.Errorf(
			"database schema version %d is newer than supported version %d",
			version,
			currentVersion,
		)
	}
	if version != 0 {
		return nil
	}

	empty, err := s.schemaEmpty(ctx)
	if err != nil {
		return err
	}
	if !empty {
		return errors.New("database schema is unversioned; recreate the database")
	}

	return nil
}

// applyPendingMigrations applies every migration newer than the supplied database version.
func (s *Store) applyPendingMigrations(ctx context.Context, migrations []migration, currentVersion int) (version int, err error) {
	version = currentVersion

	for _, migration := range migrations {
		if migration.version <= version {
			continue
		}
		if err := s.applyMigration(ctx, migration); err != nil {
			return version, err
		}
		version = migration.version
	}

	return version, nil
}

// loadMigrations loads and validates embedded migration files in version order.
func loadMigrations() ([]migration, error) {
	entries, err := fs.ReadDir(migrationFiles, "migrations")
	if err != nil {
		return nil, fmt.Errorf("read database migrations: %w", err)
	}

	migrations := make([]migration, 0, len(entries))
	for _, entry := range entries {
		migration, ok, err := loadMigration(entry)
		if err != nil {
			return nil, err
		}
		if ok {
			migrations = append(migrations, migration)
		}
	}

	sort.Slice(migrations, func(i, j int) bool {
		return migrations[i].version < migrations[j].version
	})
	if err := validateMigrationSequence(migrations); err != nil {
		return nil, err
	}

	return migrations, nil
}

// loadMigration loads one SQL migration entry when the embedded file is eligible.
func loadMigration(entry fs.DirEntry) (result migration, ok bool, err error) {
	if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".sql") {
		return migration{}, false, nil
	}

	version, err := migrationVersion(entry.Name())
	if err != nil {
		return migration{}, false, err
	}

	name := "migrations/" + entry.Name()
	statement, err := migrationFiles.ReadFile(name)
	if err != nil {
		return migration{}, false, fmt.Errorf("read database migration %q: %w", name, err)
	}

	result = migration{
		version:   version,
		name:      name,
		statement: string(statement),
	}
	return result, true, nil
}

// migrationVersion parses the positive numeric prefix from a migration file name.
func migrationVersion(name string) (int, error) {
	versionText, _, ok := strings.Cut(name, "_")
	if !ok {
		return 0, fmt.Errorf("invalid database migration name %q", name)
	}

	version, err := strconv.Atoi(versionText)
	if err != nil || version <= 0 {
		return 0, fmt.Errorf("invalid database migration version %q", versionText)
	}

	return version, nil
}

// validateMigrationSequence verifies that migration versions start at one and remain contiguous.
func validateMigrationSequence(migrations []migration) error {
	if len(migrations) == 0 {
		return errors.New("no database migrations found")
	}

	for index, migration := range migrations {
		expected := index + 1
		if migration.version != expected {
			return fmt.Errorf(
				"database migrations must be contiguous: expected version %d, found %d in %q",
				expected,
				migration.version,
				migration.name,
			)
		}
	}

	return nil
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

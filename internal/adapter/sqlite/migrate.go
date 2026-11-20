package sqlite

import (
	"cmp"
	"context"
	"embed"
	"errors"
	"fmt"
	"io/fs"
	"slices"
	"strconv"
	"strings"
	"time"
)

//go:embed migrations/*.sql
var embeddedMigrations embed.FS

// Migration defines a single versioned SQLite schema migration step.
type Migration struct {
	// Version is the strictly positive monotonic schema version number.
	Version int
	// Name is a descriptive migration filename or label.
	Name string
	// SQL is the SQL DDL/DML script executed inside a migration transaction.
	SQL string
}

// Migrate applies all embedded SQL migrations in ascending version order.
func (d *DB) Migrate(ctx context.Context) error {
	migrations, err := loadEmbeddedMigrations()
	if err != nil {
		return err
	}
	return d.MigrateWith(ctx, migrations)
}

// MigrateWith applies the supplied migrations in ascending version order,
// creating an automatic pre-migration backup when upgrading an existing file DB.
func (d *DB) MigrateWith(ctx context.Context, migrations []Migration) error {
	ordered, err := validateAndSortMigrations(migrations)
	if err != nil {
		return err
	}

	tableCount, applied, currentVersion, err := d.inspectMigrationState(ctx)
	if err != nil {
		return err
	}
	pending := filterPendingMigrations(ordered, applied)
	if len(pending) == 0 {
		return nil
	}

	if tableCount > 0 && d.path != "" && d.path != ":memory:" {
		suffix := fmt.Sprintf("v%d_%d", currentVersion, time.Now().UTC().Unix())
		if _, err := d.Backup(ctx, suffix); err != nil {
			return fmt.Errorf("pre-migration backup: %w", err)
		}
	}
	if err := d.ensureMigrationsTable(ctx); err != nil {
		return err
	}
	for _, m := range pending {
		if err := d.applyMigrationTx(ctx, m); err != nil {
			return err
		}
	}
	return nil
}

func loadEmbeddedMigrations() ([]Migration, error) {
	entries, err := fs.ReadDir(embeddedMigrations, "migrations")
	if err != nil {
		return nil, fmt.Errorf("read embedded migrations: %w", err)
	}
	migrations := make([]Migration, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".sql") {
			continue
		}
		version, err := parseMigrationVersion(entry.Name())
		if err != nil {
			return nil, err
		}
		rawSQL, err := embeddedMigrations.ReadFile("migrations/" + entry.Name())
		if err != nil {
			return nil, fmt.Errorf("read migration %s: %w", entry.Name(), err)
		}
		migrations = append(migrations, Migration{
			Version: version,
			Name:    entry.Name(),
			SQL:     string(rawSQL),
		})
	}
	return validateAndSortMigrations(migrations)
}

func parseMigrationVersion(fileName string) (int, error) {
	prefix, _, ok := strings.Cut(fileName, "_")
	if !ok {
		return 0, fmt.Errorf("migration filename %q missing version prefix", fileName)
	}
	version, err := strconv.Atoi(prefix)
	if err != nil || version <= 0 {
		return 0, fmt.Errorf("invalid migration version in %q", fileName)
	}
	return version, nil
}

func validateAndSortMigrations(migrations []Migration) ([]Migration, error) {
	out := slices.Clone(migrations)
	slices.SortFunc(out, func(a, b Migration) int {
		return cmp.Compare(a.Version, b.Version)
	})
	seen := make(map[int]struct{}, len(out))
	for _, m := range out {
		if m.Version <= 0 {
			return nil, fmt.Errorf("migration %q must have version > 0", m.Name)
		}
		if _, exists := seen[m.Version]; exists {
			return nil, fmt.Errorf("duplicate migration version %d", m.Version)
		}
		seen[m.Version] = struct{}{}
	}
	return out, nil
}

func (d *DB) inspectMigrationState(ctx context.Context) (int, map[int]bool, int, error) {
	var tableCount int
	err := d.sqlDB.QueryRowContext(
		ctx,
		`SELECT COUNT(*) FROM sqlite_master WHERE type = 'table'`,
	).Scan(&tableCount)
	if err != nil {
		return 0, nil, 0, fmt.Errorf("inspect sqlite_master: %w", err)
	}

	var hasMigrationsTable int
	err = d.sqlDB.QueryRowContext(
		ctx,
		`SELECT COUNT(*) FROM sqlite_master WHERE type = 'table' AND name = 'schema_migrations'`,
	).Scan(&hasMigrationsTable)
	if err != nil {
		return 0, nil, 0, fmt.Errorf("check schema_migrations existence: %w", err)
	}

	applied := make(map[int]bool)
	if hasMigrationsTable == 0 {
		return tableCount, applied, 0, nil
	}
	currentVersion, err := d.loadAppliedVersions(ctx, applied)
	if err != nil {
		return 0, nil, 0, err
	}
	return tableCount, applied, currentVersion, nil
}

func (d *DB) loadAppliedVersions(ctx context.Context, applied map[int]bool) (int, error) {
	rows, err := d.sqlDB.QueryContext(ctx, `SELECT version FROM schema_migrations`)
	if err != nil {
		return 0, fmt.Errorf("query schema_migrations: %w", err)
	}
	defer func() { _ = rows.Close() }()

	currentVersion := 0
	for rows.Next() {
		var v int
		if err := rows.Scan(&v); err != nil {
			return 0, fmt.Errorf("scan schema_migrations version: %w", err)
		}
		applied[v] = true
		if v > currentVersion {
			currentVersion = v
		}
	}
	if err := rows.Err(); err != nil {
		return 0, fmt.Errorf("iterate schema_migrations: %w", err)
	}
	return currentVersion, nil
}

func filterPendingMigrations(ordered []Migration, applied map[int]bool) []Migration {
	var pending []Migration
	for _, m := range ordered {
		if !applied[m.Version] {
			pending = append(pending, m)
		}
	}
	return pending
}

func (d *DB) ensureMigrationsTable(ctx context.Context) error {
	_, err := d.sqlDB.ExecContext(ctx, `
		CREATE TABLE IF NOT EXISTS schema_migrations (
			version         INTEGER PRIMARY KEY,
			applied_at_unix INTEGER NOT NULL
		)
	`)
	if err != nil {
		return fmt.Errorf("create schema_migrations table: %w", err)
	}
	return nil
}

func (d *DB) applyMigrationTx(ctx context.Context, m Migration) error {
	tx, err := d.sqlDB.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin migration %d (%s): %w", m.Version, m.Name, err)
	}

	if _, err := tx.ExecContext(ctx, m.SQL); err != nil {
		return errors.Join(fmt.Errorf("execute migration %d (%s): %w", m.Version, m.Name, err), tx.Rollback())
	}
	if _, err := tx.ExecContext(
		ctx,
		`INSERT INTO schema_migrations (version, applied_at_unix) VALUES (?, ?)`,
		m.Version,
		time.Now().UTC().Unix(),
	); err != nil {
		return errors.Join(fmt.Errorf("record migration %d (%s): %w", m.Version, m.Name, err), tx.Rollback())
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit migration %d (%s): %w", m.Version, m.Name, err)
	}
	return nil
}

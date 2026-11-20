package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	_ "modernc.org/sqlite" // Register pure-Go sqlite database/sql driver.
)

const dsnParams = "_txlock=immediate&_pragma=journal_mode(WAL)&_pragma=synchronous(NORMAL)&_pragma=foreign_keys(1)&_pragma=busy_timeout(10000)"

var uriPathEscaper = strings.NewReplacer(
	"%", "%25",
	"?", "%3F",
	"#", "%23",
)

// DB wraps a configured modernc.org/sqlite *sql.DB connection pool and path.
type DB struct {
	sqlDB *sql.DB
	path  string
}

// BuildDSN builds a modernc.org/sqlite URI DSN with WAL mode, immediate write
// locking, foreign key enforcement, and a 10s busy timeout.
func BuildDSN(dbPath string) string {
	trimmed := strings.TrimSpace(dbPath)
	if trimmed == ":memory:" {
		return "file::memory:?" + dsnParams
	}
	escaped := uriPathEscaper.Replace(filepath.ToSlash(trimmed))
	return "file:" + escaped + "?" + dsnParams
}

// Open opens and pings a SQLite database at dbPath, creating parent
// directories for file-backed databases and configuring connection pooling.
func Open(ctx context.Context, dbPath string) (*DB, error) {
	trimmed := strings.TrimSpace(dbPath)
	if trimmed == "" {
		return nil, errors.New("sqlite db path cannot be empty")
	}
	if trimmed != ":memory:" {
		if err := os.MkdirAll(filepath.Dir(trimmed), 0o755); err != nil {
			return nil, fmt.Errorf("create sqlite directory: %w", err)
		}
	}

	sqlDB, err := sql.Open("sqlite", BuildDSN(trimmed))
	if err != nil {
		return nil, fmt.Errorf("open sqlite db: %w", err)
	}
	configurePool(sqlDB, trimmed)

	if err := sqlDB.PingContext(ctx); err != nil {
		_ = sqlDB.Close()
		return nil, fmt.Errorf("ping sqlite db: %w", err)
	}
	return &DB{sqlDB: sqlDB, path: trimmed}, nil
}

func configurePool(sqlDB *sql.DB, dbPath string) {
	if dbPath == ":memory:" {
		sqlDB.SetMaxOpenConns(1)
		sqlDB.SetMaxIdleConns(1)
		return
	}
	sqlDB.SetMaxOpenConns(4)
	sqlDB.SetMaxIdleConns(4)
}

// SQL returns the underlying *sql.DB handle.
func (d *DB) SQL() *sql.DB {
	return d.sqlDB
}

// Path returns the configured database path (or ":memory:").
func (d *DB) Path() string {
	return d.path
}

// Close closes the underlying SQLite connection pool.
func (d *DB) Close() error {
	if d == nil || d.sqlDB == nil {
		return nil
	}
	return d.sqlDB.Close()
}

// Checkpoint flushes WAL frames into the main database file and truncates the WAL.
func (d *DB) Checkpoint(ctx context.Context) error {
	var busy, logFrames, checkpointed int
	err := d.sqlDB.QueryRowContext(ctx, "PRAGMA wal_checkpoint(TRUNCATE)").Scan(&busy, &logFrames, &checkpointed)
	if err != nil {
		return fmt.Errorf("sqlite wal checkpoint: %w", err)
	}
	if busy != 0 || (logFrames > 0 && checkpointed < logFrames) {
		return fmt.Errorf(
			"sqlite wal checkpoint incomplete (busy=%d, log=%d, checkpointed=%d)",
			busy, logFrames, checkpointed,
		)
	}
	return nil
}

// Backup checkpoints active WAL frames and atomically copies the database file
// to <dbPath>.bak.<suffix>.
func (d *DB) Backup(ctx context.Context, suffix string) (string, error) {
	if d.path == "" || d.path == ":memory:" {
		return "", errors.New("cannot backup in-memory sqlite database")
	}
	cleanSuffix := strings.TrimSpace(suffix)
	if cleanSuffix == "" || strings.ContainsAny(cleanSuffix, `/\`) {
		return "", fmt.Errorf("invalid backup suffix %q", suffix)
	}
	if err := d.Checkpoint(ctx); err != nil {
		return "", err
	}

	dest := d.path + ".bak." + cleanSuffix
	if err := copyFileAtomic(d.path, dest); err != nil {
		return "", fmt.Errorf("backup sqlite db: %w", err)
	}
	return dest, nil
}

func copyFileAtomic(srcPath, dstPath string) error {
	src, err := os.Open(srcPath)
	if err != nil {
		return fmt.Errorf("open source file: %w", err)
	}
	defer func() { _ = src.Close() }()

	tmpPath := dstPath + ".tmp"
	dst, err := os.OpenFile(tmpPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return fmt.Errorf("create temp backup file: %w", err)
	}

	if err := copySyncClose(src, dst); err != nil {
		_ = os.Remove(tmpPath)
		return err
	}
	if err := os.Rename(tmpPath, dstPath); err != nil {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("rename temp backup file: %w", err)
	}
	return nil
}

func copySyncClose(src, dst *os.File) error {
	if _, err := io.Copy(dst, src); err != nil {
		_ = dst.Close()
		return fmt.Errorf("copy sqlite bytes: %w", err)
	}
	if err := dst.Sync(); err != nil {
		_ = dst.Close()
		return fmt.Errorf("sync backup file: %w", err)
	}
	if err := dst.Close(); err != nil {
		return fmt.Errorf("close backup file: %w", err)
	}
	return nil
}

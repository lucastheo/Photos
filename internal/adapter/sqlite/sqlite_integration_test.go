package sqlite_test

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/lucastheo/photos/internal/adapter/sqlite"
	"github.com/lucastheo/photos/internal/domain"
)

func openTestDB(t *testing.T, dbPath string) *sqlite.DB {
	t.Helper()
	ctx := context.Background()
	db, err := sqlite.Open(ctx, dbPath)
	if err != nil {
		t.Fatalf("sqlite.Open(%q) failed: %v", dbPath, err)
	}
	t.Cleanup(func() {
		if closeErr := db.Close(); closeErr != nil {
			t.Errorf("db.Close() failed: %v", closeErr)
		}
	})
	if err := db.Migrate(ctx); err != nil {
		t.Fatalf("db.Migrate() failed: %v", err)
	}
	return db
}

func TestDB_Integration_OpenAndPragmas(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	ctx := context.Background()

	t.Run("rejects empty path", func(t *testing.T) {
		if _, err := sqlite.Open(ctx, "   "); err == nil {
			t.Fatal("expected error for empty db path")
		}
		var nilDB *sqlite.DB
		if err := nilDB.Close(); err != nil {
			t.Fatalf("nil (*DB).Close() failed: %v", err)
		}
	})

	t.Run("file db sets WAL foreign_keys and busy_timeout", func(t *testing.T) {
		dir := t.TempDir()
		dbPath := filepath.Join(dir, "100% safe?", "sub#1", "photos.db")
		db := openTestDB(t, dbPath)

		if db.Path() != dbPath {
			t.Fatalf("db.Path() = %q, want %q", db.Path(), dbPath)
		}

		var journalMode string
		if err := db.SQL().QueryRowContext(ctx, `PRAGMA journal_mode`).Scan(&journalMode); err != nil {
			t.Fatalf("PRAGMA journal_mode failed: %v", err)
		}
		if !strings.EqualFold(journalMode, "wal") {
			t.Fatalf("journal_mode = %q, want wal", journalMode)
		}

		var fk int
		if err := db.SQL().QueryRowContext(ctx, `PRAGMA foreign_keys`).Scan(&fk); err != nil {
			t.Fatalf("PRAGMA foreign_keys failed: %v", err)
		}
		if fk != 1 {
			t.Fatalf("foreign_keys = %d, want 1", fk)
		}

		var busyTimeout int
		if err := db.SQL().QueryRowContext(ctx, `PRAGMA busy_timeout`).Scan(&busyTimeout); err != nil {
			t.Fatalf("PRAGMA busy_timeout failed: %v", err)
		}
		if busyTimeout != 10000 {
			t.Fatalf("busy_timeout = %d, want 10000", busyTimeout)
		}
	})

	t.Run("in-memory db opens and migrates", func(t *testing.T) {
		db := openTestDB(t, ":memory:")
		if db.Path() != ":memory:" {
			t.Fatalf("db.Path() = %q, want :memory:", db.Path())
		}
		if _, err := db.Backup(ctx, "mem"); err == nil {
			t.Fatal("expected error backing up :memory: database")
		}
	})
}

func TestDB_Integration_CheckpointAndBackup(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "photos.db")
	db := openTestDB(t, dbPath)
	syncRepo := sqlite.NewSyncRepository(db.SQL())

	if err := syncRepo.SetSetting(ctx, "session_secret", "super-secret", time.Now()); err != nil {
		t.Fatalf("SetSetting failed: %v", err)
	}

	if _, err := db.Backup(ctx, ""); err == nil {
		t.Fatal("expected error for empty backup suffix")
	}
	if _, err := db.Backup(ctx, "bad/suffix"); err == nil {
		t.Fatal("expected error for slash in backup suffix")
	}

	backupPath, err := db.Backup(ctx, "pre_purge")
	if err != nil {
		t.Fatalf("Backup failed: %v", err)
	}
	if backupPath != dbPath+".bak.pre_purge" {
		t.Fatalf("backupPath = %q, want %q", backupPath, dbPath+".bak.pre_purge")
	}

	walInfo, err := os.Stat(dbPath + "-wal")
	if err == nil && walInfo.Size() != 0 {
		t.Fatalf("expected truncated WAL after Backup/Checkpoint, got size %d", walInfo.Size())
	}

	backupDB, err := sqlite.Open(ctx, backupPath)
	if err != nil {
		t.Fatalf("open backup db failed: %v", err)
	}
	defer func() { _ = backupDB.Close() }()

	gotVal, err := sqlite.NewSyncRepository(backupDB.SQL()).GetSetting(ctx, "session_secret")
	if err != nil || gotVal != "super-secret" {
		t.Fatalf("backup setting = (%q, %v), want (super-secret, nil)", gotVal, err)
	}

	// Blocked WAL checkpoint (active reader holding older snapshot while newer WAL frame exists) must error.
	shortDB, err := sqlite.Open(ctx, dbPath)
	if err != nil {
		t.Fatalf("open shortDB failed: %v", err)
	}
	defer func() { _ = shortDB.Close() }()
	shortDB.SQL().SetMaxOpenConns(1)
	if _, err := shortDB.SQL().ExecContext(ctx, `PRAGMA busy_timeout = 50`); err != nil {
		t.Fatalf("set short busy_timeout failed: %v", err)
	}

	readTx, err := db.SQL().BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		t.Fatalf("begin read snapshot failed: %v", err)
	}
	var dummy string
	if err := readTx.QueryRowContext(ctx, `SELECT value FROM app_settings WHERE key = 'session_secret'`).Scan(&dummy); err != nil {
		_ = readTx.Rollback()
		t.Fatalf("read snapshot query failed: %v", err)
	}
	if err := sqlite.NewSyncRepository(shortDB.SQL()).SetSetting(ctx, "wal_block", "1", time.Now()); err != nil {
		_ = readTx.Rollback()
		t.Fatalf("write newer WAL frame failed: %v", err)
	}
	if err := shortDB.Checkpoint(ctx); err == nil {
		_ = readTx.Rollback()
		t.Fatal("expected Checkpoint error when WAL reader blocks truncation")
	}
	if _, err := shortDB.Backup(ctx, "blocked"); err == nil {
		_ = readTx.Rollback()
		t.Fatal("expected Backup error when Checkpoint is blocked")
	}
	if err := readTx.Rollback(); err != nil {
		t.Fatalf("rollback read snapshot failed: %v", err)
	}
	if err := shortDB.Checkpoint(ctx); err != nil {
		t.Fatalf("Checkpoint after releasing reader failed: %v", err)
	}
}

func TestDB_Integration_MigrationsAndPreMigrationBackup(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	ctx := context.Background()

	t.Run("fresh db creates schema without backup and is idempotent", func(t *testing.T) {
		dir := t.TempDir()
		dbPath := filepath.Join(dir, "photos.db")
		db := openTestDB(t, dbPath)

		matches, err := filepath.Glob(dbPath + ".bak.*")
		if err != nil || len(matches) != 0 {
			t.Fatalf("expected 0 backup files on initial migrate, got %v (err=%v)", matches, err)
		}
		if err := db.Migrate(ctx); err != nil {
			t.Fatalf("second Migrate failed: %v", err)
		}
		matches, _ = filepath.Glob(dbPath + ".bak.*")
		if len(matches) != 0 {
			t.Fatalf("expected 0 backup files on idempotent migrate, got %v", matches)
		}

		var idxCount int
		err = db.SQL().QueryRowContext(
			ctx,
			`SELECT COUNT(*) FROM sqlite_master WHERE type = 'index' AND name LIKE 'idx_%'`,
		).Scan(&idxCount)
		if err != nil || idxCount != 10 {
			t.Fatalf("custom index count = %d (err=%v), want 10", idxCount, err)
		}
	})

	t.Run("creates v1 backup before synthetic v2 and rolls back broken v3", func(t *testing.T) {
		dir := t.TempDir()
		dbPath := filepath.Join(dir, "photos.db")
		db := openTestDB(t, dbPath)

		v1 := sqlite.Migration{Version: 1, Name: "001_initial_schema.sql", SQL: "-- already applied"}
		v2 := sqlite.Migration{Version: 2, Name: "002_add_notes.sql", SQL: `CREATE TABLE notes (id INTEGER PRIMARY KEY, body TEXT NOT NULL);`}
		if err := db.MigrateWith(ctx, []sqlite.Migration{v1, v2}); err != nil {
			t.Fatalf("MigrateWith v2 failed: %v", err)
		}

		backups, err := filepath.Glob(dbPath + ".bak.v1_*")
		if err != nil || len(backups) != 1 {
			t.Fatalf("expected 1 v1 pre-migration backup, got %v (err=%v)", backups, err)
		}

		v3Broken := sqlite.Migration{
			Version: 3,
			Name:    "003_broken.sql",
			SQL:     `ALTER TABLE notes ADD COLUMN tag TEXT; INVALID SQL HERE;`,
		}
		if err := db.MigrateWith(ctx, []sqlite.Migration{v1, v2, v3Broken}); err == nil {
			t.Fatal("expected error from broken v3 migration")
		}

		var maxVersion int
		if err := db.SQL().QueryRowContext(ctx, `SELECT MAX(version) FROM schema_migrations`).Scan(&maxVersion); err != nil {
			t.Fatalf("query schema_migrations failed: %v", err)
		}
		if maxVersion != 2 {
			t.Fatalf("maxVersion = %d, want 2 after v3 rollback", maxVersion)
		}
	})

	t.Run("creates v0 backup when existing tables lack schema_migrations and rejects invalid versions", func(t *testing.T) {
		dir := t.TempDir()
		dbPath := filepath.Join(dir, "legacy.db")
		db, err := sqlite.Open(ctx, dbPath)
		if err != nil {
			t.Fatalf("Open failed: %v", err)
		}
		defer func() { _ = db.Close() }()

		if _, err := db.SQL().ExecContext(ctx, `CREATE TABLE legacy_items (id TEXT PRIMARY KEY)`); err != nil {
			t.Fatalf("create legacy table failed: %v", err)
		}
		if err := db.Migrate(ctx); err != nil {
			t.Fatalf("Migrate on legacy DB failed: %v", err)
		}
		backups, err := filepath.Glob(dbPath + ".bak.v0_*")
		if err != nil || len(backups) != 1 {
			t.Fatalf("expected 1 v0 backup, got %v (err=%v)", backups, err)
		}

		if err := db.MigrateWith(ctx, []sqlite.Migration{{Version: 0, Name: "bad"}}); err == nil {
			t.Fatal("expected error for version 0")
		}
		if err := db.MigrateWith(ctx, []sqlite.Migration{{Version: 1, Name: "a"}, {Version: 1, Name: "b"}}); err == nil {
			t.Fatal("expected error for duplicate version")
		}
	})
}

func TestDB_Integration_ForeignKeysAndConstraints(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	ctx := context.Background()
	db := openTestDB(t, filepath.Join(t.TempDir(), "photos.db"))
	photoRepo := sqlite.NewPhotoRepository(db.SQL())
	viewRepo := sqlite.NewViewRepository(db.SQL())
	now := time.Date(2026, 4, 1, 10, 0, 0, 0, time.UTC)

	p := domain.Photo{
		ID:         "p1",
		StackKey:   "trip/photo/img_1",
		BaseName:   "IMG_1",
		FolderPath: "trip",
		MediaType:  domain.MediaTypePhoto,
		Status:     domain.FileStatusActive,
		Files: []domain.PhotoFile{
			{
				ID:           "f1",
				ContentHash:  "hash1",
				RelativePath: "trip/IMG_1.jpg",
				FileName:     "IMG_1.jpg",
				FolderPath:   "trip",
				Format:       domain.FormatJPG,
				FileSize:     100,
				ModTime:      now,
				Status:       domain.FileStatusActive,
			},
		},
		EmojiTags: []string{"❤️"},
		CreatedAt: now,
		UpdatedAt: now,
	}
	if err := photoRepo.CreatePhoto(ctx, p); err != nil {
		t.Fatalf("CreatePhoto failed: %v", err)
	}
	if _, err := viewRepo.RecordView(ctx, domain.ViewEvent{
		ViewID:   "v1",
		PhotoID:  "p1",
		ViewedAt: now,
		Source:   domain.ViewSourceDwell,
	}); err != nil {
		t.Fatalf("RecordView failed: %v", err)
	}

	// Duplicate relative_path violates UNIQUE(relative_path).
	if err := photoRepo.AttachFile(ctx, "p1", domain.PhotoFile{
		ID:           "f2",
		ContentHash:  "hash2",
		RelativePath: "trip/IMG_1.jpg",
		FileName:     "IMG_1.jpg",
		FolderPath:   "trip",
		Format:       domain.FormatJPG,
		FileSize:     100,
		ModTime:      now,
	}); err == nil {
		t.Fatal("expected UNIQUE(relative_path) violation")
	}

	// Negative view_count violates CHECK(view_count >= 0).
	if _, err := db.SQL().ExecContext(ctx, `UPDATE photos SET view_count = -1 WHERE id = 'p1'`); err == nil {
		t.Fatal("expected CHECK (view_count >= 0) violation")
	}

	// Deleting photo cascades to photo_files, photo_views, and photo_tags.
	if _, err := db.SQL().ExecContext(ctx, `DELETE FROM photos WHERE id = 'p1'`); err != nil {
		t.Fatalf("delete photo failed: %v", err)
	}
	for _, tbl := range []string{"photo_files", "photo_views", "photo_tags"} {
		var cnt int
		if err := db.SQL().QueryRowContext(ctx, `SELECT COUNT(*) FROM `+tbl).Scan(&cnt); err != nil {
			t.Fatalf("count %s failed: %v", tbl, err)
		}
		if cnt != 0 {
			t.Fatalf("%s count = %d after photo delete, want 0", tbl, cnt)
		}
	}
}

func TestDB_Integration_ErrorPaths(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	ctx := context.Background()
	dir := t.TempDir()

	// Parent path is a regular file -> MkdirAll fails.
	blockerFile := filepath.Join(dir, "not-a-dir")
	if err := os.WriteFile(blockerFile, []byte("x"), 0o600); err != nil {
		t.Fatalf("WriteFile failed: %v", err)
	}
	if _, err := sqlite.Open(ctx, filepath.Join(blockerFile, "photos.db")); err == nil {
		t.Fatal("expected Open error when parent path is a file")
	}

	// Target path is an existing directory -> PingContext fails.
	subDir := filepath.Join(dir, "existing-dir")
	if err := os.MkdirAll(subDir, 0o755); err != nil {
		t.Fatalf("MkdirAll failed: %v", err)
	}
	if _, err := sqlite.Open(ctx, subDir); err == nil {
		t.Fatal("expected Open error when target path is a directory")
	}

	// Operations on a closed DB fail cleanly.
	dbPath := filepath.Join(dir, "closed.db")
	db, err := sqlite.Open(ctx, dbPath)
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	if err := db.Close(); err != nil {
		t.Fatalf("Close failed: %v", err)
	}
	if err := db.Checkpoint(ctx); err == nil {
		t.Fatal("expected Checkpoint error on closed DB")
	}
	if _, err := db.Backup(ctx, "after_close"); err == nil {
		t.Fatal("expected Backup error on closed DB")
	}
	if err := db.Migrate(ctx); err == nil {
		t.Fatal("expected Migrate error on closed DB")
	}

	// Corrupted schema_migrations row causes loadAppliedVersions scan error.
	corruptPath := filepath.Join(dir, "corrupt_migrations.db")
	corruptDB, err := sqlite.Open(ctx, corruptPath)
	if err != nil {
		t.Fatalf("Open corruptDB failed: %v", err)
	}
	defer func() { _ = corruptDB.Close() }()
	if _, err := corruptDB.SQL().ExecContext(ctx, `CREATE TABLE schema_migrations (version TEXT PRIMARY KEY, applied_at_unix INTEGER NOT NULL); INSERT INTO schema_migrations VALUES ('bad', 1);`); err != nil {
		t.Fatalf("seed corrupt schema_migrations failed: %v", err)
	}
	if err := corruptDB.Migrate(ctx); err == nil {
		t.Fatal("expected Migrate error on non-integer schema_migrations version")
	}
}

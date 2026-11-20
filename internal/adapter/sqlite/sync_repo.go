package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/lucastheo/photos/internal/domain"
)

const syncRunSelectCols = `id, started_at_unix, finished_at_unix, status,
	scanned_count, added_count, updated_count, moved_count,
	stacked_count, duplicate_count, unchanged_count, missing_count, error_message`

// SyncRepository persists library synchronization runs and application settings
// in SQLite.
type SyncRepository struct {
	db *sql.DB
}

// NewSyncRepository returns a concrete SyncRepository backed by db.
func NewSyncRepository(db *sql.DB) *SyncRepository {
	return &SyncRepository{db: db}
}

// CreateSyncRun inserts a new SyncReport row into sync_runs.
func (r *SyncRepository) CreateSyncRun(ctx context.Context, report domain.SyncReport) error {
	if report.StartedAt.IsZero() {
		report.StartedAt = time.Now().UTC()
	}
	if report.Status == "" {
		report.Status = domain.SyncStatusRunning
	}
	e := syncReportToEntity(report)
	_, err := r.db.ExecContext(ctx, `
		INSERT INTO sync_runs (
			id, started_at_unix, finished_at_unix, status,
			scanned_count, added_count, updated_count, moved_count,
			stacked_count, duplicate_count, unchanged_count, missing_count, error_message
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		e.ID, e.StartedAtUnix, e.FinishedAtUnix, e.Status,
		e.ScannedCount, e.AddedCount, e.UpdatedCount, e.MovedCount,
		e.StackedCount, e.DuplicateCount, e.UnchangedCount, e.MissingCount, e.ErrorMessage,
	)
	if err != nil {
		return fmt.Errorf("insert sync_run %q: %w", report.ID, err)
	}
	return nil
}

// UpdateSyncRun updates counters, completion status, and error message for an
// existing SyncReport, returning domain.ErrSyncNotFound if no row matches.
func (r *SyncRepository) UpdateSyncRun(ctx context.Context, report domain.SyncReport) error {
	e := syncReportToEntity(report)
	res, err := r.db.ExecContext(ctx, `
		UPDATE sync_runs
		SET finished_at_unix = ?, status = ?,
			scanned_count = ?, added_count = ?, updated_count = ?, moved_count = ?,
			stacked_count = ?, duplicate_count = ?, unchanged_count = ?, missing_count = ?, error_message = ?
		WHERE id = ?`,
		e.FinishedAtUnix, e.Status,
		e.ScannedCount, e.AddedCount, e.UpdatedCount, e.MovedCount,
		e.StackedCount, e.DuplicateCount, e.UnchangedCount, e.MissingCount, e.ErrorMessage,
		e.ID,
	)
	if err != nil {
		return fmt.Errorf("update sync_run %q: %w", report.ID, err)
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("check sync_run rows affected: %w", err)
	}
	if affected == 0 {
		return domain.ErrSyncNotFound
	}
	return nil
}

// GetLatestSyncRun returns the most recently started SyncReport or
// domain.ErrSyncNotFound if no sync runs have been recorded.
func (r *SyncRepository) GetLatestSyncRun(ctx context.Context) (domain.SyncReport, error) {
	query := `SELECT ` + syncRunSelectCols + `
		FROM sync_runs
		ORDER BY started_at_unix DESC, rowid DESC
		LIMIT 1`
	e, err := scanSyncRunRow(r.db.QueryRowContext(ctx, query))
	if errors.Is(err, sql.ErrNoRows) {
		return domain.SyncReport{}, domain.ErrSyncNotFound
	}
	if err != nil {
		return domain.SyncReport{}, fmt.Errorf("get latest sync_run: %w", err)
	}
	return syncReportFromEntity(e), nil
}

// ListSyncRuns returns recent SyncReports ordered newest first.
func (r *SyncRepository) ListSyncRuns(ctx context.Context, limit int) ([]domain.SyncReport, error) {
	if limit <= 0 {
		limit = 20
	}
	query := `SELECT ` + syncRunSelectCols + `
		FROM sync_runs
		ORDER BY started_at_unix DESC, rowid DESC
		LIMIT ?`
	rows, err := r.db.QueryContext(ctx, query, limit)
	if err != nil {
		return nil, fmt.Errorf("list sync_runs: %w", err)
	}
	defer func() { _ = rows.Close() }()

	out := make([]domain.SyncReport, 0)
	for rows.Next() {
		e, err := scanSyncRunRow(rows)
		if err != nil {
			return nil, fmt.Errorf("scan sync_run: %w", err)
		}
		out = append(out, syncReportFromEntity(e))
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate sync_runs: %w", err)
	}
	return out, nil
}

// RecoverStaleRuns transitions any sync_runs rows still marked running into
// failed so interrupted runs never block subsequent syncs.
func (r *SyncRepository) RecoverStaleRuns(ctx context.Context, reason string, now time.Time) (int64, error) {
	if now.IsZero() {
		now = time.Now().UTC()
	}
	cleanReason := strings.TrimSpace(reason)
	if cleanReason == "" {
		cleanReason = "interrupted by server restart"
	}
	res, err := r.db.ExecContext(ctx, `
		UPDATE sync_runs
		SET status = ?, finished_at_unix = ?, error_message = ?
		WHERE status = ?`,
		string(domain.SyncStatusFailed),
		now.UTC().Unix(),
		cleanReason,
		string(domain.SyncStatusRunning),
	)
	if err != nil {
		return 0, fmt.Errorf("recover stale sync_runs: %w", err)
	}
	return res.RowsAffected()
}

// GetSetting retrieves an application setting value by key or returns
// domain.ErrSettingNotFound if absent.
func (r *SyncRepository) GetSetting(ctx context.Context, key string) (string, error) {
	var value string
	err := r.db.QueryRowContext(ctx, `SELECT value FROM app_settings WHERE key = ?`, key).Scan(&value)
	if errors.Is(err, sql.ErrNoRows) {
		return "", domain.ErrSettingNotFound
	}
	if err != nil {
		return "", fmt.Errorf("get setting %q: %w", key, err)
	}
	return value, nil
}

// SetSetting inserts or updates an application setting key/value pair.
func (r *SyncRepository) SetSetting(ctx context.Context, key, value string, now time.Time) error {
	if strings.TrimSpace(key) == "" {
		return errors.New("setting key cannot be empty")
	}
	if now.IsZero() {
		now = time.Now().UTC()
	}
	_, err := r.db.ExecContext(ctx, `
		INSERT INTO app_settings (key, value, updated_at_unix)
		VALUES (?, ?, ?)
		ON CONFLICT(key) DO UPDATE SET
			value = excluded.value,
			updated_at_unix = excluded.updated_at_unix`,
		key, value, now.UTC().Unix(),
	)
	if err != nil {
		return fmt.Errorf("set setting %q: %w", key, err)
	}
	return nil
}

func scanSyncRunRow(s rowScanner) (syncRunEntity, error) {
	var e syncRunEntity
	err := s.Scan(
		&e.ID, &e.StartedAtUnix, &e.FinishedAtUnix, &e.Status,
		&e.ScannedCount, &e.AddedCount, &e.UpdatedCount, &e.MovedCount,
		&e.StackedCount, &e.DuplicateCount, &e.UnchangedCount, &e.MissingCount, &e.ErrorMessage,
	)
	return e, err
}

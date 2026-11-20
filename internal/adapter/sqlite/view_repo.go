package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/lucastheo/photos/internal/domain"
)

// ViewRepository records view events, performs manual view adjustments, and
// queries view history while keeping photos.view_count synchronized with photo_views.
type ViewRepository struct {
	db *sql.DB
}

// NewViewRepository returns a concrete ViewRepository backed by db.
func NewViewRepository(db *sql.DB) *ViewRepository {
	return &ViewRepository{db: db}
}

// RecordView inserts a timestamped view event (idempotently when ViewID is set)
// and synchronizes photos.view_count and last_viewed_unix_ns from photo_views.
func (r *ViewRepository) RecordView(ctx context.Context, event domain.ViewEvent) (domain.Photo, error) {
	if !event.Source.IsValid() {
		return domain.Photo{}, fmt.Errorf("%w: %q", domain.ErrInvalidViewSource, event.Source)
	}
	if event.ViewedAt.IsZero() {
		event.ViewedAt = time.Now().UTC()
	}

	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return domain.Photo{}, fmt.Errorf("begin record view tx: %w", err)
	}
	if err := ensurePhotoExists(ctx, tx.QueryRowContext, event.PhotoID); err != nil {
		return domain.Photo{}, errors.Join(err, tx.Rollback())
	}

	inserted, err := insertViewEventTx(ctx, tx, event)
	if err != nil {
		return domain.Photo{}, errors.Join(err, tx.Rollback())
	}
	if inserted {
		if err := syncPhotoViewSummaryTx(ctx, tx, event.PhotoID, event.ViewedAt); err != nil {
			return domain.Photo{}, errors.Join(err, tx.Rollback())
		}
	}

	photo, err := loadHydratedPhotoByID(ctx, tx.QueryRowContext, tx.QueryContext, event.PhotoID)
	if err != nil {
		return domain.Photo{}, errors.Join(err, tx.Rollback())
	}
	if err := tx.Commit(); err != nil {
		return domain.Photo{}, fmt.Errorf("commit record view tx: %w", err)
	}
	return photo, nil
}

// AdjustViewCount performs a manual increment, decrement, or reset operation
// on photoID while keeping photos.view_count strictly equal to COUNT(photo_views).
func (r *ViewRepository) AdjustViewCount(
	ctx context.Context,
	photoID string,
	action domain.ViewAction,
	now time.Time,
) (domain.Photo, error) {
	if !action.IsValid() {
		return domain.Photo{}, fmt.Errorf("%w: %q", domain.ErrInvalidViewAction, action)
	}
	if now.IsZero() {
		now = time.Now().UTC()
	}

	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return domain.Photo{}, fmt.Errorf("begin adjust view tx: %w", err)
	}
	if err := ensurePhotoExists(ctx, tx.QueryRowContext, photoID); err != nil {
		return domain.Photo{}, errors.Join(err, tx.Rollback())
	}
	if err := applyViewAdjustmentTx(ctx, tx, photoID, action, now); err != nil {
		return domain.Photo{}, errors.Join(err, tx.Rollback())
	}
	if err := syncPhotoViewSummaryTx(ctx, tx, photoID, now); err != nil {
		return domain.Photo{}, errors.Join(err, tx.Rollback())
	}

	photo, err := loadHydratedPhotoByID(ctx, tx.QueryRowContext, tx.QueryContext, photoID)
	if err != nil {
		return domain.Photo{}, errors.Join(err, tx.Rollback())
	}
	if err := tx.Commit(); err != nil {
		return domain.Photo{}, fmt.Errorf("commit adjust view tx: %w", err)
	}
	return photo, nil
}

// ListViewHistory returns recorded ViewEvents for photoID ordered newest first.
func (r *ViewRepository) ListViewHistory(ctx context.Context, photoID string, limit int) ([]domain.ViewEvent, error) {
	if err := ensurePhotoExists(ctx, r.db.QueryRowContext, photoID); err != nil {
		return nil, err
	}

	query := `SELECT id, view_id, photo_id, viewed_at_unix_ns, source
		FROM photo_views
		WHERE photo_id = ?
		ORDER BY viewed_at_unix_ns DESC, id DESC`
	args := []any{photoID}
	if limit > 0 {
		query += ` LIMIT ?`
		args = append(args, limit)
	}

	rows, err := r.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("query view history: %w", err)
	}
	defer func() { _ = rows.Close() }()

	events := make([]domain.ViewEvent, 0)
	for rows.Next() {
		var e photoViewEntity
		if err := rows.Scan(&e.ID, &e.ViewID, &e.PhotoID, &e.ViewedAtUnixNs, &e.Source); err != nil {
			return nil, fmt.Errorf("scan photo_view: %w", err)
		}
		events = append(events, viewEventFromEntity(e))
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate photo_views: %w", err)
	}
	return events, nil
}

func insertViewEventTx(ctx context.Context, tx *sql.Tx, event domain.ViewEvent) (bool, error) {
	e := viewEventToEntity(event)
	if !e.ViewID.Valid {
		if _, err := tx.ExecContext(
			ctx,
			`INSERT INTO photo_views (view_id, photo_id, viewed_at_unix_ns, source) VALUES (NULL, ?, ?, ?)`,
			e.PhotoID, e.ViewedAtUnixNs, e.Source,
		); err != nil {
			return false, fmt.Errorf("insert photo_view: %w", err)
		}
		return true, nil
	}

	res, err := tx.ExecContext(
		ctx,
		`INSERT INTO photo_views (view_id, photo_id, viewed_at_unix_ns, source)
		VALUES (?, ?, ?, ?)
		ON CONFLICT(view_id) DO NOTHING`,
		e.ViewID, e.PhotoID, e.ViewedAtUnixNs, e.Source,
	)
	if err != nil {
		return false, fmt.Errorf("insert idempotent photo_view: %w", err)
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("check photo_view rows affected: %w", err)
	}
	return affected > 0, nil
}

func applyViewAdjustmentTx(
	ctx context.Context,
	tx *sql.Tx,
	photoID string,
	action domain.ViewAction,
	now time.Time,
) error {
	switch action {
	case domain.ViewActionIncrement:
		_, err := tx.ExecContext(
			ctx,
			`INSERT INTO photo_views (view_id, photo_id, viewed_at_unix_ns, source) VALUES (NULL, ?, ?, ?)`,
			photoID, timeToUnixNs(now), string(domain.ViewSourceManual),
		)
		if err != nil {
			return fmt.Errorf("insert manual increment view: %w", err)
		}
	case domain.ViewActionDecrement:
		_, err := tx.ExecContext(
			ctx,
			`DELETE FROM photo_views
			WHERE id = (
				SELECT id FROM photo_views
				WHERE photo_id = ?
				ORDER BY viewed_at_unix_ns DESC, id DESC
				LIMIT 1
			)`,
			photoID,
		)
		if err != nil {
			return fmt.Errorf("delete latest view for decrement: %w", err)
		}
	case domain.ViewActionReset:
		if _, err := tx.ExecContext(ctx, `DELETE FROM photo_views WHERE photo_id = ?`, photoID); err != nil {
			return fmt.Errorf("reset photo views: %w", err)
		}
	}
	return nil
}

func syncPhotoViewSummaryTx(ctx context.Context, tx *sql.Tx, photoID string, now time.Time) error {
	_, err := tx.ExecContext(
		ctx,
		`UPDATE photos
		SET view_count = (SELECT COUNT(*) FROM photo_views WHERE photo_id = ?),
			last_viewed_unix_ns = (SELECT MAX(viewed_at_unix_ns) FROM photo_views WHERE photo_id = ?),
			updated_at_unix = ?
		WHERE id = ?`,
		photoID, photoID, timeToUnix(now), photoID,
	)
	if err != nil {
		return fmt.Errorf("sync photo view summary: %w", err)
	}
	return nil
}

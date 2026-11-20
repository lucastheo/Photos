package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/lucastheo/photos/internal/domain"
)

// UpdatePhotoMetadata updates dimensions, playback duration, capture timestamp,
// and EXIF metadata for an existing logical Photo record.
func (r *PhotoRepository) UpdatePhotoMetadata(ctx context.Context, photo domain.Photo) error {
	if strings.TrimSpace(photo.ID) == "" {
		return domain.ErrPhotoNotFound
	}
	updatedAt := photo.UpdatedAt
	if updatedAt.IsZero() {
		updatedAt = time.Now().UTC()
	}
	clone := photo
	clone.UpdatedAt = updatedAt
	e := photoToEntity(clone)

	res, err := r.db.ExecContext(ctx, `
		UPDATE photos
		SET width = ?, height = ?, orientation = ?, duration_sec = ?, taken_at_unix = ?,
			camera_make = ?, camera_model = ?, lens_model = ?, iso = ?, f_number = ?,
			exposure_time = ?, focal_length_mm = ?, gps_latitude = ?, gps_longitude = ?,
			updated_at_unix = ?
		WHERE id = ?`,
		e.Width, e.Height, e.Orientation, e.DurationSec, e.TakenAtUnix,
		e.CameraMake, e.CameraModel, e.LensModel, e.ISO, e.FNumber,
		e.ExposureTime, e.FocalLengthMM, e.GPSLatitude, e.GPSLongitude,
		e.UpdatedAtUnix, e.ID,
	)
	if err != nil {
		return fmt.Errorf("update photo metadata %q: %w", photo.ID, err)
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("check updated photo metadata rows: %w", err)
	}
	if affected == 0 {
		return domain.ErrPhotoNotFound
	}
	return nil
}

// SplitMovedFile transactionally splits a moved variant file out of an existing
// multi-file stack into a new standalone Photo preserving view count, view history,
// and emoji tags while leaving remaining files on the original Photo.
func (r *PhotoRepository) SplitMovedFile(
	ctx context.Context,
	fileID string,
	newPhoto domain.Photo,
	move domain.FileMove,
	now time.Time,
) error {
	normMove, cleanNow, err := prepareSplitInputs(fileID, newPhoto.ID, move, now)
	if err != nil {
		return err
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin split moved file tx: %w", err)
	}
	if err := executeSplitMovedFileTx(ctx, tx, fileID, newPhoto, normMove, cleanNow); err != nil {
		return errors.Join(err, tx.Rollback())
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit split moved file tx: %w", err)
	}
	return nil
}

func prepareSplitInputs(fileID, newPhotoID string, move domain.FileMove, now time.Time) (domain.FileMove, time.Time, error) {
	if strings.TrimSpace(fileID) == "" || strings.TrimSpace(newPhotoID) == "" {
		return domain.FileMove{}, time.Time{}, errors.New("split moved file requires non-empty fileID and newPhoto.ID")
	}
	move.FileID = fileID
	normMoves, err := normalizeFileMoves([]FileMove{move})
	if err != nil {
		return domain.FileMove{}, time.Time{}, err
	}
	if now.IsZero() {
		now = time.Now().UTC()
	}
	return normMoves[0], now.UTC(), nil
}

func executeSplitMovedFileTx(
	ctx context.Context,
	tx *sql.Tx,
	fileID string,
	newPhoto domain.Photo,
	normMove domain.FileMove,
	now time.Time,
) error {
	oldPhotoID, fileFmt, err := lookupSplittingFileTx(ctx, tx, fileID)
	if err != nil {
		return err
	}
	oldPhoto, err := loadHydratedPhotoByID(ctx, tx.QueryRowContext, tx.QueryContext, oldPhotoID)
	if err != nil {
		return err
	}
	splitPhoto := buildSplitPhoto(oldPhoto, newPhoto, normMove, fileFmt, now)
	if err := persistSplitPhotoTx(ctx, tx, oldPhotoID, splitPhoto, now); err != nil {
		return err
	}
	if err := reassignMovedFileTx(ctx, tx, splitPhoto.ID, normMove, now.Unix()); err != nil {
		return err
	}
	if err := refreshSinglePhotoParentTx(ctx, tx, oldPhotoID, now.Unix()); err != nil {
		return err
	}
	return recomputePhotoStatusChunksTx(ctx, tx, []string{oldPhotoID}, now.Unix())
}

func lookupSplittingFileTx(ctx context.Context, tx *sql.Tx, fileID string) (string, domain.MediaFormat, error) {
	var oldPhotoID, rawFormat string
	err := tx.QueryRowContext(ctx, `SELECT photo_id, format FROM photo_files WHERE id = ?`, fileID).Scan(&oldPhotoID, &rawFormat)
	if errors.Is(err, sql.ErrNoRows) {
		return "", "", fmt.Errorf("%w: %s", domain.ErrFileNotFound, fileID)
	}
	if err != nil {
		return "", "", fmt.Errorf("lookup splitting file %q: %w", fileID, err)
	}
	return oldPhotoID, domain.MediaFormat(rawFormat), nil
}

func buildSplitPhoto(
	oldPhoto, newPhoto domain.Photo,
	normMove domain.FileMove,
	fileFmt domain.MediaFormat,
	now time.Time,
) domain.Photo {
	out := newPhoto
	out.FolderPath = normMove.FolderPath
	if strings.TrimSpace(out.BaseName) == "" {
		out.BaseName = fileBaseStem(normMove.FileName)
	}
	if out.MediaType == "" {
		out.MediaType = oldPhoto.MediaType
	}
	out.StackKey = domain.ComputeStackKey(out.FolderPath, out.MediaType, out.BaseName, fileFmt)
	fillSplitMetadata(&out, oldPhoto)
	if out.ViewCount < oldPhoto.ViewCount {
		out.ViewCount = oldPhoto.ViewCount
	}
	if out.LastViewedAt == nil {
		out.LastViewedAt = oldPhoto.LastViewedAt
	}
	if len(out.EmojiTags) == 0 {
		out.EmojiTags = slices.Clone(oldPhoto.EmojiTags)
	}
	out.Status = domain.FileStatusActive
	if out.CreatedAt.IsZero() {
		out.CreatedAt = now
	}
	out.UpdatedAt = now
	return out
}

func fillSplitMetadata(out *domain.Photo, oldPhoto domain.Photo) {
	if out.Width == 0 {
		out.Width = oldPhoto.Width
		out.Height = oldPhoto.Height
		out.DurationSec = oldPhoto.DurationSec
	}
	if out.TakenAt == nil {
		out.TakenAt = oldPhoto.TakenAt
	}
	if out.EXIF.Orientation == 0 {
		out.EXIF = oldPhoto.EXIF
	}
}

func persistSplitPhotoTx(ctx context.Context, tx *sql.Tx, oldPhotoID string, splitPhoto domain.Photo, now time.Time) error {
	if err := insertPhotoTx(ctx, tx, splitPhoto); err != nil {
		return err
	}
	if err := insertPhotoTagsTx(ctx, tx, splitPhoto.ID, splitPhoto.EmojiTags, now); err != nil {
		return err
	}
	return clonePhotoViewsTx(ctx, tx, oldPhotoID, splitPhoto.ID, splitPhoto.ViewCount, splitPhoto.LastViewedAt, now)
}

func clonePhotoViewsTx(
	ctx context.Context,
	tx *sql.Tx,
	srcPhotoID, dstPhotoID string,
	targetCount int,
	lastViewed *time.Time,
	now time.Time,
) error {
	res, err := tx.ExecContext(ctx, `
		INSERT INTO photo_views (view_id, photo_id, viewed_at_unix_ns, source)
		SELECT NULL, ?, viewed_at_unix_ns, source
		FROM photo_views
		WHERE photo_id = ?`,
		dstPhotoID, srcPhotoID,
	)
	if err != nil {
		return fmt.Errorf("clone photo_views from %q to %q: %w", srcPhotoID, dstPhotoID, err)
	}
	cloned, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("count cloned photo_views: %w", err)
	}
	return padMissingPhotoViewsTx(ctx, tx, dstPhotoID, int64(targetCount)-cloned, lastViewed, now)
}

func padMissingPhotoViewsTx(
	ctx context.Context,
	tx *sql.Tx,
	photoID string,
	missing int64,
	lastViewed *time.Time,
	now time.Time,
) error {
	if missing <= 0 {
		return nil
	}
	ts := now
	if lastViewed != nil {
		ts = *lastViewed
	}
	for range missing {
		if _, err := tx.ExecContext(
			ctx,
			`INSERT INTO photo_views (view_id, photo_id, viewed_at_unix_ns, source) VALUES (NULL, ?, ?, ?)`,
			photoID, timeToUnixNs(ts), string(domain.ViewSourceManual),
		); err != nil {
			return fmt.Errorf("pad split photo_view for %q: %w", photoID, err)
		}
	}
	return nil
}

func reassignMovedFileTx(ctx context.Context, tx *sql.Tx, newPhotoID string, normMove domain.FileMove, nowUnix int64) error {
	_, err := tx.ExecContext(ctx, `
		UPDATE photo_files
		SET photo_id = ?, relative_path = ?, file_name = ?, folder_path = ?, status = ?, updated_at_unix = ?
		WHERE id = ?`,
		newPhotoID, normMove.RelativePath, normMove.FileName, normMove.FolderPath,
		string(domain.FileStatusActive), nowUnix, normMove.FileID,
	)
	if err != nil {
		return fmt.Errorf("reassign moved file %q to %q: %w", normMove.FileID, newPhotoID, err)
	}
	return nil
}

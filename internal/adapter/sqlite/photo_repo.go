package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"path"
	"strings"
	"time"

	"github.com/lucastheo/photos/internal/domain"
)

const (
	sqlInChunkSize = 500

	photoSelectCols = `p.id, p.stack_key, p.base_name, p.folder_path, p.media_type,
		p.width, p.height, p.orientation, p.duration_sec, p.taken_at_unix,
		p.camera_make, p.camera_model, p.lens_model, p.iso, p.f_number,
		p.exposure_time, p.focal_length_mm, p.gps_latitude, p.gps_longitude,
		p.view_count, p.last_viewed_unix_ns, p.status, p.created_at_unix, p.updated_at_unix`

	photoFileSelectCols = `id, photo_id, content_hash, relative_path, file_name,
		folder_path, format, file_size, mod_time_unix_ns, status, created_at_unix, updated_at_unix`
)

type (
	rowsQueryFunc func(ctx context.Context, query string, args ...any) (*sql.Rows, error)
	rowQueryFunc  func(ctx context.Context, query string, args ...any) *sql.Row
)

// FileMove describes a target relative path update for an existing PhotoFile.
type FileMove struct {
	// FileID is the unique identifier of the PhotoFile to move.
	FileID string
	// RelativePath is the destination path relative to the library root.
	RelativePath string
	// FileName is the destination base filename including extension.
	FileName string
	// FolderPath is the destination folder path relative to the library root.
	FolderPath string
}

// PhotoRepository persists and queries logical Photo aggregates, physical
// PhotoFile records, emoji tags, and library-wide statistics in SQLite.
type PhotoRepository struct {
	db *sql.DB
}

// NewPhotoRepository returns a concrete PhotoRepository backed by db.
func NewPhotoRepository(db *sql.DB) *PhotoRepository {
	return &PhotoRepository{db: db}
}

// CreatePhoto inserts a logical Photo along with its attached PhotoFile rows
// and normalized emoji tags in a single transaction.
func (r *PhotoRepository) CreatePhoto(ctx context.Context, photo domain.Photo) error {
	prepared := preparePhotoForInsert(photo)
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin create photo tx: %w", err)
	}
	if err := insertPhotoTx(ctx, tx, prepared); err != nil {
		return errors.Join(err, tx.Rollback())
	}
	for _, f := range prepared.Files {
		if err := insertPhotoFileTx(ctx, tx, prepareFileForPhoto(prepared, f)); err != nil {
			return errors.Join(err, tx.Rollback())
		}
	}
	if err := insertPhotoTagsTx(ctx, tx, prepared.ID, prepared.EmojiTags, prepared.CreatedAt); err != nil {
		return errors.Join(err, tx.Rollback())
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit create photo tx: %w", err)
	}
	return nil
}

// AttachFile inserts an additional PhotoFile attached to an existing logical Photo.
func (r *PhotoRepository) AttachFile(ctx context.Context, photoID string, file domain.PhotoFile) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin attach file tx: %w", err)
	}
	if err := ensurePhotoExists(ctx, tx.QueryRowContext, photoID); err != nil {
		return errors.Join(err, tx.Rollback())
	}
	prepared := prepareAttachedFile(photoID, file)
	if err := insertPhotoFileTx(ctx, tx, prepared); err != nil {
		return errors.Join(err, tx.Rollback())
	}
	if prepared.Status == domain.FileStatusActive {
		if _, err := tx.ExecContext(
			ctx,
			`UPDATE photos SET status = ?, updated_at_unix = ? WHERE id = ?`,
			string(domain.FileStatusActive),
			timeToUnix(prepared.UpdatedAt),
			photoID,
		); err != nil {
			return errors.Join(fmt.Errorf("reactivate parent photo: %w", err), tx.Rollback())
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit attach file tx: %w", err)
	}
	return nil
}

// UpdateFileInPlace updates an existing PhotoFile's hash, size, mtime, and status
// in place while preserving its owning PhotoID.
func (r *PhotoRepository) UpdateFileInPlace(ctx context.Context, file domain.PhotoFile) error {
	status := file.Status
	if status == "" {
		status = domain.FileStatusActive
	}
	if !status.IsValid() {
		return fmt.Errorf("invalid file status %q", status)
	}
	updatedAt := file.UpdatedAt
	if updatedAt.IsZero() {
		updatedAt = time.Now().UTC()
	}

	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin update file tx: %w", err)
	}
	photoID, err := updateFileInPlaceTx(ctx, tx, file, status, updatedAt)
	if err != nil {
		return errors.Join(err, tx.Rollback())
	}
	if err := recomputePhotoStatusChunksTx(ctx, tx, []string{photoID}, timeToUnix(updatedAt)); err != nil {
		return errors.Join(err, tx.Rollback())
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit update file tx: %w", err)
	}
	return nil
}

// MoveFilesBatch applies a batch of file moves/renames inside one transaction
// using a two-step temporary rename so rename chains never collide on UNIQUE(relative_path).
func (r *PhotoRepository) MoveFilesBatch(ctx context.Context, moves []FileMove, now time.Time) error {
	if len(moves) == 0 {
		return nil
	}
	if now.IsZero() {
		now = time.Now().UTC()
	}
	normMoves, err := normalizeFileMoves(moves)
	if err != nil {
		return err
	}

	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin move files tx: %w", err)
	}
	photoIDs, err := stageTempMovesTx(ctx, tx, normMoves, now.UnixNano())
	if err != nil {
		return errors.Join(err, tx.Rollback())
	}
	if err := applyFinalMovesTx(ctx, tx, normMoves, now.Unix()); err != nil {
		return errors.Join(err, tx.Rollback())
	}
	if err := refreshMovedPhotoParentsTx(ctx, tx, photoIDs, now.Unix()); err != nil {
		return errors.Join(err, tx.Rollback())
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit move files tx: %w", err)
	}
	return nil
}

// MarkFilesMissing marks the specified PhotoFile IDs as missing and sets any
// affected parent Photo to missing when zero active files remain for it.
func (r *PhotoRepository) MarkFilesMissing(ctx context.Context, fileIDs []string, now time.Time) error {
	cleanIDs := uniqueNonEmptyStrings(fileIDs)
	if len(cleanIDs) == 0 {
		return nil
	}
	if now.IsZero() {
		now = time.Now().UTC()
	}

	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin mark missing tx: %w", err)
	}
	photoIDs, err := markFileChunksMissingTx(ctx, tx, cleanIDs, now.Unix())
	if err != nil {
		return errors.Join(err, tx.Rollback())
	}
	if err := recomputePhotoStatusChunksTx(ctx, tx, photoIDs, now.Unix()); err != nil {
		return errors.Join(err, tx.Rollback())
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit mark missing tx: %w", err)
	}
	return nil
}

// GetByID retrieves a logical Photo by ID with hydrated Files, Formats, and EmojiTags.
func (r *PhotoRepository) GetByID(ctx context.Context, photoID string) (domain.Photo, error) {
	tx, err := r.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return domain.Photo{}, fmt.Errorf("begin get photo tx: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	return loadHydratedPhotoByID(ctx, tx.QueryRowContext, tx.QueryContext, photoID)
}

// GetByStackKey retrieves a logical Photo matching stackKey, preferring active records.
func (r *PhotoRepository) GetByStackKey(ctx context.Context, stackKey string) (domain.Photo, error) {
	tx, err := r.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return domain.Photo{}, fmt.Errorf("begin get photo by stack key tx: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	query := `SELECT ` + photoSelectCols + `
		FROM photos p
		WHERE p.stack_key = ?
		ORDER BY CASE WHEN p.status = 'active' THEN 0 ELSE 1 END, p.created_at_unix ASC
		LIMIT 1`
	entity, err := scanSinglePhoto(tx.QueryRowContext(ctx, query, stackKey))
	if err != nil {
		return domain.Photo{}, err
	}
	hydrated, err := hydratePhotos(ctx, tx.QueryContext, []photoEntity{entity})
	if err != nil {
		return domain.Photo{}, err
	}
	return hydrated[0], nil
}

// FindFilesByContentHash returns all PhotoFile records matching contentHash,
// ordered with active files first and then by relative path.
func (r *PhotoRepository) FindFilesByContentHash(ctx context.Context, contentHash string) ([]domain.PhotoFile, error) {
	query := `SELECT ` + photoFileSelectCols + `
		FROM photo_files
		WHERE content_hash = ?
		ORDER BY CASE WHEN status = 'active' THEN 0 ELSE 1 END, relative_path ASC`
	return queryPhotoFiles(ctx, r.db.QueryContext, query, contentHash)
}

// ListAllFiles returns every PhotoFile record ordered by relative_path ascending.
func (r *PhotoRepository) ListAllFiles(ctx context.Context) ([]domain.PhotoFile, error) {
	query := `SELECT ` + photoFileSelectCols + ` FROM photo_files ORDER BY relative_path ASC`
	return queryPhotoFiles(ctx, r.db.QueryContext, query)
}

func preparePhotoForInsert(photo domain.Photo) domain.Photo {
	out := photo
	if out.CreatedAt.IsZero() {
		out.CreatedAt = time.Now().UTC()
	}
	if out.UpdatedAt.IsZero() {
		out.UpdatedAt = out.CreatedAt
	}
	if out.Status == "" {
		out.Status = domain.FileStatusActive
	}
	if out.FolderPath == "" && len(out.Files) > 0 {
		if pf, ok := domain.PrimaryFile(out.Files); ok {
			out.FolderPath = domain.NormalizeRelativePath(pf.FolderPath)
		}
	}
	if out.StackKey == "" && out.MediaType != "" && out.BaseName != "" {
		var fmtHint domain.MediaFormat
		if pf, ok := domain.PrimaryFile(out.Files); ok {
			fmtHint = pf.Format
		}
		out.StackKey = domain.ComputeStackKey(out.FolderPath, out.MediaType, out.BaseName, fmtHint)
	}
	return out
}

func prepareFileForPhoto(photo domain.Photo, file domain.PhotoFile) domain.PhotoFile {
	out := file
	if out.PhotoID == "" {
		out.PhotoID = photo.ID
	}
	if out.Status == "" {
		out.Status = domain.FileStatusActive
	}
	if out.CreatedAt.IsZero() {
		out.CreatedAt = photo.CreatedAt
	}
	if out.UpdatedAt.IsZero() {
		out.UpdatedAt = photo.UpdatedAt
	}
	return out
}

func prepareAttachedFile(photoID string, file domain.PhotoFile) domain.PhotoFile {
	now := time.Now().UTC()
	out := file
	out.PhotoID = photoID
	if out.Status == "" {
		out.Status = domain.FileStatusActive
	}
	if out.CreatedAt.IsZero() {
		out.CreatedAt = now
	}
	if out.UpdatedAt.IsZero() {
		out.UpdatedAt = out.CreatedAt
	}
	return out
}

func insertPhotoTx(ctx context.Context, tx *sql.Tx, photo domain.Photo) error {
	e := photoToEntity(photo)
	_, err := tx.ExecContext(ctx, `
		INSERT INTO photos (
			id, stack_key, base_name, folder_path, media_type,
			width, height, orientation, duration_sec, taken_at_unix,
			camera_make, camera_model, lens_model, iso, f_number,
			exposure_time, focal_length_mm, gps_latitude, gps_longitude,
			view_count, last_viewed_unix_ns, status, created_at_unix, updated_at_unix
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		e.ID, e.StackKey, e.BaseName, e.FolderPath, e.MediaType,
		e.Width, e.Height, e.Orientation, e.DurationSec, e.TakenAtUnix,
		e.CameraMake, e.CameraModel, e.LensModel, e.ISO, e.FNumber,
		e.ExposureTime, e.FocalLengthMM, e.GPSLatitude, e.GPSLongitude,
		e.ViewCount, e.LastViewedUnixNs, e.Status, e.CreatedAtUnix, e.UpdatedAtUnix,
	)
	if err != nil {
		return fmt.Errorf("insert photo %q: %w", photo.ID, err)
	}
	return nil
}

func insertPhotoFileTx(ctx context.Context, tx *sql.Tx, file domain.PhotoFile) error {
	e := photoFileToEntity(file)
	_, err := tx.ExecContext(ctx, `
		INSERT INTO photo_files (
			id, photo_id, content_hash, relative_path, file_name,
			folder_path, format, file_size, mod_time_unix_ns,
			status, created_at_unix, updated_at_unix
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		e.ID, e.PhotoID, e.ContentHash, e.RelativePath, e.FileName,
		e.FolderPath, e.Format, e.FileSize, e.ModTimeUnixNs,
		e.Status, e.CreatedAtUnix, e.UpdatedAtUnix,
	)
	if err != nil {
		return fmt.Errorf("insert photo_file %q: %w", file.ID, err)
	}
	return nil
}

func insertPhotoTagsTx(ctx context.Context, tx *sql.Tx, photoID string, tags []string, now time.Time) error {
	for _, raw := range tags {
		emoji, err := domain.NormalizeEmojiTag(raw)
		if err != nil {
			return err
		}
		e := photoTagToEntity(photoID, emoji, now)
		if _, err := tx.ExecContext(
			ctx,
			`INSERT INTO photo_tags (photo_id, emoji, created_at_unix) VALUES (?, ?, ?) ON CONFLICT(photo_id, emoji) DO NOTHING`,
			e.PhotoID, e.Emoji, e.CreatedAtUnix,
		); err != nil {
			return fmt.Errorf("insert photo_tag %q: %w", e.Emoji, err)
		}
	}
	return nil
}

func updateFileInPlaceTx(
	ctx context.Context,
	tx *sql.Tx,
	file domain.PhotoFile,
	status domain.FileStatus,
	updatedAt time.Time,
) (string, error) {
	var whereCol string
	var whereArg string
	if file.ID != "" {
		whereCol = "id"
		whereArg = file.ID
	} else {
		whereCol = "relative_path"
		whereArg = domain.NormalizeRelativePath(file.RelativePath)
	}
	if whereArg == "" {
		return "", domain.ErrFileNotFound
	}

	res, err := tx.ExecContext(
		ctx,
		`UPDATE photo_files
		SET content_hash = ?, file_size = ?, mod_time_unix_ns = ?, status = ?, updated_at_unix = ?
		WHERE `+whereCol+` = ?`,
		file.ContentHash, file.FileSize, timeToUnixNs(file.ModTime),
		string(status), timeToUnix(updatedAt), whereArg,
	)
	if err != nil {
		return "", fmt.Errorf("update photo_file in place: %w", err)
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return "", fmt.Errorf("read update rows affected: %w", err)
	}
	if affected == 0 {
		return "", domain.ErrFileNotFound
	}

	var photoID string
	if err := tx.QueryRowContext(ctx, `SELECT photo_id FROM photo_files WHERE `+whereCol+` = ?`, whereArg).Scan(&photoID); err != nil {
		return "", fmt.Errorf("lookup photo_id after update: %w", err)
	}
	return photoID, nil
}

func normalizeFileMoves(moves []FileMove) ([]FileMove, error) {
	out := make([]FileMove, 0, len(moves))
	for _, m := range moves {
		relPath := domain.NormalizeRelativePath(m.RelativePath)
		if strings.TrimSpace(m.FileID) == "" || relPath == "" {
			return nil, fmt.Errorf("invalid file move (id=%q, relative_path=%q)", m.FileID, m.RelativePath)
		}
		fileName := domain.NormalizeNFC(m.FileName)
		if fileName == "" {
			fileName = path.Base(relPath)
		}
		folderPath := domain.NormalizeRelativePath(m.FolderPath)
		if strings.TrimSpace(m.FolderPath) == "" {
			if dir := path.Dir(relPath); dir != "." {
				folderPath = dir
			}
		}
		out = append(out, FileMove{
			FileID:       m.FileID,
			RelativePath: relPath,
			FileName:     fileName,
			FolderPath:   folderPath,
		})
	}
	return out, nil
}

func stageTempMovesTx(ctx context.Context, tx *sql.Tx, moves []FileMove, stampNs int64) ([]string, error) {
	seenPhotos := make(map[string]struct{}, len(moves))
	photoIDs := make([]string, 0, len(moves))
	for i, m := range moves {
		var photoID string
		err := tx.QueryRowContext(ctx, `SELECT photo_id FROM photo_files WHERE id = ?`, m.FileID).Scan(&photoID)
		if errors.Is(err, sql.ErrNoRows) {
			return nil, fmt.Errorf("%w: %s", domain.ErrFileNotFound, m.FileID)
		}
		if err != nil {
			return nil, fmt.Errorf("lookup moving file %q: %w", m.FileID, err)
		}
		tmpPath := fmt.Sprintf("__tmp_move_%d_%d_%s__", stampNs, i, m.FileID)
		if _, err := tx.ExecContext(ctx, `UPDATE photo_files SET relative_path = ? WHERE id = ?`, tmpPath, m.FileID); err != nil {
			return nil, fmt.Errorf("stage temp move for %q: %w", m.FileID, err)
		}
		if _, exists := seenPhotos[photoID]; !exists {
			seenPhotos[photoID] = struct{}{}
			photoIDs = append(photoIDs, photoID)
		}
	}
	return photoIDs, nil
}

func applyFinalMovesTx(ctx context.Context, tx *sql.Tx, moves []FileMove, nowUnix int64) error {
	for _, m := range moves {
		_, err := tx.ExecContext(
			ctx,
			`UPDATE photo_files
			SET relative_path = ?, file_name = ?, folder_path = ?, status = ?, updated_at_unix = ?
			WHERE id = ?`,
			m.RelativePath, m.FileName, m.FolderPath,
			string(domain.FileStatusActive), nowUnix, m.FileID,
		)
		if err != nil {
			return fmt.Errorf("apply final move for %q -> %q: %w", m.FileID, m.RelativePath, err)
		}
	}
	return nil
}

func refreshMovedPhotoParentsTx(ctx context.Context, tx *sql.Tx, photoIDs []string, nowUnix int64) error {
	for _, photoID := range photoIDs {
		if err := refreshSinglePhotoParentTx(ctx, tx, photoID, nowUnix); err != nil {
			return err
		}
	}
	return nil
}

type activeFileMeta struct {
	folderPath string
	fileName   string
	format     domain.MediaFormat
}

func refreshSinglePhotoParentTx(ctx context.Context, tx *sql.Tx, photoID string, nowUnix int64) error {
	var mediaType, curBaseName, curFolder, curStackKey string
	err := tx.QueryRowContext(
		ctx,
		`SELECT media_type, base_name, folder_path, stack_key FROM photos WHERE id = ?`,
		photoID,
	).Scan(&mediaType, &curBaseName, &curFolder, &curStackKey)
	if err != nil {
		return fmt.Errorf("load parent photo %q: %w", photoID, err)
	}

	activeFiles, err := loadActiveFileMetaTx(ctx, tx, photoID)
	if err != nil {
		return err
	}
	newFolder, newBaseName, newStackKey := computeMovedParentAttrs(
		domain.MediaType(mediaType), curBaseName, curFolder, curStackKey, activeFiles,
	)
	_, err = tx.ExecContext(
		ctx,
		`UPDATE photos SET status = ?, folder_path = ?, base_name = ?, stack_key = ?, updated_at_unix = ? WHERE id = ?`,
		string(domain.FileStatusActive), newFolder, newBaseName, newStackKey, nowUnix, photoID,
	)
	if err != nil {
		return fmt.Errorf("update parent photo %q after move: %w", photoID, err)
	}
	return nil
}

func loadActiveFileMetaTx(ctx context.Context, tx *sql.Tx, photoID string) ([]activeFileMeta, error) {
	rows, err := tx.QueryContext(
		ctx,
		`SELECT folder_path, file_name, format
		FROM photo_files
		WHERE photo_id = ? AND status = ?
		ORDER BY relative_path ASC`,
		photoID, string(domain.FileStatusActive),
	)
	if err != nil {
		return nil, fmt.Errorf("query active files for photo %q: %w", photoID, err)
	}
	defer func() { _ = rows.Close() }()

	var out []activeFileMeta
	for rows.Next() {
		var m activeFileMeta
		var rawFmt string
		if err := rows.Scan(&m.folderPath, &m.fileName, &rawFmt); err != nil {
			return nil, fmt.Errorf("scan active file metadata: %w", err)
		}
		m.format = domain.MediaFormat(rawFmt)
		out = append(out, m)
	}
	return out, rows.Err()
}

func computeMovedParentAttrs(
	mediaType domain.MediaType,
	curBaseName, curFolder, curStackKey string,
	activeFiles []activeFileMeta,
) (string, string, string) {
	if len(activeFiles) == 0 {
		return curFolder, curBaseName, curStackKey
	}
	commonFolder := activeFiles[0].folderPath
	allSameFolder, curFolderActive := true, false
	best := activeFiles[0]
	for _, f := range activeFiles {
		if f.folderPath != commonFolder {
			allSameFolder = false
		}
		if f.folderPath == curFolder {
			curFolderActive = true
		}
		if domain.FormatPriority(f.format) > domain.FormatPriority(best.format) {
			best = f
		}
	}
	if !allSameFolder {
		if curFolderActive {
			return curFolder, curBaseName, curStackKey
		}
		return best.folderPath, curBaseName, domain.ComputeStackKey(best.folderPath, mediaType, curBaseName, best.format)
	}

	newBaseName, primaryFmt := resolveUnambiguousStemAndFormat(curBaseName, activeFiles)
	newStackKey := domain.ComputeStackKey(commonFolder, mediaType, newBaseName, primaryFmt)
	return commonFolder, newBaseName, newStackKey
}

func resolveUnambiguousStemAndFormat(curBaseName string, activeFiles []activeFileMeta) (string, domain.MediaFormat) {
	best := activeFiles[0]
	firstStem := fileBaseStem(best.fileName)
	unambiguous := firstStem != ""

	for _, f := range activeFiles {
		if domain.FormatPriority(f.format) > domain.FormatPriority(best.format) {
			best = f
		}
		if !strings.EqualFold(fileBaseStem(f.fileName), firstStem) {
			unambiguous = false
		}
	}
	if unambiguous {
		return fileBaseStem(best.fileName), best.format
	}
	return curBaseName, best.format
}

func fileBaseStem(fileName string) string {
	ext := path.Ext(fileName)
	return strings.TrimSuffix(fileName, ext)
}

func markFileChunksMissingTx(ctx context.Context, tx *sql.Tx, fileIDs []string, nowUnix int64) ([]string, error) {
	seenPhotos := make(map[string]struct{})
	var photoIDs []string

	for i := 0; i < len(fileIDs); i += sqlInChunkSize {
		chunk := fileIDs[i:min(i+sqlInChunkSize, len(fileIDs))]
		placeholders := sqlPlaceholders(len(chunk))
		args := append([]any{string(domain.FileStatusMissing), nowUnix}, stringArgs(chunk)...)
		if _, err := tx.ExecContext(
			ctx,
			`UPDATE photo_files SET status = ?, updated_at_unix = ? WHERE id IN (`+placeholders+`)`,
			args...,
		); err != nil {
			return nil, fmt.Errorf("mark photo_files missing: %w", err)
		}

		chunkPhotos, err := queryAffectedPhotoIDsTx(ctx, tx, placeholders, stringArgs(chunk))
		if err != nil {
			return nil, err
		}
		for _, pid := range chunkPhotos {
			if _, ok := seenPhotos[pid]; !ok {
				seenPhotos[pid] = struct{}{}
				photoIDs = append(photoIDs, pid)
			}
		}
	}
	return photoIDs, nil
}

func queryAffectedPhotoIDsTx(ctx context.Context, tx *sql.Tx, placeholders string, args []any) ([]string, error) {
	rows, err := tx.QueryContext(ctx, `SELECT DISTINCT photo_id FROM photo_files WHERE id IN (`+placeholders+`)`, args...)
	if err != nil {
		return nil, fmt.Errorf("query affected photo_ids: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var out []string
	for rows.Next() {
		var pid string
		if err := rows.Scan(&pid); err != nil {
			return nil, fmt.Errorf("scan affected photo_id: %w", err)
		}
		out = append(out, pid)
	}
	return out, rows.Err()
}

func recomputePhotoStatusChunksTx(ctx context.Context, tx *sql.Tx, photoIDs []string, nowUnix int64) error {
	for i := 0; i < len(photoIDs); i += sqlInChunkSize {
		chunk := photoIDs[i:min(i+sqlInChunkSize, len(photoIDs))]
		placeholders := sqlPlaceholders(len(chunk))
		args := append([]any{
			string(domain.FileStatusActive),
			string(domain.FileStatusActive),
			string(domain.FileStatusMissing),
			nowUnix,
		}, stringArgs(chunk)...)
		if _, err := tx.ExecContext(ctx, `
			UPDATE photos
			SET status = CASE
				WHEN EXISTS (SELECT 1 FROM photo_files pf WHERE pf.photo_id = photos.id AND pf.status = ?) THEN ?
				ELSE ?
			END,
			updated_at_unix = ?
			WHERE id IN (`+placeholders+`)`, args...); err != nil {
			return fmt.Errorf("recompute parent photo status: %w", err)
		}
	}
	return nil
}

func loadHydratedPhotoByID(ctx context.Context, queryRow rowQueryFunc, queryRows rowsQueryFunc, photoID string) (domain.Photo, error) {
	entity, err := scanSinglePhoto(queryRow(ctx, `SELECT `+photoSelectCols+` FROM photos p WHERE p.id = ?`, photoID))
	if err != nil {
		return domain.Photo{}, err
	}
	hydrated, err := hydratePhotos(ctx, queryRows, []photoEntity{entity})
	if err != nil {
		return domain.Photo{}, err
	}
	return hydrated[0], nil
}

func ensurePhotoExists(ctx context.Context, queryRow rowQueryFunc, photoID string) error {
	var exists int
	err := queryRow(ctx, `SELECT 1 FROM photos WHERE id = ?`, photoID).Scan(&exists)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.ErrPhotoNotFound
	}
	if err != nil {
		return fmt.Errorf("check photo existence %q: %w", photoID, err)
	}
	return nil
}

func scanSinglePhoto(row *sql.Row) (photoEntity, error) {
	e, err := scanPhotoRow(row)
	if errors.Is(err, sql.ErrNoRows) {
		return photoEntity{}, domain.ErrPhotoNotFound
	}
	if err != nil {
		return photoEntity{}, fmt.Errorf("scan photo row: %w", err)
	}
	return e, nil
}

type rowScanner interface {
	Scan(dest ...any) error
}

func scanPhotoRow(s rowScanner) (photoEntity, error) {
	var e photoEntity
	err := s.Scan(
		&e.ID, &e.StackKey, &e.BaseName, &e.FolderPath, &e.MediaType,
		&e.Width, &e.Height, &e.Orientation, &e.DurationSec, &e.TakenAtUnix,
		&e.CameraMake, &e.CameraModel, &e.LensModel, &e.ISO, &e.FNumber,
		&e.ExposureTime, &e.FocalLengthMM, &e.GPSLatitude, &e.GPSLongitude,
		&e.ViewCount, &e.LastViewedUnixNs, &e.Status, &e.CreatedAtUnix, &e.UpdatedAtUnix,
	)
	return e, err
}

func scanPhotoFileRow(s rowScanner) (photoFileEntity, error) {
	var e photoFileEntity
	err := s.Scan(
		&e.ID, &e.PhotoID, &e.ContentHash, &e.RelativePath, &e.FileName,
		&e.FolderPath, &e.Format, &e.FileSize, &e.ModTimeUnixNs,
		&e.Status, &e.CreatedAtUnix, &e.UpdatedAtUnix,
	)
	return e, err
}

func queryPhotoFiles(ctx context.Context, queryRows rowsQueryFunc, query string, args ...any) ([]domain.PhotoFile, error) {
	rows, err := queryRows(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("query photo_files: %w", err)
	}
	defer func() { _ = rows.Close() }()

	files := make([]domain.PhotoFile, 0)
	for rows.Next() {
		e, err := scanPhotoFileRow(rows)
		if err != nil {
			return nil, fmt.Errorf("scan photo_file: %w", err)
		}
		files = append(files, photoFileFromEntity(e))
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate photo_files: %w", err)
	}
	return files, nil
}

func hydratePhotos(ctx context.Context, queryRows rowsQueryFunc, entities []photoEntity) ([]domain.Photo, error) {
	if len(entities) == 0 {
		return []domain.Photo{}, nil
	}
	photoIDs := make([]string, 0, len(entities))
	for _, e := range entities {
		photoIDs = append(photoIDs, e.ID)
	}
	filesByPhoto, err := loadFilesByPhotoIDs(ctx, queryRows, photoIDs)
	if err != nil {
		return nil, err
	}
	tagsByPhoto, err := loadTagsByPhotoIDs(ctx, queryRows, photoIDs)
	if err != nil {
		return nil, err
	}
	out := make([]domain.Photo, 0, len(entities))
	for _, e := range entities {
		out = append(out, photoFromEntity(e, filesByPhoto[e.ID], tagsByPhoto[e.ID]))
	}
	return out, nil
}

func loadFilesByPhotoIDs(ctx context.Context, queryRows rowsQueryFunc, photoIDs []string) (map[string][]domain.PhotoFile, error) {
	grouped := make(map[string][]domain.PhotoFile, len(photoIDs))
	for i := 0; i < len(photoIDs); i += sqlInChunkSize {
		chunk := photoIDs[i:min(i+sqlInChunkSize, len(photoIDs))]
		query := `SELECT ` + photoFileSelectCols + ` FROM photo_files WHERE photo_id IN (` + sqlPlaceholders(len(chunk)) + `) ORDER BY relative_path ASC`
		files, err := queryPhotoFiles(ctx, queryRows, query, stringArgs(chunk)...)
		if err != nil {
			return nil, err
		}
		for _, f := range files {
			grouped[f.PhotoID] = append(grouped[f.PhotoID], f)
		}
	}
	return grouped, nil
}

func loadTagsByPhotoIDs(ctx context.Context, queryRows rowsQueryFunc, photoIDs []string) (map[string][]string, error) {
	grouped := make(map[string][]string, len(photoIDs))
	for i := 0; i < len(photoIDs); i += sqlInChunkSize {
		chunk := photoIDs[i:min(i+sqlInChunkSize, len(photoIDs))]
		query := `SELECT photo_id, emoji, created_at_unix FROM photo_tags WHERE photo_id IN (` + sqlPlaceholders(len(chunk)) + `) ORDER BY created_at_unix ASC, emoji ASC`
		rows, err := queryRows(ctx, query, stringArgs(chunk)...)
		if err != nil {
			return nil, fmt.Errorf("query photo_tags: %w", err)
		}
		if err := scanTagMapRows(rows, grouped); err != nil {
			return nil, err
		}
	}
	return grouped, nil
}

func scanTagMapRows(rows *sql.Rows, grouped map[string][]string) error {
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var e photoTagEntity
		if err := rows.Scan(&e.PhotoID, &e.Emoji, &e.CreatedAtUnix); err != nil {
			return fmt.Errorf("scan photo_tag row: %w", err)
		}
		grouped[e.PhotoID] = append(grouped[e.PhotoID], e.Emoji)
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("iterate photo_tags: %w", err)
	}
	return nil
}

func sqlPlaceholders(n int) string {
	if n <= 0 {
		return ""
	}
	return strings.TrimSuffix(strings.Repeat("?,", n), ",")
}

func stringArgs(items []string) []any {
	args := make([]any, 0, len(items))
	for _, s := range items {
		args = append(args, s)
	}
	return args
}

func uniqueNonEmptyStrings(items []string) []string {
	seen := make(map[string]struct{}, len(items))
	out := make([]string, 0, len(items))
	for _, item := range items {
		if item == "" {
			continue
		}
		if _, exists := seen[item]; exists {
			continue
		}
		seen[item] = struct{}{}
		out = append(out, item)
	}
	return out
}

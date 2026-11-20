package sqlite

import (
	"cmp"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"slices"

	"github.com/lucastheo/photos/internal/domain"
)

// ListDuplicates returns all SHA-256 content hashes shared by more than one
// active physical file on disk.
func (r *PhotoRepository) ListDuplicates(ctx context.Context) ([]domain.DuplicateGroup, error) {
	query := `SELECT ` + photoFileSelectCols + `
		FROM photo_files
		WHERE status = ?
		  AND content_hash IN (
			SELECT content_hash
			FROM photo_files
			WHERE status = ?
			GROUP BY content_hash
			HAVING COUNT(*) > 1
		  )
		ORDER BY content_hash ASC, relative_path ASC`

	active := string(domain.FileStatusActive)
	files, err := queryPhotoFiles(ctx, r.db.QueryContext, query, active, active)
	if err != nil {
		return nil, err
	}
	return groupDuplicateFiles(files), nil
}

func groupDuplicateFiles(files []domain.PhotoFile) []domain.DuplicateGroup {
	if len(files) == 0 {
		return []domain.DuplicateGroup{}
	}
	byHash := make(map[string][]domain.PhotoFile)
	for _, f := range files {
		byHash[f.ContentHash] = append(byHash[f.ContentHash], f)
	}

	groups := make([]domain.DuplicateGroup, 0, len(byHash))
	for hash, groupFiles := range byHash {
		sorted := domain.SortPhotoFilesByPriority(groupFiles)
		groups = append(groups, domain.DuplicateGroup{
			ContentHash: hash,
			PhotoID:     sorted[0].PhotoID,
			FileCount:   len(sorted),
			Files:       sorted,
		})
	}
	slices.SortFunc(groups, func(a, b domain.DuplicateGroup) int {
		if r := cmp.Compare(b.FileCount, a.FileCount); r != 0 {
			return r
		}
		return cmp.Compare(a.ContentHash, b.ContentHash)
	})
	return groups
}

// ListMissing returns all logical Photo records that are marked missing or
// still own at least one missing PhotoFile, bulk-hydrated with files and tags.
func (r *PhotoRepository) ListMissing(ctx context.Context) ([]domain.Photo, error) {
	tx, err := r.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return nil, fmt.Errorf("begin list missing tx: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	missing := string(domain.FileStatusMissing)
	query := `SELECT ` + photoSelectCols + `
		FROM photos p
		WHERE p.status = ?
		   OR EXISTS (SELECT 1 FROM photo_files pf WHERE pf.photo_id = p.id AND pf.status = ?)
		ORDER BY p.folder_path ASC, p.base_name ASC, p.id ASC`

	rows, err := tx.QueryContext(ctx, query, missing, missing)
	if err != nil {
		return nil, fmt.Errorf("query missing photos: %w", err)
	}
	entities, err := scanPhotoEntities(rows)
	if err != nil {
		return nil, err
	}
	return hydratePhotos(ctx, tx.QueryContext, entities)
}

// PurgeMissing transactionally deletes all missing PhotoFile rows and any
// orphaned missing Photo rows, returning the total number of deleted rows.
func (r *PhotoRepository) PurgeMissing(ctx context.Context) (int64, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, fmt.Errorf("begin purge missing tx: %w", err)
	}

	missing := string(domain.FileStatusMissing)
	resFiles, err := tx.ExecContext(ctx, `DELETE FROM photo_files WHERE status = ?`, missing)
	if err != nil {
		return 0, errors.Join(fmt.Errorf("delete missing photo_files: %w", err), tx.Rollback())
	}
	filesDeleted, err := resFiles.RowsAffected()
	if err != nil {
		return 0, errors.Join(fmt.Errorf("count deleted photo_files: %w", err), tx.Rollback())
	}

	resPhotos, err := tx.ExecContext(ctx, `
		DELETE FROM photos
		WHERE status = ?
		  AND NOT EXISTS (SELECT 1 FROM photo_files pf WHERE pf.photo_id = photos.id)`,
		missing,
	)
	if err != nil {
		return 0, errors.Join(fmt.Errorf("delete orphaned missing photos: %w", err), tx.Rollback())
	}
	photosDeleted, err := resPhotos.RowsAffected()
	if err != nil {
		return 0, errors.Join(fmt.Errorf("count deleted photos: %w", err), tx.Rollback())
	}

	if err := tx.Commit(); err != nil {
		return 0, fmt.Errorf("commit purge missing tx: %w", err)
	}
	return filesDeleted + photosDeleted, nil
}

// GetLibraryStats computes aggregate counts, duplicate totals, missing counts,
// and breakdowns by format and folder for the indexed media library.
func (r *PhotoRepository) GetLibraryStats(ctx context.Context) (domain.LibraryStats, error) {
	tx, err := r.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return domain.LibraryStats{}, fmt.Errorf("begin library stats tx: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	var stats domain.LibraryStats
	if err := loadPhotoViewAggregates(ctx, tx.QueryRowContext, &stats); err != nil {
		return domain.LibraryStats{}, err
	}
	if err := loadFileAndDuplicateAggregates(ctx, tx.QueryRowContext, &stats); err != nil {
		return domain.LibraryStats{}, err
	}
	byFormat, err := loadFormatStats(ctx, tx.QueryContext)
	if err != nil {
		return domain.LibraryStats{}, err
	}
	byFolder, err := loadFolderStats(ctx, tx.QueryContext)
	if err != nil {
		return domain.LibraryStats{}, err
	}
	stats.ByFormat = byFormat
	stats.ByFolder = byFolder
	return stats, nil
}

func loadPhotoViewAggregates(ctx context.Context, queryRow rowQueryFunc, stats *domain.LibraryStats) error {
	active := string(domain.FileStatusActive)
	missing := string(domain.FileStatusMissing)
	err := queryRow(ctx, `
		SELECT
			COALESCE(SUM(CASE WHEN status = ? THEN 1 ELSE 0 END), 0),
			COALESCE(SUM(CASE WHEN status = ? AND view_count = 0 THEN 1 ELSE 0 END), 0),
			COALESCE(SUM(CASE WHEN status = ? AND view_count >= 1 THEN 1 ELSE 0 END), 0),
			COALESCE(SUM(CASE WHEN status = ? THEN view_count ELSE 0 END), 0),
			COALESCE(SUM(CASE WHEN status = ? THEN 1 ELSE 0 END), 0)
		FROM photos`,
		active, active, active, active, missing,
	).Scan(
		&stats.TotalPhotos,
		&stats.UnseenPhotos,
		&stats.SeenPhotos,
		&stats.TotalViews,
		&stats.MissingPhotos,
	)
	if err != nil {
		return fmt.Errorf("query photo library stats: %w", err)
	}
	return nil
}

func loadFileAndDuplicateAggregates(ctx context.Context, queryRow rowQueryFunc, stats *domain.LibraryStats) error {
	active := string(domain.FileStatusActive)
	missing := string(domain.FileStatusMissing)
	err := queryRow(ctx, `
		SELECT
			COALESCE(SUM(CASE WHEN status = ? THEN 1 ELSE 0 END), 0),
			COALESCE(SUM(CASE WHEN status = ? THEN 1 ELSE 0 END), 0)
		FROM photo_files`,
		active, missing,
	).Scan(&stats.TotalFiles, &stats.MissingFiles)
	if err != nil {
		return fmt.Errorf("query file counts: %w", err)
	}

	err = queryRow(ctx, `
		SELECT COUNT(*), COALESCE(SUM(cnt), 0)
		FROM (
			SELECT COUNT(*) AS cnt
			FROM photo_files
			WHERE status = ?
			GROUP BY content_hash
			HAVING COUNT(*) > 1
		)`,
		active,
	).Scan(&stats.DuplicateGroups, &stats.DuplicateFiles)
	if err != nil {
		return fmt.Errorf("query duplicate aggregates: %w", err)
	}
	return nil
}

func loadFormatStats(ctx context.Context, queryRows rowsQueryFunc) ([]domain.FormatStat, error) {
	rows, err := queryRows(ctx, `
		SELECT format, COUNT(*) AS cnt
		FROM photo_files
		WHERE status = ?
		GROUP BY format
		ORDER BY cnt DESC, format ASC`,
		string(domain.FileStatusActive),
	)
	if err != nil {
		return nil, fmt.Errorf("query format stats: %w", err)
	}
	defer func() { _ = rows.Close() }()

	out := make([]domain.FormatStat, 0)
	for rows.Next() {
		var rawFmt string
		var cnt int64
		if err := rows.Scan(&rawFmt, &cnt); err != nil {
			return nil, fmt.Errorf("scan format stat: %w", err)
		}
		out = append(out, domain.FormatStat{
			Format: domain.MediaFormat(rawFmt),
			Count:  cnt,
		})
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate format stats: %w", err)
	}
	return out, nil
}

func loadFolderStats(ctx context.Context, queryRows rowsQueryFunc) ([]domain.FolderStat, error) {
	rows, err := queryRows(ctx, `
		SELECT folder_path, COUNT(*) AS cnt
		FROM photos
		WHERE status = ?
		GROUP BY folder_path
		ORDER BY cnt DESC, folder_path ASC`,
		string(domain.FileStatusActive),
	)
	if err != nil {
		return nil, fmt.Errorf("query folder stats: %w", err)
	}
	defer func() { _ = rows.Close() }()

	out := make([]domain.FolderStat, 0)
	for rows.Next() {
		var item domain.FolderStat
		if err := rows.Scan(&item.FolderPath, &item.Count); err != nil {
			return nil, fmt.Errorf("scan folder stat: %w", err)
		}
		out = append(out, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate folder stats: %w", err)
	}
	return out, nil
}

package sqlite

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	"github.com/lucastheo/photos/internal/domain"
)

var likeEscaper = strings.NewReplacer(
	`\`, `\\`,
	`%`, `\%`,
	`_`, `\_`,
)

// Search filters, sorts, paginates, and hydrates matching logical Photo items.
func (r *PhotoRepository) Search(ctx context.Context, filter domain.PhotoFilter) (domain.PaginatedPhotos, error) {
	norm, err := filter.NormalizeAndValidate()
	if err != nil {
		return domain.PaginatedPhotos{}, err
	}

	tx, err := r.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return domain.PaginatedPhotos{}, fmt.Errorf("begin search photos tx: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	whereClause, whereArgs := buildSearchWhere(norm)
	var total int64
	countSQL := `SELECT COUNT(*) FROM photos p WHERE ` + whereClause
	if err := tx.QueryRowContext(ctx, countSQL, whereArgs...).Scan(&total); err != nil {
		return domain.PaginatedPhotos{}, fmt.Errorf("count search photos: %w", err)
	}
	if total == 0 || int64(norm.Page-1) > (total-1)/int64(norm.PageSize) {
		return domain.PaginatedPhotos{
			Items:    []domain.Photo{},
			Total:    total,
			Page:     norm.Page,
			PageSize: norm.PageSize,
		}, nil
	}

	items, err := fetchSearchPage(ctx, tx.QueryContext, norm, whereClause, whereArgs)
	if err != nil {
		return domain.PaginatedPhotos{}, err
	}
	return domain.PaginatedPhotos{
		Items:    items,
		Total:    total,
		Page:     norm.Page,
		PageSize: norm.PageSize,
	}, nil
}

func fetchSearchPage(
	ctx context.Context,
	queryRows rowsQueryFunc,
	norm domain.PhotoFilter,
	whereClause string,
	whereArgs []any,
) ([]domain.Photo, error) {
	orderClause, orderArgs := buildSearchOrder(norm)
	offset := int64(norm.Page-1) * int64(norm.PageSize)

	query := `SELECT ` + photoSelectCols + ` FROM photos p WHERE ` + whereClause +
		` ORDER BY ` + orderClause + ` LIMIT ? OFFSET ?`
	args := make([]any, 0, len(whereArgs)+len(orderArgs)+2)
	args = append(args, whereArgs...)
	args = append(args, orderArgs...)
	args = append(args, norm.PageSize, offset)

	rows, err := queryRows(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("query search photos: %w", err)
	}
	entities, err := scanPhotoEntities(rows)
	if err != nil {
		return nil, err
	}
	return hydratePhotos(ctx, queryRows, entities)
}

func scanPhotoEntities(rows *sql.Rows) ([]photoEntity, error) {
	defer func() { _ = rows.Close() }()
	entities := make([]photoEntity, 0)
	for rows.Next() {
		e, err := scanPhotoRow(rows)
		if err != nil {
			return nil, fmt.Errorf("scan search photo: %w", err)
		}
		entities = append(entities, e)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate search photos: %w", err)
	}
	return entities, nil
}

func buildSearchWhere(f domain.PhotoFilter) (string, []any) {
	clauses := []string{`p.status = ?`}
	args := []any{string(f.Status)}

	appendViewClauses(&clauses, &args, f)
	appendTagAndMediaClauses(&clauses, &args, f)
	appendLocationAndDateClauses(&clauses, &args, f)
	appendQueryClause(&clauses, &args, f)

	return strings.Join(clauses, " AND "), args
}

func appendViewClauses(clauses *[]string, args *[]any, f domain.PhotoFilter) {
	if f.ExactViews != nil {
		*clauses = append(*clauses, `p.view_count = ?`)
		*args = append(*args, *f.ExactViews)
	}
	if f.MinViews != nil {
		*clauses = append(*clauses, `p.view_count >= ?`)
		*args = append(*args, *f.MinViews)
	}
	if f.MaxViews != nil {
		*clauses = append(*clauses, `p.view_count <= ?`)
		*args = append(*args, *f.MaxViews)
	}
}

func appendTagAndMediaClauses(clauses *[]string, args *[]any, f domain.PhotoFilter) {
	if f.Untagged {
		*clauses = append(*clauses, `NOT EXISTS (SELECT 1 FROM photo_tags pt WHERE pt.photo_id = p.id)`)
	}
	for _, tag := range f.EmojiTags {
		*clauses = append(*clauses, `EXISTS (SELECT 1 FROM photo_tags pt WHERE pt.photo_id = p.id AND pt.emoji = ?)`)
		*args = append(*args, tag)
	}
	if f.MediaType != "" {
		*clauses = append(*clauses, `p.media_type = ?`)
		*args = append(*args, string(f.MediaType))
	}
	if len(f.Formats) > 0 {
		placeholders := sqlPlaceholders(len(f.Formats))
		*clauses = append(
			*clauses,
			`EXISTS (SELECT 1 FROM photo_files pf WHERE pf.photo_id = p.id AND pf.status = p.status AND pf.format IN (`+placeholders+`))`,
		)
		for _, fmtVal := range f.Formats {
			*args = append(*args, string(fmtVal))
		}
	}
}

func appendLocationAndDateClauses(clauses *[]string, args *[]any, f domain.PhotoFilter) {
	if f.Folder != "" {
		*clauses = append(*clauses, `(p.folder_path = ? OR (p.folder_path >= ? AND p.folder_path < ?))`)
		*args = append(*args, f.Folder, f.Folder+"/", f.Folder+"0")
	}
	if f.HasGPS != nil {
		if *f.HasGPS {
			*clauses = append(*clauses, `p.gps_latitude IS NOT NULL AND p.gps_longitude IS NOT NULL`)
		} else {
			*clauses = append(*clauses, `(p.gps_latitude IS NULL OR p.gps_longitude IS NULL)`)
		}
	}
	appendGPSBoundClauses(clauses, args, f)
	if f.FromDate != nil {
		*clauses = append(*clauses, `p.taken_at_unix >= ?`)
		*args = append(*args, f.FromDate.UTC().Unix())
	}
	if f.ToDate != nil {
		*clauses = append(*clauses, `p.taken_at_unix <= ?`)
		*args = append(*args, f.ToDate.UTC().Unix())
	}
}

func appendGPSBoundClauses(clauses *[]string, args *[]any, f domain.PhotoFilter) {
	if f.MinLat != nil {
		*clauses = append(*clauses, `p.gps_latitude >= ?`)
		*args = append(*args, *f.MinLat)
	}
	if f.MaxLat != nil {
		*clauses = append(*clauses, `p.gps_latitude <= ?`)
		*args = append(*args, *f.MaxLat)
	}
	if f.MinLng != nil {
		*clauses = append(*clauses, `p.gps_longitude >= ?`)
		*args = append(*args, *f.MinLng)
	}
	if f.MaxLng != nil {
		*clauses = append(*clauses, `p.gps_longitude <= ?`)
		*args = append(*args, *f.MaxLng)
	}
}

func appendQueryClause(clauses *[]string, args *[]any, f domain.PhotoFilter) {
	if f.Query == "" {
		return
	}
	pattern := `%` + likeEscaper.Replace(f.Query) + `%`
	*clauses = append(
		*clauses,
		`(p.base_name LIKE ? ESCAPE '\' OR p.folder_path LIKE ? ESCAPE '\' OR EXISTS (SELECT 1 FROM photo_files pf WHERE pf.photo_id = p.id AND pf.relative_path LIKE ? ESCAPE '\'))`,
	)
	*args = append(*args, pattern, pattern, pattern)
}

func buildSearchOrder(f domain.PhotoFilter) (string, []any) {
	dir := "ASC"
	if f.SortOrder == domain.SortOrderDesc {
		dir = "DESC"
	}

	switch f.SortBy {
	case domain.SortByDateTaken:
		return `p.taken_at_unix ` + dir + `, p.id ASC`, nil
	case domain.SortByLastViewed:
		return `p.last_viewed_unix_ns ` + dir + `, p.id ASC`, nil
	case domain.SortByRandom:
		mult, offset := seededRandomOrderParams(f.Seed)
		return `((p.rowid * ? + ?) & 2147483647) ` + dir + `, p.id ASC`, []any{mult, offset}
	default:
		return `p.view_count ` + dir + `, p.taken_at_unix DESC, p.id ASC`, nil
	}
}

func seededRandomOrderParams(seed int64) (int64, int64) {
	z := uint64(seed) + 0x9e3779b97f4a7c15
	z = (z ^ (z >> 30)) * 0xbf58476d1ce4e5b9
	z = (z ^ (z >> 27)) * 0x94d049bb133111eb
	z ^= z >> 31
	mult := int64((z & 0x7fffffff) | 1)
	offset := int64((z >> 32) & 0x7fffffff)
	return mult, offset
}

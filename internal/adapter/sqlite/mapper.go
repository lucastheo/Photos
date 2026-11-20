package sqlite

import (
	"database/sql"
	"slices"
	"time"

	"github.com/lucastheo/photos/internal/domain"
)

func photoToEntity(p domain.Photo) photoEntity {
	orientation := p.EXIF.Orientation
	if orientation <= 0 {
		orientation = 1
	}
	status := p.Status
	if status == "" {
		status = domain.FileStatusActive
	}
	return photoEntity{
		ID:               p.ID,
		StackKey:         p.StackKey,
		BaseName:         domain.NormalizeNFC(p.BaseName),
		FolderPath:       domain.NormalizeRelativePath(p.FolderPath),
		MediaType:        string(p.MediaType),
		Width:            p.Width,
		Height:           p.Height,
		Orientation:      orientation,
		DurationSec:      p.DurationSec,
		TakenAtUnix:      ptrTimeToNullUnix(p.TakenAt),
		CameraMake:       nullString(p.EXIF.CameraMake),
		CameraModel:      nullString(p.EXIF.CameraModel),
		LensModel:        nullString(p.EXIF.LensModel),
		ISO:              nullIntPtr(p.EXIF.ISO),
		FNumber:          nullFloatPtr(p.EXIF.FNumber),
		ExposureTime:     nullString(p.EXIF.ExposureTime),
		FocalLengthMM:    nullFloatPtr(p.EXIF.FocalLengthMM),
		GPSLatitude:      nullFloatPtr(p.EXIF.GPSLatitude),
		GPSLongitude:     nullFloatPtr(p.EXIF.GPSLongitude),
		ViewCount:        p.ViewCount,
		LastViewedUnixNs: ptrTimeToNullUnixNs(p.LastViewedAt),
		Status:           string(status),
		CreatedAtUnix:    timeToUnix(p.CreatedAt),
		UpdatedAtUnix:    timeToUnix(p.UpdatedAt),
	}
}

func photoFromEntity(e photoEntity, files []domain.PhotoFile, tags []string) domain.Photo {
	sortedFiles := domain.SortPhotoFilesByPriority(files)
	formats := domain.UniqueSortedFormats(sortedFiles)
	return domain.Photo{
		ID:          e.ID,
		StackKey:    e.StackKey,
		BaseName:    e.BaseName,
		FolderPath:  e.FolderPath,
		MediaType:   domain.MediaType(e.MediaType),
		Formats:     formats,
		Files:       sortedFiles,
		Width:       e.Width,
		Height:      e.Height,
		DurationSec: e.DurationSec,
		TakenAt:     nullUnixToPtrTime(e.TakenAtUnix),
		EXIF: domain.EXIFMetadata{
			Orientation:   e.Orientation,
			CameraMake:    e.CameraMake.String,
			CameraModel:   e.CameraModel.String,
			LensModel:     e.LensModel.String,
			ISO:           nullIntPtrFromEntity(e.ISO),
			FNumber:       nullFloatPtrFromEntity(e.FNumber),
			ExposureTime:  e.ExposureTime.String,
			FocalLengthMM: nullFloatPtrFromEntity(e.FocalLengthMM),
			GPSLatitude:   nullFloatPtrFromEntity(e.GPSLatitude),
			GPSLongitude:  nullFloatPtrFromEntity(e.GPSLongitude),
		},
		ViewCount:    e.ViewCount,
		EmojiTags:    slices.Clone(tags),
		LastViewedAt: nullUnixNsToPtrTime(e.LastViewedUnixNs),
		Status:       domain.FileStatus(e.Status),
		CreatedAt:    unixToTime(e.CreatedAtUnix),
		UpdatedAt:    unixToTime(e.UpdatedAtUnix),
	}
}

func photoFileToEntity(f domain.PhotoFile) photoFileEntity {
	status := f.Status
	if status == "" {
		status = domain.FileStatusActive
	}
	return photoFileEntity{
		ID:            f.ID,
		PhotoID:       f.PhotoID,
		ContentHash:   f.ContentHash,
		RelativePath:  domain.NormalizeRelativePath(f.RelativePath),
		FileName:      domain.NormalizeNFC(f.FileName),
		FolderPath:    domain.NormalizeRelativePath(f.FolderPath),
		Format:        string(f.Format),
		FileSize:      f.FileSize,
		ModTimeUnixNs: timeToUnixNs(f.ModTime),
		Status:        string(status),
		CreatedAtUnix: timeToUnix(f.CreatedAt),
		UpdatedAtUnix: timeToUnix(f.UpdatedAt),
	}
}

func photoFileFromEntity(e photoFileEntity) domain.PhotoFile {
	return domain.PhotoFile{
		ID:           e.ID,
		PhotoID:      e.PhotoID,
		ContentHash:  e.ContentHash,
		RelativePath: e.RelativePath,
		FileName:     e.FileName,
		FolderPath:   e.FolderPath,
		Format:       domain.MediaFormat(e.Format),
		FileSize:     e.FileSize,
		ModTime:      unixNsToTime(e.ModTimeUnixNs),
		Status:       domain.FileStatus(e.Status),
		CreatedAt:    unixToTime(e.CreatedAtUnix),
		UpdatedAt:    unixToTime(e.UpdatedAtUnix),
	}
}

func viewEventToEntity(v domain.ViewEvent) photoViewEntity {
	return photoViewEntity{
		ID:             v.ID,
		ViewID:         nullString(v.ViewID),
		PhotoID:        v.PhotoID,
		ViewedAtUnixNs: timeToUnixNs(v.ViewedAt),
		Source:         string(v.Source),
	}
}

func viewEventFromEntity(e photoViewEntity) domain.ViewEvent {
	return domain.ViewEvent{
		ID:       e.ID,
		ViewID:   e.ViewID.String,
		PhotoID:  e.PhotoID,
		ViewedAt: unixNsToTime(e.ViewedAtUnixNs),
		Source:   domain.ViewSource(e.Source),
	}
}

func photoTagToEntity(photoID, emoji string, createdAt time.Time) photoTagEntity {
	return photoTagEntity{
		PhotoID:       photoID,
		Emoji:         emoji,
		CreatedAtUnix: timeToUnix(createdAt),
	}
}

func syncReportToEntity(r domain.SyncReport) syncRunEntity {
	return syncRunEntity{
		ID:             r.ID,
		StartedAtUnix:  timeToUnix(r.StartedAt),
		FinishedAtUnix: ptrTimeToNullUnix(r.FinishedAt),
		Status:         string(r.Status),
		ScannedCount:   r.ScannedCount,
		AddedCount:     r.AddedCount,
		UpdatedCount:   r.UpdatedCount,
		MovedCount:     r.MovedCount,
		StackedCount:   r.StackedCount,
		DuplicateCount: r.DuplicateCount,
		UnchangedCount: r.UnchangedCount,
		MissingCount:   r.MissingCount,
		ErrorMessage:   nullString(r.ErrorMessage),
	}
}

func syncReportFromEntity(e syncRunEntity) domain.SyncReport {
	return domain.SyncReport{
		ID:             e.ID,
		StartedAt:      unixToTime(e.StartedAtUnix),
		FinishedAt:     nullUnixToPtrTime(e.FinishedAtUnix),
		Status:         domain.SyncStatus(e.Status),
		ScannedCount:   e.ScannedCount,
		AddedCount:     e.AddedCount,
		UpdatedCount:   e.UpdatedCount,
		MovedCount:     e.MovedCount,
		StackedCount:   e.StackedCount,
		DuplicateCount: e.DuplicateCount,
		UnchangedCount: e.UnchangedCount,
		MissingCount:   e.MissingCount,
		ErrorMessage:   e.ErrorMessage.String,
	}
}

func nullString(s string) sql.NullString {
	if s == "" {
		return sql.NullString{}
	}
	return sql.NullString{String: s, Valid: true}
}

func nullIntPtr(v *int) sql.NullInt64 {
	if v == nil {
		return sql.NullInt64{}
	}
	return sql.NullInt64{Int64: int64(*v), Valid: true}
}

func nullIntPtrFromEntity(v sql.NullInt64) *int {
	if !v.Valid {
		return nil
	}
	out := int(v.Int64)
	return &out
}

func nullFloatPtr(v *float64) sql.NullFloat64 {
	if v == nil {
		return sql.NullFloat64{}
	}
	return sql.NullFloat64{Float64: *v, Valid: true}
}

func nullFloatPtrFromEntity(v sql.NullFloat64) *float64 {
	if !v.Valid {
		return nil
	}
	out := v.Float64
	return &out
}

func timeToUnix(t time.Time) int64 {
	if t.IsZero() {
		return 0
	}
	return t.UTC().Unix()
}

func unixToTime(sec int64) time.Time {
	if sec == 0 {
		return time.Time{}
	}
	return time.Unix(sec, 0).UTC()
}

func timeToUnixNs(t time.Time) int64 {
	if t.IsZero() {
		return 0
	}
	return t.UTC().UnixNano()
}

func unixNsToTime(ns int64) time.Time {
	if ns == 0 {
		return time.Time{}
	}
	return time.Unix(0, ns).UTC()
}

func ptrTimeToNullUnix(t *time.Time) sql.NullInt64 {
	if t == nil {
		return sql.NullInt64{}
	}
	return sql.NullInt64{Int64: t.UTC().Unix(), Valid: true}
}

func nullUnixToPtrTime(v sql.NullInt64) *time.Time {
	if !v.Valid {
		return nil
	}
	t := time.Unix(v.Int64, 0).UTC()
	return &t
}

func ptrTimeToNullUnixNs(t *time.Time) sql.NullInt64 {
	if t == nil {
		return sql.NullInt64{}
	}
	return sql.NullInt64{Int64: t.UTC().UnixNano(), Valid: true}
}

func nullUnixNsToPtrTime(v sql.NullInt64) *time.Time {
	if !v.Valid {
		return nil
	}
	t := time.Unix(0, v.Int64).UTC()
	return &t
}

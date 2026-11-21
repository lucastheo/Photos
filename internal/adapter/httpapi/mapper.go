package httpapi

import (
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/lucastheo/photos/internal/domain"
)

func toDomainPhotoFilter(req ListPhotosRequest) (domain.PhotoFilter, error) {
	fromDate, err := parseFilterDate(req.FromDate, false, "from_date")
	if err != nil {
		return domain.PhotoFilter{}, err
	}
	toDate, err := parseFilterDate(req.ToDate, true, "to_date")
	if err != nil {
		return domain.PhotoFilter{}, err
	}

	formats := make([]domain.MediaFormat, 0, len(req.Formats))
	for _, f := range req.Formats {
		formats = append(formats, domain.MediaFormat(f))
	}

	filter := domain.PhotoFilter{
		MinViews:   req.MinViews,
		MaxViews:   req.MaxViews,
		ExactViews: req.ExactViews,
		EmojiTags:  req.EmojiTags,
		Untagged:   req.Untagged,
		Formats:    formats,
		MediaType:  domain.MediaType(strings.ToLower(strings.TrimSpace(req.MediaType))),
		Folder:     req.Folder,
		HasGPS:     req.HasGPS,
		MinLat:     req.MinLat,
		MaxLat:     req.MaxLat,
		MinLng:     req.MinLng,
		MaxLng:     req.MaxLng,
		FromDate:   fromDate,
		ToDate:     toDate,
		Query:      req.Query,
		SortBy:     domain.SortField(strings.ToLower(strings.TrimSpace(req.SortBy))),
		SortOrder:  domain.SortOrder(strings.ToLower(strings.TrimSpace(req.SortOrder))),
		Seed:       req.Seed,
		Page:       req.Page,
		PageSize:   req.PageSize,
	}
	return filter.NormalizeAndValidate()
}

func parsePhotoFilterFromQuery(q url.Values) (domain.PhotoFilter, error) {
	var req ListPhotosRequest
	if err := parseQueryViewAndBoolParams(q, &req); err != nil {
		return domain.PhotoFilter{}, err
	}
	if err := parseQueryGPSParams(q, &req); err != nil {
		return domain.PhotoFilter{}, err
	}
	if err := parseQueryPagingParams(q, &req); err != nil {
		return domain.PhotoFilter{}, err
	}
	req.EmojiTags = parseMultiOrCSV(q["emoji_tags"])
	req.Formats = parseMultiOrCSV(q["formats"])
	req.MediaType = q.Get("media_type")
	req.Folder = q.Get("folder")
	req.Query = q.Get("q")
	req.SortBy = q.Get("sort_by")
	req.SortOrder = q.Get("sort_order")
	if v := strings.TrimSpace(q.Get("from_date")); v != "" {
		req.FromDate = &v
	}
	if v := strings.TrimSpace(q.Get("to_date")); v != "" {
		req.ToDate = &v
	}

	filter, err := toDomainPhotoFilter(req)
	if err != nil {
		return domain.PhotoFilter{}, err
	}
	if status := strings.TrimSpace(q.Get("status")); status != "" {
		filter.Status = domain.FileStatus(strings.ToLower(status))
		return filter.NormalizeAndValidate()
	}
	return filter, nil
}

func parseQueryViewAndBoolParams(q url.Values, req *ListPhotosRequest) error {
	var err error
	if req.MinViews, err = parseOptionalInt(q, "min_views"); err != nil {
		return err
	}
	if req.MaxViews, err = parseOptionalInt(q, "max_views"); err != nil {
		return err
	}
	if req.ExactViews, err = parseOptionalInt(q, "exact_views"); err != nil {
		return err
	}
	if req.HasGPS, err = parseOptionalBool(q, "has_gps"); err != nil {
		return err
	}
	untagged, err := parseOptionalBool(q, "untagged")
	if err != nil {
		return err
	}
	if untagged != nil {
		req.Untagged = *untagged
	}
	return nil
}

func parseQueryGPSParams(q url.Values, req *ListPhotosRequest) error {
	var err error
	if req.MinLat, err = parseOptionalFloat(q, "min_lat"); err != nil {
		return err
	}
	if req.MaxLat, err = parseOptionalFloat(q, "max_lat"); err != nil {
		return err
	}
	if req.MinLng, err = parseOptionalFloat(q, "min_lng"); err != nil {
		return err
	}
	if req.MaxLng, err = parseOptionalFloat(q, "max_lng"); err != nil {
		return err
	}
	return nil
}

func parseQueryPagingParams(q url.Values, req *ListPhotosRequest) error {
	if raw := strings.TrimSpace(q.Get("seed")); raw != "" {
		v, err := strconv.ParseInt(raw, 10, 64)
		if err != nil {
			return fmt.Errorf("%w: invalid seed %q", domain.ErrInvalidFilter, raw)
		}
		req.Seed = v
	}
	if p, err := parseOptionalInt(q, "page"); err != nil {
		return err
	} else if p != nil {
		req.Page = *p
	}
	if ps, err := parseOptionalInt(q, "page_size"); err != nil {
		return err
	} else if ps != nil {
		req.PageSize = *ps
	}
	return nil
}

func parseOptionalInt(q url.Values, key string) (*int, error) {
	raw := strings.TrimSpace(q.Get(key))
	if raw == "" {
		return nil, nil
	}
	v, err := strconv.Atoi(raw)
	if err != nil {
		return nil, fmt.Errorf("%w: invalid %s %q", domain.ErrInvalidFilter, key, raw)
	}
	return &v, nil
}

func parseOptionalFloat(q url.Values, key string) (*float64, error) {
	raw := strings.TrimSpace(q.Get(key))
	if raw == "" {
		return nil, nil
	}
	v, err := strconv.ParseFloat(raw, 64)
	if err != nil {
		return nil, fmt.Errorf("%w: invalid %s %q", domain.ErrInvalidFilter, key, raw)
	}
	return &v, nil
}

func parseOptionalBool(q url.Values, key string) (*bool, error) {
	raw := strings.TrimSpace(q.Get(key))
	if raw == "" {
		return nil, nil
	}
	v, err := strconv.ParseBool(raw)
	if err != nil {
		return nil, fmt.Errorf("%w: invalid %s %q", domain.ErrInvalidFilter, key, raw)
	}
	return &v, nil
}

func parseMultiOrCSV(values []string) []string {
	var out []string
	for _, entry := range values {
		for _, part := range strings.Split(entry, ",") {
			trimmed := strings.TrimSpace(part)
			if trimmed != "" {
				out = append(out, trimmed)
			}
		}
	}
	return out
}

func parseFilterDate(raw *string, endOfDay bool, field string) (*time.Time, error) {
	if raw == nil || strings.TrimSpace(*raw) == "" {
		return nil, nil
	}
	s := strings.TrimSpace(*raw)
	if t, err := time.Parse(time.RFC3339Nano, s); err == nil {
		utc := t.UTC()
		return &utc, nil
	}
	if t, err := time.Parse("2006-01-02", s); err == nil {
		utc := t.UTC()
		if endOfDay {
			utc = utc.Add(24*time.Hour - time.Second)
		}
		return &utc, nil
	}
	return nil, fmt.Errorf("%w: invalid %s %q (expected RFC3339 or YYYY-MM-DD)", domain.ErrInvalidFilter, field, s)
}

func toPhotoResponse(p domain.Photo, includeDetails bool, history []domain.ViewEvent) PhotoResponse {
	formats := make([]string, 0, len(p.Formats))
	for _, f := range p.Formats {
		formats = append(formats, string(f))
	}
	files := toPhotoFileDTOs(p.Files)
	tags := make([]string, 0, len(p.EmojiTags))
	tags = append(tags, p.EmojiTags...)

	resp := PhotoResponse{
		ID:           p.ID,
		BaseName:     p.BaseName,
		FolderPath:   p.FolderPath,
		Formats:      formats,
		MediaType:    string(p.MediaType),
		Width:        p.Width,
		Height:       p.Height,
		DurationSec:  p.DurationSec,
		TakenAt:      formatOptionalTime(p.TakenAt),
		GPSLatitude:  p.EXIF.GPSLatitude,
		GPSLongitude: p.EXIF.GPSLongitude,
		ViewCount:    p.ViewCount,
		EmojiTags:    tags,
		Files:        files,
		LastViewedAt: formatOptionalTimeNano(p.LastViewedAt),
		Status:       string(p.Status),
		ThumbnailURL: "/api/v1/photos/" + url.PathEscape(p.ID) + "/thumbnail",
		PreviewURL:   "/api/v1/photos/" + url.PathEscape(p.ID) + "/preview",
		OriginalURL:  "/api/v1/photos/" + url.PathEscape(p.ID) + "/original",
	}
	if includeDetails {
		exifDTO := toEXIFMetadataDTO(p.EXIF)
		resp.EXIF = &exifDTO
		resp.ViewHistory = toViewEventDTOs(history)
	}
	return resp
}

func toPhotoFileDTOs(files []domain.PhotoFile) []PhotoFileDTO {
	out := make([]PhotoFileDTO, 0, len(files))
	for _, f := range files {
		out = append(out, PhotoFileDTO{
			ID:           f.ID,
			RelativePath: f.RelativePath,
			FileName:     f.FileName,
			FolderPath:   f.FolderPath,
			ContentHash:  f.ContentHash,
			Format:       string(f.Format),
			FileSize:     f.FileSize,
			Status:       string(f.Status),
		})
	}
	return out
}

func toEXIFMetadataDTO(e domain.EXIFMetadata) EXIFMetadataDTO {
	return EXIFMetadataDTO{
		Orientation:   e.Orientation,
		CameraMake:    e.CameraMake,
		CameraModel:   e.CameraModel,
		LensModel:     e.LensModel,
		ISO:           e.ISO,
		FNumber:       e.FNumber,
		ExposureTime:  e.ExposureTime,
		FocalLengthMM: e.FocalLengthMM,
		GPSLatitude:   e.GPSLatitude,
		GPSLongitude:  e.GPSLongitude,
	}
}

func toViewEventDTOs(events []domain.ViewEvent) []ViewEventDTO {
	out := make([]ViewEventDTO, 0, len(events))
	for _, ev := range events {
		out = append(out, ViewEventDTO{
			ID:       ev.ID,
			ViewID:   ev.ViewID,
			PhotoID:  ev.PhotoID,
			ViewedAt: ev.ViewedAt.UTC().Format(time.RFC3339Nano),
			Source:   string(ev.Source),
		})
	}
	return out
}

func toPaginatedPhotosResponse(page domain.PaginatedPhotos) PaginatedPhotosResponse {
	items := make([]PhotoResponse, 0, len(page.Items))
	for _, p := range page.Items {
		items = append(items, toPhotoResponse(p, false, nil))
	}
	return PaginatedPhotosResponse{
		Items:    items,
		Total:    page.Total,
		Page:     page.Page,
		PageSize: page.PageSize,
	}
}

func toEmojiTagCountDTOs(counts []domain.EmojiTagCount) []EmojiTagCountDTO {
	out := make([]EmojiTagCountDTO, 0, len(counts))
	for _, c := range counts {
		out = append(out, EmojiTagCountDTO{
			Emoji: c.Emoji,
			Count: c.Count,
		})
	}
	return out
}

func toSyncReportResponse(r domain.SyncReport) SyncReportResponse {
	var startedAt *string
	if !r.StartedAt.IsZero() {
		s := r.StartedAt.UTC().Format(time.RFC3339)
		startedAt = &s
	}
	return SyncReportResponse{
		ID:             r.ID,
		StartedAt:      startedAt,
		FinishedAt:     formatOptionalTime(r.FinishedAt),
		Status:         string(r.Status),
		ScannedCount:   r.ScannedCount,
		AddedCount:     r.AddedCount,
		UpdatedCount:   r.UpdatedCount,
		MovedCount:     r.MovedCount,
		StackedCount:   r.StackedCount,
		DuplicateCount: r.DuplicateCount,
		UnchangedCount: r.UnchangedCount,
		MissingCount:   r.MissingCount,
		ErrorMessage:   r.ErrorMessage,
	}
}

func toDuplicateGroupDTOs(groups []domain.DuplicateGroup) []DuplicateGroupDTO {
	out := make([]DuplicateGroupDTO, 0, len(groups))
	for _, g := range groups {
		out = append(out, DuplicateGroupDTO{
			ContentHash: g.ContentHash,
			PhotoID:     g.PhotoID,
			FileCount:   g.FileCount,
			Files:       toPhotoFileDTOs(g.Files),
		})
	}
	return out
}

func toLibraryStatsResponse(s domain.LibraryStats) LibraryStatsResponse {
	formats := make([]FormatStatDTO, 0, len(s.ByFormat))
	for _, f := range s.ByFormat {
		formats = append(formats, FormatStatDTO{Format: string(f.Format), Count: f.Count})
	}
	folders := make([]FolderStatDTO, 0, len(s.ByFolder))
	for _, f := range s.ByFolder {
		folders = append(folders, FolderStatDTO{FolderPath: f.FolderPath, Count: f.Count})
	}
	return LibraryStatsResponse{
		TotalPhotos:     s.TotalPhotos,
		UnseenPhotos:    s.UnseenPhotos,
		SeenPhotos:      s.SeenPhotos,
		TotalViews:      s.TotalViews,
		TotalFiles:      s.TotalFiles,
		DuplicateGroups: s.DuplicateGroups,
		DuplicateFiles:  s.DuplicateFiles,
		MissingPhotos:   s.MissingPhotos,
		MissingFiles:    s.MissingFiles,
		ByFormat:        formats,
		ByFolder:        folders,
	}
}

func toSessionResponse(s domain.AuthSession) SessionResponse {
	return SessionResponse{
		Authenticated: s.Authenticated,
		AuthDisabled:  s.AuthDisabled,
	}
}

func formatOptionalTime(t *time.Time) *string {
	if t == nil || t.IsZero() {
		return nil
	}
	s := t.UTC().Format(time.RFC3339)
	return &s
}

func formatOptionalTimeNano(t *time.Time) *string {
	if t == nil || t.IsZero() {
		return nil
	}
	s := t.UTC().Format(time.RFC3339Nano)
	return &s
}

package httpapi

import (
	"errors"
	"net/url"
	"testing"
	"time"

	"github.com/lucastheo/photos/internal/domain"
)

func intPtr(v int) *int           { return &v }
func floatPtr(v float64) *float64 { return &v }
func boolPtr(v bool) *bool        { return &v }
func strPtr(v string) *string     { return &v }

func TestParsePhotoFilterFromQuery(t *testing.T) {
	t.Parallel()

	t.Run("parses full filter query parameters", func(t *testing.T) {
		t.Parallel()
		q := url.Values{
			"min_views":  {"1"},
			"max_views":  {"5"},
			"emoji_tags": {"❤️,🏖️", "⭐"},
			"formats":    {"cr3,jpg"},
			"media_type": {"photo"},
			"folder":     {"/2026/Trip/"},
			"has_gps":    {"true"},
			"min_lat":    {"-34.5"},
			"max_lat":    {"-34.0"},
			"min_lng":    {"-58.6"},
			"max_lng":    {"-58.1"},
			"from_date":  {"2026-01-01"},
			"to_date":    {"2026-01-31"},
			"q":          {"IMG_0042"},
			"sort_by":    {"random"},
			"sort_order": {"desc"},
			"seed":       {"42"},
			"page":       {"2"},
			"page_size":  {"25"},
			"status":     {"missing"},
		}
		got, err := parsePhotoFilterFromQuery(q)
		if err != nil {
			t.Fatalf("parsePhotoFilterFromQuery failed: %v", err)
		}
		if *got.MinViews != 1 || *got.MaxViews != 5 || len(got.EmojiTags) != 3 ||
			got.EmojiTags[0] != "❤" || len(got.Formats) != 2 || got.Folder != "2026/Trip" ||
			!*got.HasGPS || got.SortBy != domain.SortByRandom || got.SortOrder != domain.SortOrderDesc ||
			got.Seed != 42 || got.Page != 2 || got.PageSize != 25 || got.Status != domain.FileStatusMissing {
			t.Fatalf("unexpected parsed filter: %+v", got)
		}
		wantFrom := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
		wantTo := time.Date(2026, 1, 31, 23, 59, 59, 0, time.UTC)
		if !got.FromDate.Equal(wantFrom) || !got.ToDate.Equal(wantTo) {
			t.Fatalf("dates = (%v, %v), want (%v, %v)", got.FromDate, got.ToDate, wantFrom, wantTo)
		}
	})

	t.Run("parses exact_views untagged and RFC3339 dates", func(t *testing.T) {
		t.Parallel()
		q := url.Values{
			"exact_views": {"0"},
			"untagged":    {"true"},
			"from_date":   {"2026-03-01T10:20:30Z"},
			"to_date":     {"2026-03-02T10:20:30Z"},
		}
		got, err := parsePhotoFilterFromQuery(q)
		if err != nil {
			t.Fatalf("parsePhotoFilterFromQuery failed: %v", err)
		}
		if *got.ExactViews != 0 || !got.Untagged || got.FromDate.Hour() != 10 {
			t.Fatalf("unexpected filter: %+v", got)
		}
	})

	t.Run("rejects malformed query parameters with ErrInvalidFilter", func(t *testing.T) {
		t.Parallel()
		cases := []struct {
			name string
			q    url.Values
		}{
			{name: "bad min_views", q: url.Values{"min_views": {"abc"}}},
			{name: "bad max_views", q: url.Values{"max_views": {"abc"}}},
			{name: "bad exact_views", q: url.Values{"exact_views": {"abc"}}},
			{name: "bad has_gps", q: url.Values{"has_gps": {"maybe"}}},
			{name: "bad untagged", q: url.Values{"untagged": {"nope"}}},
			{name: "bad min_lat", q: url.Values{"min_lat": {"nanx"}}},
			{name: "bad max_lat", q: url.Values{"max_lat": {"nanx"}}},
			{name: "bad min_lng", q: url.Values{"min_lng": {"nanx"}}},
			{name: "bad max_lng", q: url.Values{"max_lng": {"nanx"}}},
			{name: "bad seed", q: url.Values{"seed": {"xyz"}}},
			{name: "bad page", q: url.Values{"page": {"xyz"}}},
			{name: "bad page_size", q: url.Values{"page_size": {"xyz"}}},
			{name: "bad from_date", q: url.Values{"from_date": {"not-a-date"}}},
			{name: "bad to_date", q: url.Values{"to_date": {"not-a-date"}}},
			{name: "bad status", q: url.Values{"status": {"bogus"}}},
		}
		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				t.Parallel()
				if _, err := parsePhotoFilterFromQuery(tc.q); !errors.Is(err, domain.ErrInvalidFilter) {
					t.Fatalf("expected ErrInvalidFilter, got %v", err)
				}
			})
		}
	})

	t.Run("toDomainPhotoFilter maps DTO directly", func(t *testing.T) {
		t.Parallel()
		got, err := toDomainPhotoFilter(ListPhotosRequest{
			MinViews:  intPtr(0),
			MaxViews:  intPtr(2),
			Formats:   []string{"heic"},
			MinLat:    floatPtr(10),
			MaxLat:    floatPtr(20),
			HasGPS:    boolPtr(true),
			FromDate:  strPtr("2026-05-01"),
			ToDate:    strPtr("2026-05-02"),
			SortBy:    "views",
			SortOrder: "asc",
			Page:      1,
			PageSize:  10,
		})
		if err != nil {
			t.Fatalf("toDomainPhotoFilter failed: %v", err)
		}
		if len(got.Formats) != 1 || got.Formats[0] != domain.FormatHEIC {
			t.Fatalf("unexpected formats: %+v", got.Formats)
		}
	})
}

func TestMapperDomainToDTOConversions(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 7, 20, 14, 30, 0, 123456789, time.UTC)
	iso := 200
	fnum := 2.8
	focal := 35.0
	lat := -34.6037
	lng := -58.3816

	photo := domain.Photo{
		ID:          "p-1",
		BaseName:    "IMG_0042",
		FolderPath:  "2026/Trip",
		MediaType:   domain.MediaTypePhoto,
		Formats:     []domain.MediaFormat{domain.FormatCR3, domain.FormatJPG},
		Width:       6000,
		Height:      4000,
		DurationSec: 0,
		TakenAt:     &now,
		EXIF: domain.EXIFMetadata{
			Orientation:   6,
			CameraMake:    "Canon",
			CameraModel:   "EOS R5",
			LensModel:     "RF 35mm",
			ISO:           &iso,
			FNumber:       &fnum,
			ExposureTime:  "1/250",
			FocalLengthMM: &focal,
			GPSLatitude:   &lat,
			GPSLongitude:  &lng,
		},
		ViewCount:    3,
		EmojiTags:    []string{"❤", "🏖"},
		LastViewedAt: &now,
		Status:       domain.FileStatusActive,
		Files: []domain.PhotoFile{
			{
				ID:           "f-1",
				PhotoID:      "p-1",
				ContentHash:  "hash-cr3",
				RelativePath: "2026/Trip/IMG_0042.CR3",
				FileName:     "IMG_0042.CR3",
				FolderPath:   "2026/Trip",
				Format:       domain.FormatCR3,
				FileSize:     1024,
				Status:       domain.FileStatusActive,
			},
		},
	}
	history := []domain.ViewEvent{
		{ID: 7, ViewID: "v-1", PhotoID: "p-1", ViewedAt: now, Source: domain.ViewSourceDwell},
	}

	detail := toPhotoResponse(photo, true, history)
	if detail.ID != "p-1" || detail.EXIF == nil || *detail.EXIF.ISO != 200 ||
		*detail.GPSLatitude != lat || len(detail.ViewHistory) != 1 ||
		detail.LastViewedAt == nil || *detail.LastViewedAt != detail.ViewHistory[0].ViewedAt ||
		detail.ThumbnailURL != "/api/v1/photos/p-1/thumbnail" ||
		detail.PreviewURL != "/api/v1/photos/p-1/preview" ||
		detail.OriginalURL != "/api/v1/photos/p-1/original" {
		t.Fatalf("unexpected detail PhotoResponse: %+v", detail)
	}

	paged := toPaginatedPhotosResponse(domain.PaginatedPhotos{
		Items:    []domain.Photo{photo},
		Total:    1,
		Page:     1,
		PageSize: 50,
	})
	if paged.Total != 1 || len(paged.Items) != 1 || paged.Items[0].EXIF != nil || len(paged.Items[0].ViewHistory) != 0 {
		t.Fatalf("unexpected paginated response: %+v", paged)
	}

	tags := toEmojiTagCountDTOs([]domain.EmojiTagCount{{Emoji: "❤", Count: 4}})
	if len(tags) != 1 || tags[0].Emoji != "❤" || tags[0].Count != 4 {
		t.Fatalf("unexpected tag DTOs: %+v", tags)
	}

	rep := toSyncReportResponse(domain.SyncReport{
		ID:             "sync-1",
		StartedAt:      now,
		FinishedAt:     &now,
		Status:         domain.SyncStatusCompleted,
		ScannedCount:   10,
		AddedCount:     8,
		UpdatedCount:   1,
		MovedCount:     1,
		StackedCount:   2,
		DuplicateCount: 1,
		UnchangedCount: 0,
		MissingCount:   0,
	})
	if rep.ID != "sync-1" || rep.StartedAt == nil || rep.FinishedAt == nil || rep.AddedCount != 8 {
		t.Fatalf("unexpected SyncReportResponse: %+v", rep)
	}

	dups := toDuplicateGroupDTOs([]domain.DuplicateGroup{
		{ContentHash: "h1", PhotoID: "p-1", FileCount: 2, Files: photo.Files},
	})
	if len(dups) != 1 || dups[0].FileCount != 2 || len(dups[0].Files) != 1 {
		t.Fatalf("unexpected DuplicateGroupDTOs: %+v", dups)
	}

	stats := toLibraryStatsResponse(domain.LibraryStats{
		TotalPhotos:     5,
		UnseenPhotos:    2,
		SeenPhotos:      3,
		TotalViews:      7,
		TotalFiles:      6,
		DuplicateGroups: 1,
		DuplicateFiles:  2,
		MissingPhotos:   1,
		MissingFiles:    1,
		ByFormat:        []domain.FormatStat{{Format: domain.FormatJPG, Count: 4}},
		ByFolder:        []domain.FolderStat{{FolderPath: "2026", Count: 5}},
	})
	if stats.TotalPhotos != 5 || len(stats.ByFormat) != 1 || len(stats.ByFolder) != 1 {
		t.Fatalf("unexpected LibraryStatsResponse: %+v", stats)
	}

	sess := toSessionResponse(domain.AuthSession{Authenticated: true, AuthDisabled: false})
	if !sess.Authenticated || sess.AuthDisabled {
		t.Fatalf("unexpected SessionResponse: %+v", sess)
	}
}

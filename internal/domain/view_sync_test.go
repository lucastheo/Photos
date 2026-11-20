package domain_test

import (
	"errors"
	"testing"
	"time"

	"github.com/lucastheo/photos/internal/domain"
)

func TestViewSourceIsValid(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name  string
		input domain.ViewSource
		want  bool
	}{
		{name: "dwell", input: domain.ViewSourceDwell, want: true},
		{name: "manual", input: domain.ViewSourceManual, want: true},
		{name: "slideshow", input: domain.ViewSourceSlideshow, want: true},
		{name: "empty", input: "", want: false},
		{name: "unknown", input: "api", want: false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := tc.input.IsValid(); got != tc.want {
				t.Fatalf("ViewSource(%q).IsValid() = %v, want %v", tc.input, got, tc.want)
			}
		})
	}
}

func TestViewActionIsValid(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name  string
		input domain.ViewAction
		want  bool
	}{
		{name: "increment", input: domain.ViewActionIncrement, want: true},
		{name: "decrement", input: domain.ViewActionDecrement, want: true},
		{name: "reset", input: domain.ViewActionReset, want: true},
		{name: "empty", input: "", want: false},
		{name: "unknown", input: "set", want: false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := tc.input.IsValid(); got != tc.want {
				t.Fatalf("ViewAction(%q).IsValid() = %v, want %v", tc.input, got, tc.want)
			}
		})
	}
}

func TestSyncStatusIsValid(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name  string
		input domain.SyncStatus
		want  bool
	}{
		{name: "running", input: domain.SyncStatusRunning, want: true},
		{name: "completed", input: domain.SyncStatusCompleted, want: true},
		{name: "failed", input: domain.SyncStatusFailed, want: true},
		{name: "empty", input: "", want: false},
		{name: "unknown", input: "pending", want: false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := tc.input.IsValid(); got != tc.want {
				t.Fatalf("SyncStatus(%q).IsValid() = %v, want %v", tc.input, got, tc.want)
			}
		})
	}
}

func TestSentinelErrorsDistinct(t *testing.T) {
	t.Parallel()

	sentinels := []error{
		domain.ErrPhotoNotFound,
		domain.ErrFileNotFound,
		domain.ErrInvalidEmojiTag,
		domain.ErrInvalidFilter,
		domain.ErrInvalidViewAction,
		domain.ErrInvalidViewSource,
		domain.ErrSyncInProgress,
		domain.ErrSyncNotFound,
		domain.ErrSettingNotFound,
		domain.ErrUnsupportedFormat,
		domain.ErrLibraryUnavailable,
	}

	for i := range sentinels {
		if sentinels[i] == nil || sentinels[i].Error() == "" {
			t.Fatalf("sentinel[%d] is nil or empty", i)
		}
		for j := i + 1; j < len(sentinels); j++ {
			if errors.Is(sentinels[i], sentinels[j]) {
				t.Fatalf("sentinels %d and %d are not distinct", i, j)
			}
		}
	}
}

func TestViewAndSyncStructsInstantiation(t *testing.T) {
	t.Parallel()

	now := time.Now().UTC()
	view := domain.ViewEvent{
		ID:       1,
		ViewID:   "v1",
		PhotoID:  "p1",
		ViewedAt: now,
		Source:   domain.ViewSourceDwell,
	}
	vStats := domain.ViewStats{TotalViews: 10, UnseenPhotos: 5, SeenPhotos: 4}
	tagCount := domain.EmojiTagCount{Emoji: "\u2764", Count: 7}
	scanned := domain.ScannedFile{
		RelativePath: "2026/IMG_1.CR3",
		FileName:     "IMG_1.CR3",
		FolderPath:   "2026",
		BaseName:     "IMG_1",
		Format:       domain.FormatCR3,
		MediaType:    domain.MediaTypePhoto,
		FileSize:     2048,
		ModTime:      now,
		ContentHash:  "deadbeef",
	}
	dup := domain.DuplicateGroup{
		ContentHash: "deadbeef",
		PhotoID:     "p1",
		FileCount:   2,
		Files:       []domain.PhotoFile{{ID: "f1"}, {ID: "f2"}},
	}
	report := domain.SyncReport{
		ID:             "s1",
		StartedAt:      now,
		FinishedAt:     &now,
		Status:         domain.SyncStatusCompleted,
		ScannedCount:   10,
		AddedCount:     5,
		UpdatedCount:   1,
		MovedCount:     1,
		StackedCount:   1,
		DuplicateCount: 1,
		UnchangedCount: 1,
		MissingCount:   0,
	}
	libStats := domain.LibraryStats{
		TotalPhotos:     9,
		UnseenPhotos:    5,
		SeenPhotos:      4,
		TotalViews:      10,
		TotalFiles:      11,
		DuplicateGroups: 1,
		DuplicateFiles:  2,
		MissingPhotos:   0,
		MissingFiles:    0,
		ByFormat:        []domain.FormatStat{{Format: domain.FormatCR3, Count: 5}},
		ByFolder:        []domain.FolderStat{{FolderPath: "2026", Count: 9}},
	}
	page := domain.PaginatedPhotos{Items: []domain.Photo{{ID: "p1"}}, Total: 1, Page: 1, PageSize: 50}
	move := domain.FileMove{
		FileID:       "f1",
		RelativePath: "2026/Renamed.CR3",
		FileName:     "Renamed.CR3",
		FolderPath:   "2026",
	}
	meta := domain.MediaMetadata{
		Width:       6000,
		Height:      4000,
		DurationSec: 0,
		TakenAt:     &now,
		EXIF:        domain.EXIFMetadata{Orientation: 6},
	}

	if view.Source != domain.ViewSourceDwell || vStats.TotalViews != 10 || tagCount.Count != 7 ||
		scanned.Format != domain.FormatCR3 || dup.FileCount != 2 || report.Status != domain.SyncStatusCompleted ||
		libStats.TotalPhotos != 9 || page.Total != 1 || move.FileID != "f1" || meta.EXIF.Orientation != 6 {
		t.Fatal("unexpected zero field in domain structs")
	}
}

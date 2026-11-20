package sqlite_test

import (
	"context"
	"errors"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/lucastheo/photos/internal/adapter/sqlite"
	"github.com/lucastheo/photos/internal/domain"
)

func TestPhotoRepository_Integration_UpdatePhotoMetadataAndSplitMovedFile(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	ctx := context.Background()
	db := openTestDB(t, filepath.Join(t.TempDir(), "photos.db"))
	repo := sqlite.NewPhotoRepository(db.SQL())
	viewRepo := sqlite.NewViewRepository(db.SQL())
	now := time.Date(2026, 8, 1, 12, 0, 0, 0, time.UTC)

	stacked := makeSamplePhoto("p-stack", "2026/Trip", "IMG_0042", domain.MediaTypePhoto, []domain.PhotoFile{
		{
			ID: "f-jpg", ContentHash: "h-jpg", RelativePath: "2026/Trip/IMG_0042.jpg",
			FileName: "IMG_0042.jpg", FolderPath: "2026/Trip", Format: domain.FormatJPG,
			FileSize: 1200, ModTime: now, Status: domain.FileStatusActive,
		},
		{
			ID: "f-cr3", ContentHash: "h-cr3", RelativePath: "2026/Trip/IMG_0042.CR3",
			FileName: "IMG_0042.CR3", FolderPath: "2026/Trip", Format: domain.FormatCR3,
			FileSize: 25000, ModTime: now, Status: domain.FileStatusActive,
		},
	}, now)
	if err := repo.CreatePhoto(ctx, stacked); err != nil {
		t.Fatalf("CreatePhoto stacked failed: %v", err)
	}
	if _, err := viewRepo.RecordView(ctx, domain.ViewEvent{
		ViewID: "v-1", PhotoID: "p-stack", ViewedAt: now.Add(time.Minute), Source: domain.ViewSourceDwell,
	}); err != nil {
		t.Fatalf("RecordView failed: %v", err)
	}
	if _, err := viewRepo.AdjustViewCount(ctx, "p-stack", domain.ViewActionIncrement, now.Add(2*time.Minute)); err != nil {
		t.Fatalf("AdjustViewCount failed: %v", err)
	}
	if err := repo.AddTag(ctx, "p-stack", "❤️", now); err != nil {
		t.Fatalf("AddTag failed: %v", err)
	}

	// UpdatePhotoMetadata updates EXIF, dimensions, and GPS.
	if err := repo.UpdatePhotoMetadata(ctx, domain.Photo{ID: ""}); !errors.Is(err, domain.ErrPhotoNotFound) {
		t.Fatalf("UpdatePhotoMetadata empty ID err = %v, want ErrPhotoNotFound", err)
	}
	if err := repo.UpdatePhotoMetadata(ctx, domain.Photo{ID: "unknown"}); !errors.Is(err, domain.ErrPhotoNotFound) {
		t.Fatalf("UpdatePhotoMetadata missing err = %v, want ErrPhotoNotFound", err)
	}

	iso := 800
	lat := -22.9068
	lng := -43.1729
	taken := now.Add(-time.Hour)
	if err := repo.UpdatePhotoMetadata(ctx, domain.Photo{
		ID:      "p-stack",
		Width:   6000,
		Height:  4000,
		TakenAt: &taken,
		EXIF: domain.EXIFMetadata{
			Orientation:  6,
			CameraMake:   "Canon",
			CameraModel:  "EOS R6",
			ISO:          &iso,
			GPSLatitude:  &lat,
			GPSLongitude: &lng,
		},
	}); err != nil {
		t.Fatalf("UpdatePhotoMetadata failed: %v", err)
	}
	updated, err := repo.GetByID(ctx, "p-stack")
	if err != nil || updated.Width != 6000 || updated.EXIF.CameraMake != "Canon" || updated.EXIF.GPSLatitude == nil {
		t.Fatalf("GetByID after metadata update = (%+v, %v)", updated, err)
	}

	// SplitMovedFile splits f-jpg into Favorites/IMG_0042_fav.jpg while f-cr3 stays in 2026/Trip.
	// Even if caller passes a smaller ViewCount (1 < 2), ViewCount is clamped to oldPhoto.ViewCount so view_count == COUNT(photo_views).
	err = repo.SplitMovedFile(ctx, "f-jpg", domain.Photo{ID: "p-split", ViewCount: 1}, domain.FileMove{
		RelativePath: "Favorites/IMG_0042_fav.jpg",
		FileName:     "IMG_0042_fav.jpg",
		FolderPath:   "Favorites",
	}, now.Add(3*time.Minute))
	if err != nil {
		t.Fatalf("SplitMovedFile failed: %v", err)
	}

	origAfter, err := repo.GetByID(ctx, "p-stack")
	if err != nil {
		t.Fatalf("GetByID origAfter failed: %v", err)
	}
	if len(origAfter.Files) != 1 || origAfter.Files[0].ID != "f-cr3" || origAfter.ViewCount != 2 {
		t.Fatalf("origAfter unexpected: %+v", origAfter)
	}

	splitPhoto, err := repo.GetByID(ctx, "p-split")
	if err != nil {
		t.Fatalf("GetByID splitPhoto failed: %v", err)
	}
	if splitPhoto.FolderPath != "Favorites" || splitPhoto.BaseName != "IMG_0042_fav" ||
		splitPhoto.ViewCount != 2 || !slices.Equal(splitPhoto.EmojiTags, []string{"\u2764"}) ||
		splitPhoto.Width != 6000 || len(splitPhoto.Files) != 1 {
		t.Fatalf("splitPhoto unexpected: %+v", splitPhoto)
	}
	splitHistory, err := viewRepo.ListViewHistory(ctx, "p-split", 10)
	if err != nil || len(splitHistory) != 2 {
		t.Fatalf("splitHistory = (%d, %v), want 2", len(splitHistory), err)
	}

	// Padding branch: seed a photo with ViewCount=2 but 0 photo_views rows, then split.
	if err := repo.CreatePhoto(ctx, domain.Photo{
		ID: "p-pad", StackKey: "A/photo/shot", BaseName: "shot", FolderPath: "A", MediaType: domain.MediaTypePhoto,
		ViewCount: 2,
		Files: []domain.PhotoFile{
			{ID: "f-pad-1", ContentHash: "hp1", RelativePath: "A/shot.CR3", FileName: "shot.CR3", FolderPath: "A", Format: domain.FormatCR3, FileSize: 10, ModTime: now},
			{ID: "f-pad-2", ContentHash: "hp2", RelativePath: "A/shot.jpg", FileName: "shot.jpg", FolderPath: "A", Format: domain.FormatJPG, FileSize: 5, ModTime: now},
		},
		CreatedAt: now, UpdatedAt: now,
	}); err != nil {
		t.Fatalf("CreatePhoto p-pad failed: %v", err)
	}
	if err := repo.SplitMovedFile(ctx, "f-pad-2", domain.Photo{ID: "p-pad-split"}, domain.FileMove{
		RelativePath: "B/shot.jpg",
	}, time.Time{}); err != nil {
		t.Fatalf("SplitMovedFile pad failed: %v", err)
	}
	padHistory, err := viewRepo.ListViewHistory(ctx, "p-pad-split", 10)
	if err != nil || len(padHistory) != 2 {
		t.Fatalf("padHistory = (%d, %v), want 2", len(padHistory), err)
	}

	// Validation & error paths.
	if err := repo.SplitMovedFile(ctx, "", domain.Photo{ID: "x"}, domain.FileMove{RelativePath: "C/x.jpg"}, now); err == nil {
		t.Fatal("expected error for empty fileID")
	}
	if err := repo.SplitMovedFile(ctx, "f-pad-1", domain.Photo{ID: "x"}, domain.FileMove{RelativePath: ""}, now); err == nil {
		t.Fatal("expected error for empty move path")
	}
	if err := repo.SplitMovedFile(ctx, "missing-file", domain.Photo{ID: "x"}, domain.FileMove{RelativePath: "C/x.jpg"}, now); !errors.Is(err, domain.ErrFileNotFound) {
		t.Fatalf("expected ErrFileNotFound, got %v", err)
	}
	// Duplicate ID rolls back split cleanly.
	if err := repo.SplitMovedFile(ctx, "f-cr3", domain.Photo{ID: "p-split"}, domain.FileMove{RelativePath: "D/x.CR3"}, now); err == nil {
		t.Fatal("expected duplicate photo ID error")
	}

	if err := db.Close(); err != nil {
		t.Fatalf("db.Close failed: %v", err)
	}
	if err := repo.UpdatePhotoMetadata(ctx, domain.Photo{ID: "p-stack"}); err == nil {
		t.Fatal("expected UpdatePhotoMetadata error on closed DB")
	}
	if err := repo.SplitMovedFile(ctx, "f-cr3", domain.Photo{ID: "p-new"}, domain.FileMove{RelativePath: "E/x.CR3"}, now); err == nil {
		t.Fatal("expected SplitMovedFile error on closed DB")
	}
}

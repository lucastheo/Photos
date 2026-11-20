package usecase_test

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/lucastheo/photos/internal/domain"
	"github.com/lucastheo/photos/internal/usecase"
)

func testClock(start time.Time) func() time.Time {
	cur := start
	return func() time.Time {
		out := cur
		cur = cur.Add(time.Second)
		return out
	}
}

func testSeqIDs() func() string {
	var n atomic.Int64
	return func() string {
		return fmt.Sprintf("id-%d", n.Add(1))
	}
}

func TestSyncLibraryUseCase_InitialImportMixedFormatsAndStacking(t *testing.T) {
	now := time.Date(2026, 7, 10, 12, 0, 0, 0, time.UTC)
	takenCR3 := now.Add(-48 * time.Hour)
	takenJPG := now.Add(-24 * time.Hour)

	tests := []struct {
		name          string
		scanned       []domain.ScannedFile
		wantAdded     int
		wantStacked   int
		wantDups      int
		wantStackW    int
		wantStackMake string
	}{
		{
			name: "jpg arrives before cr3 and video does not stack",
			scanned: []domain.ScannedFile{
				{RelativePath: "Trip/IMG_0042.jpg", FileName: "IMG_0042.jpg", FolderPath: "Trip", BaseName: "IMG_0042", Format: domain.FormatJPG, MediaType: domain.MediaTypePhoto, FileSize: 1000, ModTime: now},
				{RelativePath: "Trip/IMG_0042.CR3", FileName: "IMG_0042.CR3", FolderPath: "Trip", BaseName: "IMG_0042", Format: domain.FormatCR3, MediaType: domain.MediaTypePhoto, FileSize: 25000, ModTime: now},
				{RelativePath: "Trip/IMG_0042.MOV", FileName: "IMG_0042.MOV", FolderPath: "Trip", BaseName: "IMG_0042", Format: domain.FormatMOV, MediaType: domain.MediaTypeVideo, FileSize: 50000, ModTime: now},
				{RelativePath: "Trip/IMG_0042.mp4", FileName: "IMG_0042.mp4", FolderPath: "Trip", BaseName: "IMG_0042", Format: domain.FormatMP4, MediaType: domain.MediaTypeVideo, FileSize: 40000, ModTime: now},
				{RelativePath: "Backup/IMG_0042.jpg", FileName: "IMG_0042.jpg", FolderPath: "Backup", BaseName: "IMG_0042", Format: domain.FormatJPG, MediaType: domain.MediaTypePhoto, FileSize: 1000, ModTime: now},
				// Duplicate relative path normalized (NFD/slash) gets deduplicated.
				{RelativePath: "/Trip/IMG_0042.jpg", Format: domain.FormatJPG, FileSize: 1000, ModTime: now},
			},
			wantAdded:     3, // stack + mov + mp4
			wantStacked:   1, // cr3 onto jpg
			wantDups:      1, // backup jpg
			wantStackW:    6000,
			wantStackMake: "Canon CR3",
		},
		{
			name: "cr3 arrives before jpg keeps cr3 metadata",
			scanned: []domain.ScannedFile{
				{RelativePath: "Trip/IMG_0042.CR3", Format: domain.FormatCR3, FileSize: 25000, ModTime: now},
				{RelativePath: "Trip/IMG_0042.jpg", Format: domain.FormatJPG, FileSize: 1000, ModTime: now},
				{RelativePath: "Trip/IMG_0042.MOV", Format: domain.FormatMOV, FileSize: 50000, ModTime: now},
			},
			wantAdded:     2,
			wantStacked:   1,
			wantDups:      0,
			wantStackW:    6000,
			wantStackMake: "Canon CR3",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			store := newFakePhotoStore()
			runs := newFakeSyncRunStore()
			maintainer := &fakeMaintainer{}
			previews := newFakePreviewExtractor()
			scanner := newFakeMediaScanner(tc.scanned, map[string]string{
				"Trip/IMG_0042.jpg":   "h-jpg",
				"Backup/IMG_0042.jpg": "h-jpg",
				"Trip/IMG_0042.CR3":   "h-cr3",
				"Trip/IMG_0042.MOV":   "h-mov",
				"Trip/IMG_0042.mp4":   "h-mp4",
			})
			extractor := &fakeMetadataExtractor{
				byPath: map[string]domain.MediaMetadata{
					"Trip/IMG_0042.jpg": {Width: 4000, Height: 3000, TakenAt: &takenJPG, EXIF: domain.EXIFMetadata{Orientation: 1, CameraMake: "Canon JPG"}},
					"Trip/IMG_0042.CR3": {Width: 6000, Height: 4000, TakenAt: &takenCR3, EXIF: domain.EXIFMetadata{Orientation: 6, CameraMake: "Canon CR3"}},
					"Trip/IMG_0042.MOV": {Width: 1920, Height: 1080, DurationSec: 3.5}, // TakenAt nil -> ModTime fallback
					"Trip/IMG_0042.mp4": {Width: 1920, Height: 1080, DurationSec: 4.0},
				},
			}

			uc := usecase.NewSyncLibraryUseCase(store, scanner, previews).
				WithMetadataExtractor(extractor).
				WithSyncRunStore(runs).
				WithDatabaseMaintainer(maintainer).
				WithClock(testClock(now)).
				WithIDGenerator(testSeqIDs())

			report, err := uc.Sync(context.Background())
			if err != nil {
				t.Fatalf("Sync failed: %v", err)
			}
			if report.Status != domain.SyncStatusCompleted ||
				report.AddedCount != tc.wantAdded ||
				report.StackedCount != tc.wantStacked ||
				report.DuplicateCount != tc.wantDups {
				t.Fatalf("unexpected report: %+v", report)
			}
			if maintainer.checkpointCalls != 1 {
				t.Fatalf("checkpointCalls = %d, want 1", maintainer.checkpointCalls)
			}

			stacked, err := store.GetByStackKey(context.Background(), "Trip/photo/img_0042")
			if err != nil {
				t.Fatalf("GetByStackKey failed: %v", err)
			}
			if stacked.Width != tc.wantStackW || stacked.EXIF.CameraMake != tc.wantStackMake || stacked.ViewCount != 0 {
				t.Fatalf("stacked photo metadata unexpected: %+v", stacked)
			}
			movPhoto, err := store.GetByStackKey(context.Background(), "Trip/video/img_0042.mov")
			if err != nil || movPhoto.ID == stacked.ID || movPhoto.TakenAt == nil || !movPhoto.TakenAt.Equal(now) {
				t.Fatalf("video should not stack and TakenAt should fallback to ModTime: (%+v, %v)", movPhoto, err)
			}
			if previews.orientation["Trip/IMG_0042.CR3"] != 6 {
				t.Fatalf("CR3 orientation = %d, want 6", previews.orientation["Trip/IMG_0042.CR3"])
			}
		})
	}

	t.Run("stacked CR3 with zero dimensions preserves JPG dimensions while updating EXIF", func(t *testing.T) {
		store := newFakePhotoStore()
		scanner := newFakeMediaScanner([]domain.ScannedFile{
			{RelativePath: "Trip/IMG_0042.jpg", Format: domain.FormatJPG, FileSize: 1000, ModTime: now},
			{RelativePath: "Trip/IMG_0042.CR3", Format: domain.FormatCR3, FileSize: 25000, ModTime: now},
		}, map[string]string{
			"Trip/IMG_0042.jpg": "h-jpg",
			"Trip/IMG_0042.CR3": "h-cr3",
		})
		extractor := &fakeMetadataExtractor{
			byPath: map[string]domain.MediaMetadata{
				"Trip/IMG_0042.jpg": {Width: 4000, Height: 3000, DurationSec: 1.5, TakenAt: &takenJPG, EXIF: domain.EXIFMetadata{Orientation: 1, CameraMake: "Canon JPG"}},
				"Trip/IMG_0042.CR3": {Width: 0, Height: 0, DurationSec: 0, TakenAt: &takenCR3, EXIF: domain.EXIFMetadata{Orientation: 6, CameraMake: "Canon CR3"}},
			},
		}
		uc := usecase.NewSyncLibraryUseCase(store, scanner, nil).
			WithMetadataExtractor(extractor).
			WithClock(testClock(now)).
			WithIDGenerator(testSeqIDs())

		rep, err := uc.Sync(context.Background())
		if err != nil || rep.AddedCount != 1 || rep.StackedCount != 1 {
			t.Fatalf("Sync = (%+v, %v)", rep, err)
		}
		stacked, err := store.GetByStackKey(context.Background(), "Trip/photo/img_0042")
		if err != nil {
			t.Fatalf("GetByStackKey failed: %v", err)
		}
		if stacked.Width != 4000 || stacked.Height != 3000 || stacked.DurationSec != 1.5 || stacked.EXIF.CameraMake != "Canon CR3" {
			t.Fatalf("expected preserved 4000x3000/1.5s and updated CameraMake, got %+v", stacked)
		}
	})
}

func TestSyncLibraryUseCase_FastSkipAndReactivateMissing(t *testing.T) {
	now := time.Date(2026, 7, 11, 10, 0, 0, 0, time.UTC)

	tests := []struct {
		name           string
		seedFiles      []domain.PhotoFile
		scanned        []domain.ScannedFile
		wantUnchanged  int
		wantUpdated    int
		wantHashCalls  int
		wantFileStatus domain.FileStatus
	}{
		{
			name: "unchanged active files skip hashing",
			seedFiles: []domain.PhotoFile{
				{ID: "f1", PhotoID: "p1", ContentHash: "h1", RelativePath: "Trip/caf\u00e9.jpg", FileName: "caf\u00e9.jpg", FolderPath: "Trip", Format: domain.FormatJPG, FileSize: 500, ModTime: now, Status: domain.FileStatusActive},
				{ID: "f2", PhotoID: "p2", ContentHash: "h2", RelativePath: "zero.jpg", FileName: "zero.jpg", FolderPath: "", Format: domain.FormatJPG, FileSize: 100, ModTime: time.Time{}, Status: domain.FileStatusActive},
			},
			scanned: []domain.ScannedFile{
				// NFD + backslash path normalizes to Trip/café.jpg.
				{RelativePath: "Trip\\cafe\u0301.jpg", Format: domain.FormatJPG, FileSize: 500, ModTime: now},
				{RelativePath: "zero.jpg", Format: domain.FormatJPG, FileSize: 100, ModTime: time.Time{}},
			},
			wantUnchanged:  2,
			wantUpdated:    0,
			wantHashCalls:  0,
			wantFileStatus: domain.FileStatusActive,
		},
		{
			name: "unchanged missing file reactivates preserving hash",
			seedFiles: []domain.PhotoFile{
				{ID: "f1", PhotoID: "p1", ContentHash: "h1-keep", RelativePath: "Trip/A.jpg", FileName: "A.jpg", FolderPath: "Trip", Format: domain.FormatJPG, FileSize: 500, ModTime: now, Status: domain.FileStatusMissing},
			},
			scanned: []domain.ScannedFile{
				{RelativePath: "Trip/A.jpg", Format: domain.FormatJPG, FileSize: 500, ModTime: now},
			},
			wantUnchanged:  0,
			wantUpdated:    1,
			wantHashCalls:  0,
			wantFileStatus: domain.FileStatusActive,
		},
		{
			name: "empty scan with only missing records completes cleanly",
			seedFiles: []domain.PhotoFile{
				{ID: "f1", PhotoID: "p1", ContentHash: "h1", RelativePath: "Trip/A.jpg", FileName: "A.jpg", FolderPath: "Trip", Format: domain.FormatJPG, FileSize: 500, ModTime: now, Status: domain.FileStatusMissing},
			},
			scanned:        nil,
			wantUnchanged:  0,
			wantUpdated:    0,
			wantHashCalls:  0,
			wantFileStatus: domain.FileStatusMissing,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			store := newFakePhotoStore()
			for _, f := range tc.seedFiles {
				store.seedPhoto(domain.Photo{
					ID: f.PhotoID, BaseName: "base", FolderPath: f.FolderPath,
					MediaType: domain.MediaTypePhoto, Files: []domain.PhotoFile{f},
					ViewCount: 3, EmojiTags: []string{"❤"}, Status: f.Status,
				})
			}
			scanner := newFakeMediaScanner(tc.scanned, nil)
			uc := usecase.NewSyncLibraryUseCase(store, scanner, nil).WithClock(testClock(now))

			report, err := uc.Sync(context.Background())
			if err != nil {
				t.Fatalf("Sync failed: %v", err)
			}
			if report.UnchangedCount != tc.wantUnchanged || report.UpdatedCount != tc.wantUpdated || scanner.hashCalls != tc.wantHashCalls {
				t.Fatalf("report=%+v hashCalls=%d", report, scanner.hashCalls)
			}
			got, _ := store.GetByID(context.Background(), "p1")
			if got.Status != tc.wantFileStatus || got.Files[0].ContentHash == "" || got.ViewCount != 3 {
				t.Fatalf("p1 unexpected: %+v", got)
			}
		})
	}
}

func TestSyncLibraryUseCase_InPlaceEditPrimaryVsNonPrimary(t *testing.T) {
	now := time.Date(2026, 7, 12, 10, 0, 0, 0, time.UTC)
	later := now.Add(time.Minute)

	tests := []struct {
		name               string
		editPath           string
		withExtractor      bool
		wantMetaUpdateCall int
		wantWidth          int
	}{
		{
			name:               "primary CR3 edited updates photo metadata and keeps views/tags",
			editPath:           "Trip/IMG.CR3",
			withExtractor:      true,
			wantMetaUpdateCall: 1,
			wantWidth:          7000,
		},
		{
			name:               "non-primary JPG edited updates file only without overwriting CR3 metadata",
			editPath:           "Trip/IMG.jpg",
			withExtractor:      true,
			wantMetaUpdateCall: 0,
			wantWidth:          6000,
		},
		{
			name:               "primary CR3 edited without extractor updates file and cache",
			editPath:           "Trip/IMG.CR3",
			withExtractor:      false,
			wantMetaUpdateCall: 0,
			wantWidth:          6000,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			store := newFakePhotoStore()
			store.seedPhoto(domain.Photo{
				ID: "p1", StackKey: "Trip/photo/img", BaseName: "IMG", FolderPath: "Trip",
				MediaType: domain.MediaTypePhoto, Width: 6000, Height: 4000, ViewCount: 5,
				EmojiTags: []string{"❤", "🏖"}, Status: domain.FileStatusActive,
				EXIF: domain.EXIFMetadata{Orientation: 1},
				Files: []domain.PhotoFile{
					{ID: "f-cr3", PhotoID: "p1", ContentHash: "h-cr3", RelativePath: "Trip/IMG.CR3", FileName: "IMG.CR3", FolderPath: "Trip", Format: domain.FormatCR3, FileSize: 2000, ModTime: now, Status: domain.FileStatusActive},
					{ID: "f-jpg", PhotoID: "p1", ContentHash: "h-jpg", RelativePath: "Trip/IMG.jpg", FileName: "IMG.jpg", FolderPath: "Trip", Format: domain.FormatJPG, FileSize: 500, ModTime: now, Status: domain.FileStatusActive},
				},
			})

			scanned := []domain.ScannedFile{
				{RelativePath: "Trip/IMG.CR3", Format: domain.FormatCR3, FileSize: 2000, ModTime: now},
				{RelativePath: "Trip/IMG.jpg", Format: domain.FormatJPG, FileSize: 500, ModTime: now},
			}
			for i := range scanned {
				if scanned[i].RelativePath == tc.editPath {
					scanned[i].FileSize += 123
					scanned[i].ModTime = later
				}
			}

			scanner := newFakeMediaScanner(scanned, map[string]string{tc.editPath: "h-edited"})
			previews := newFakePreviewExtractor()
			uc := usecase.NewSyncLibraryUseCase(store, scanner, previews).WithClock(testClock(later))
			if tc.withExtractor {
				uc.WithMetadataExtractor(&fakeMetadataExtractor{
					byPath: map[string]domain.MediaMetadata{
						"Trip/IMG.CR3": {Width: 7000, Height: 4500, EXIF: domain.EXIFMetadata{Orientation: 6}},
						"Trip/IMG.jpg": {Width: 1000, Height: 800, EXIF: domain.EXIFMetadata{Orientation: 1}},
					},
				})
			}

			report, err := uc.Sync(context.Background())
			if err != nil {
				t.Fatalf("Sync failed: %v", err)
			}
			if report.UpdatedCount != 1 || report.UnchangedCount != 1 {
				t.Fatalf("report = %+v", report)
			}
			if store.updateMetaCalls != tc.wantMetaUpdateCall {
				t.Fatalf("updateMetaCalls = %d, want %d", store.updateMetaCalls, tc.wantMetaUpdateCall)
			}
			p, _ := store.GetByID(context.Background(), "p1")
			if p.ViewCount != 5 || !slices.Equal(p.EmojiTags, []string{"❤", "🏖"}) || p.Width != tc.wantWidth {
				t.Fatalf("photo after in-place edit = %+v", p)
			}
			if len(previews.ensured) != 1 || previews.ensured[0] != tc.editPath {
				t.Fatalf("previews ensured = %v, want [%s]", previews.ensured, tc.editPath)
			}
		})
	}
}

func TestSyncLibraryUseCase_MovesCyclicSplitAndDuplicates(t *testing.T) {
	now := time.Date(2026, 7, 13, 10, 0, 0, 0, time.UTC)
	later := now.Add(time.Minute)

	t.Run("cyclic 3-way move preserves view counts and tags", func(t *testing.T) {
		store := newFakePhotoStore()
		for _, item := range []struct {
			id, rel, hash string
			views         int
		}{
			{"p1", "Inbox/A.jpg", "hA", 1},
			{"p2", "Inbox/B.jpg", "hB", 2},
			{"p3", "Inbox/C.jpg", "hC", 3},
		} {
			store.seedPhoto(domain.Photo{
				ID: item.id, StackKey: "Inbox/photo/" + item.id, BaseName: item.id, FolderPath: "Inbox",
				MediaType: domain.MediaTypePhoto, ViewCount: item.views, EmojiTags: []string{"⭐"}, Status: domain.FileStatusActive,
				Files: []domain.PhotoFile{{
					ID: "f-" + item.id, PhotoID: item.id, ContentHash: item.hash,
					RelativePath: item.rel, FileName: item.rel[6:], FolderPath: "Inbox",
					Format: domain.FormatJPG, FileSize: 100, ModTime: now, Status: domain.FileStatusActive,
				}},
			})
		}

		// Move A->Archive/B.jpg, B->Archive/C.jpg, C->Archive/A.jpg with updated mtime on A.
		scanned := []domain.ScannedFile{
			{RelativePath: "Archive/B.jpg", Format: domain.FormatJPG, FileSize: 100, ModTime: later},
			{RelativePath: "Archive/C.jpg", Format: domain.FormatJPG, FileSize: 100, ModTime: now},
			{RelativePath: "Archive/A.jpg", Format: domain.FormatJPG, FileSize: 100, ModTime: now},
		}
		scanner := newFakeMediaScanner(scanned, map[string]string{
			"Archive/B.jpg": "hA",
			"Archive/C.jpg": "hB",
			"Archive/A.jpg": "hC",
		})
		uc := usecase.NewSyncLibraryUseCase(store, scanner, nil).WithClock(testClock(later))

		report, err := uc.Sync(context.Background())
		if err != nil {
			t.Fatalf("Sync failed: %v", err)
		}
		if report.MovedCount != 3 || report.MissingCount != 0 || store.moveBatchCalls != 1 {
			t.Fatalf("report=%+v moveBatchCalls=%d", report, store.moveBatchCalls)
		}
		p1, _ := store.GetByID(context.Background(), "p1")
		if p1.ViewCount != 1 || p1.Files[0].RelativePath != "Archive/B.jpg" || !p1.Files[0].ModTime.Equal(later) {
			t.Fatalf("p1 after cyclic move = %+v", p1)
		}
	})

	t.Run("split-stack move vs whole-stack move vs exact duplicate move", func(t *testing.T) {
		store := newFakePhotoStore()
		// p-split: CR3 stays in Trip, JPG moves to Favorites (with changed ModTime).
		store.seedPhoto(domain.Photo{
			ID: "p-split", StackKey: "Trip/photo/shot1", BaseName: "shot1", FolderPath: "Trip",
			MediaType: domain.MediaTypePhoto, ViewCount: 7, EmojiTags: []string{"❤"}, Status: domain.FileStatusActive,
			Files: []domain.PhotoFile{
				{ID: "f-s-cr3", PhotoID: "p-split", ContentHash: "hs-cr3", RelativePath: "Trip/shot1.CR3", FileName: "shot1.CR3", FolderPath: "Trip", Format: domain.FormatCR3, FileSize: 5000, ModTime: now, Status: domain.FileStatusActive},
				{ID: "f-s-jpg", PhotoID: "p-split", ContentHash: "hs-jpg", RelativePath: "Trip/shot1.jpg", FileName: "shot1.jpg", FolderPath: "Trip", Format: domain.FormatJPG, FileSize: 500, ModTime: now, Status: domain.FileStatusActive},
			},
		})
		// p-together: both CR3 and JPG move together to Archive.
		store.seedPhoto(domain.Photo{
			ID: "p-together", StackKey: "Trip/photo/shot2", BaseName: "shot2", FolderPath: "Trip",
			MediaType: domain.MediaTypePhoto, ViewCount: 2, Status: domain.FileStatusActive,
			Files: []domain.PhotoFile{
				{ID: "f-t-cr3", PhotoID: "p-together", ContentHash: "ht-cr3", RelativePath: "Trip/shot2.CR3", FileName: "shot2.CR3", FolderPath: "Trip", Format: domain.FormatCR3, FileSize: 5000, ModTime: now, Status: domain.FileStatusActive},
				{ID: "f-t-jpg", PhotoID: "p-together", ContentHash: "ht-jpg", RelativePath: "Trip/shot2.jpg", FileName: "shot2.jpg", FolderPath: "Trip", Format: domain.FormatJPG, FileSize: 500, ModTime: now, Status: domain.FileStatusActive},
			},
		})
		// p-diverge: CR3 moves to FolderX and JPG moves to FolderY -> one keeps p-diverge, other splits.
		store.seedPhoto(domain.Photo{
			ID: "p-diverge", StackKey: "Trip/photo/shot3", BaseName: "shot3", FolderPath: "Trip",
			MediaType: domain.MediaTypePhoto, ViewCount: 4, Status: domain.FileStatusActive,
			Files: []domain.PhotoFile{
				{ID: "f-d-cr3", PhotoID: "p-diverge", ContentHash: "hd-cr3", RelativePath: "Trip/shot3.CR3", FileName: "shot3.CR3", FolderPath: "Trip", Format: domain.FormatCR3, FileSize: 5000, ModTime: now, Status: domain.FileStatusActive},
				{ID: "f-d-jpg", PhotoID: "p-diverge", ContentHash: "hd-jpg", RelativePath: "Trip/shot3.jpg", FileName: "shot3.jpg", FolderPath: "Trip", Format: domain.FormatJPG, FileSize: 500, ModTime: now, Status: domain.FileStatusActive},
			},
		})
		// p-dup: exact duplicate in Backup moves to Archive2 (same hash -> never splits).
		store.seedPhoto(domain.Photo{
			ID: "p-dup", StackKey: "Trip/photo/dup", BaseName: "dup", FolderPath: "Trip",
			MediaType: domain.MediaTypePhoto, ViewCount: 9, Status: domain.FileStatusActive,
			Files: []domain.PhotoFile{
				{ID: "f-dup-1", PhotoID: "p-dup", ContentHash: "hdup", RelativePath: "Trip/dup.jpg", FileName: "dup.jpg", FolderPath: "Trip", Format: domain.FormatJPG, FileSize: 400, ModTime: now, Status: domain.FileStatusActive},
				{ID: "f-dup-2", PhotoID: "p-dup", ContentHash: "hdup", RelativePath: "Backup/dup.jpg", FileName: "dup.jpg", FolderPath: "Backup", Format: domain.FormatJPG, FileSize: 400, ModTime: now, Status: domain.FileStatusActive},
			},
		})
		// p-delmove: CR3 deleted, JPG moves to Archive3 -> does NOT split.
		store.seedPhoto(domain.Photo{
			ID: "p-delmove", StackKey: "Trip/photo/shot4", BaseName: "shot4", FolderPath: "Trip",
			MediaType: domain.MediaTypePhoto, ViewCount: 6, Status: domain.FileStatusActive,
			Files: []domain.PhotoFile{
				{ID: "f-dm-cr3", PhotoID: "p-delmove", ContentHash: "hdm-cr3", RelativePath: "Trip/shot4.CR3", FileName: "shot4.CR3", FolderPath: "Trip", Format: domain.FormatCR3, FileSize: 5000, ModTime: now, Status: domain.FileStatusActive},
				{ID: "f-dm-jpg", PhotoID: "p-delmove", ContentHash: "hdm-jpg", RelativePath: "Trip/shot4.jpg", FileName: "shot4.jpg", FolderPath: "Trip", Format: domain.FormatJPG, FileSize: 500, ModTime: now, Status: domain.FileStatusActive},
			},
		})
		// p-wasmissing: previously missing file reappears at new path + gets duplicated.
		store.seedPhoto(domain.Photo{
			ID: "p-wasmissing", StackKey: "Old/photo/lost", BaseName: "lost", FolderPath: "Old",
			MediaType: domain.MediaTypePhoto, ViewCount: 8, Status: domain.FileStatusMissing,
			Files: []domain.PhotoFile{
				{ID: "f-lost", PhotoID: "p-wasmissing", ContentHash: "hlost", RelativePath: "Old/lost.jpg", FileName: "lost.jpg", FolderPath: "Old", Format: domain.FormatJPG, FileSize: 300, ModTime: now, Status: domain.FileStatusMissing},
			},
		})

		scanned := []domain.ScannedFile{
			{RelativePath: "Trip/shot1.CR3", Format: domain.FormatCR3, FileSize: 5000, ModTime: now},
			{RelativePath: "Favorites/shot1.jpg", Format: domain.FormatJPG, FileSize: 500, ModTime: later},
			{RelativePath: "Archive/shot2.CR3", Format: domain.FormatCR3, FileSize: 5000, ModTime: now},
			{RelativePath: "Archive/shot2.jpg", Format: domain.FormatJPG, FileSize: 500, ModTime: now},
			{RelativePath: "FolderX/shot3.CR3", Format: domain.FormatCR3, FileSize: 5000, ModTime: now},
			{RelativePath: "FolderY/shot3.jpg", Format: domain.FormatJPG, FileSize: 500, ModTime: now},
			{RelativePath: "Trip/dup.jpg", Format: domain.FormatJPG, FileSize: 400, ModTime: now},
			{RelativePath: "Archive2/dup.jpg", Format: domain.FormatJPG, FileSize: 400, ModTime: now},
			{RelativePath: "Archive3/shot4.jpg", Format: domain.FormatJPG, FileSize: 500, ModTime: now},
			{RelativePath: "Found/lost.jpg", Format: domain.FormatJPG, FileSize: 300, ModTime: now},
			{RelativePath: "FoundCopy/lost.jpg", Format: domain.FormatJPG, FileSize: 300, ModTime: now},
		}
		scanner := newFakeMediaScanner(scanned, map[string]string{
			"Favorites/shot1.jpg": "hs-jpg",
			"Archive/shot2.CR3":   "ht-cr3",
			"Archive/shot2.jpg":   "ht-jpg",
			"FolderX/shot3.CR3":   "hd-cr3",
			"FolderY/shot3.jpg":   "hd-jpg",
			"Archive2/dup.jpg":    "hdup",
			"Archive3/shot4.jpg":  "hdm-jpg",
			"Found/lost.jpg":      "hlost",
			"FoundCopy/lost.jpg":  "hlost",
		})
		uc := usecase.NewSyncLibraryUseCase(store, scanner, newFakePreviewExtractor()).
			WithClock(testClock(later)).
			WithIDGenerator(testSeqIDs())

		report, err := uc.Sync(context.Background())
		if err != nil {
			t.Fatalf("Sync failed: %v", err)
		}
		if report.MovedCount != 8 || report.DuplicateCount != 1 || report.MissingCount != 1 || store.splitCalls != 2 {
			t.Fatalf("unexpected report=%+v splitCalls=%d", report, store.splitCalls)
		}

		origSplit, _ := store.GetByID(context.Background(), "p-split")
		if len(origSplit.Files) != 1 || origSplit.Files[0].ID != "f-s-cr3" || origSplit.ViewCount != 7 {
			t.Fatalf("origSplit unexpected: %+v", origSplit)
		}
		newSplit, err := store.GetByStackKey(context.Background(), "Favorites/photo/shot1")
		if err != nil || newSplit.ViewCount != 7 || !slices.Equal(newSplit.EmojiTags, []string{"❤"}) || !newSplit.Files[0].ModTime.Equal(later) {
			t.Fatalf("newSplit unexpected: (%+v, %v)", newSplit, err)
		}
		together, _ := store.GetByID(context.Background(), "p-together")
		if len(together.Files) != 2 || together.FolderPath != "Archive" {
			t.Fatalf("p-together should stay stacked in Archive: %+v", together)
		}
		wasMissing, _ := store.GetByID(context.Background(), "p-wasmissing")
		if wasMissing.Status != domain.FileStatusActive || len(wasMissing.Files) != 2 {
			t.Fatalf("p-wasmissing should be reactivated with duplicate: %+v", wasMissing)
		}
	})

	t.Run("move candidates prefer active before missing and sort deterministically", func(t *testing.T) {
		store := newFakePhotoStore()
		store.seedPhoto(domain.Photo{
			ID: "p-cand", StackKey: "A/photo/x", BaseName: "x", FolderPath: "A", MediaType: domain.MediaTypePhoto,
			Status: domain.FileStatusActive,
			Files: []domain.PhotoFile{
				{ID: "f-miss", PhotoID: "p-cand", ContentHash: "h-same", RelativePath: "A/01_miss.jpg", FileName: "01_miss.jpg", FolderPath: "A", Format: domain.FormatJPG, FileSize: 100, ModTime: now, Status: domain.FileStatusMissing},
				{ID: "f-act-b", PhotoID: "p-cand", ContentHash: "h-same", RelativePath: "B/02_act.jpg", FileName: "02_act.jpg", FolderPath: "B", Format: domain.FormatJPG, FileSize: 100, ModTime: now, Status: domain.FileStatusActive},
				{ID: "f-act-a", PhotoID: "p-cand", ContentHash: "h-same", RelativePath: "A/03_act.jpg", FileName: "03_act.jpg", FolderPath: "A", Format: domain.FormatJPG, FileSize: 100, ModTime: now, Status: domain.FileStatusActive},
			},
		})
		scanner := newFakeMediaScanner([]domain.ScannedFile{
			{RelativePath: "New/01.jpg", Format: domain.FormatJPG, FileSize: 100, ModTime: now},
		}, map[string]string{"New/01.jpg": "h-same"})
		uc := usecase.NewSyncLibraryUseCase(store, scanner, nil).WithClock(testClock(now))

		rep, err := uc.Sync(context.Background())
		if err != nil {
			t.Fatalf("Sync failed: %v", err)
		}
		// f-act-a should be chosen as the move (active before missing, A/ before B/), and f-act-b marked missing.
		if rep.MovedCount != 1 || rep.MissingCount != 1 {
			t.Fatalf("rep = %+v, want MovedCount=1 MissingCount=1", rep)
		}
		got, _ := store.GetByID(context.Background(), "p-cand")
		var activeRel string
		for _, f := range got.Files {
			if f.Status == domain.FileStatusActive {
				activeRel = f.RelativePath
			}
		}
		if activeRel != "New/01.jpg" {
			t.Fatalf("expected New/01.jpg active, got %+v", got.Files)
		}
	})
}

func TestSyncLibraryUseCase_MarkMissingPurgeStatusAndPreflight(t *testing.T) {
	now := time.Date(2026, 7, 14, 15, 0, 0, 0, time.UTC)

	t.Run("mark missing skips already-missing and purge creates backup", func(t *testing.T) {
		store := newFakePhotoStore()
		store.seedPhoto(domain.Photo{
			ID: "p-active", StackKey: "A/photo/1", BaseName: "1", FolderPath: "A", MediaType: domain.MediaTypePhoto,
			Status: domain.FileStatusActive,
			Files: []domain.PhotoFile{
				{ID: "f-keep", PhotoID: "p-active", ContentHash: "hk", RelativePath: "A/keep.jpg", FileName: "keep.jpg", FolderPath: "A", Format: domain.FormatJPG, FileSize: 10, ModTime: now, Status: domain.FileStatusActive},
				{ID: "f-gone", PhotoID: "p-active", ContentHash: "hg", RelativePath: "A/gone.jpg", FileName: "gone.jpg", FolderPath: "A", Format: domain.FormatJPG, FileSize: 10, ModTime: now, Status: domain.FileStatusActive},
			},
		})
		store.seedPhoto(domain.Photo{
			ID: "p-oldmiss", StackKey: "A/photo/old", BaseName: "old", FolderPath: "A", MediaType: domain.MediaTypePhoto,
			Status: domain.FileStatusMissing,
			Files: []domain.PhotoFile{
				{ID: "f-oldmiss", PhotoID: "p-oldmiss", ContentHash: "ho", RelativePath: "A/old.jpg", FileName: "old.jpg", FolderPath: "A", Format: domain.FormatJPG, FileSize: 10, ModTime: now, Status: domain.FileStatusMissing},
			},
		})

		scanner := newFakeMediaScanner([]domain.ScannedFile{
			{RelativePath: "A/keep.jpg", Format: domain.FormatJPG, FileSize: 10, ModTime: now},
		}, nil)
		runs := newFakeSyncRunStore()
		maintainer := &fakeMaintainer{}
		uc := usecase.NewSyncLibraryUseCase(store, scanner, nil).
			WithSyncRunStore(runs).
			WithDatabaseMaintainer(maintainer).
			WithClock(func() time.Time { return now })

		report, err := uc.Sync(context.Background())
		if err != nil {
			t.Fatalf("Sync failed: %v", err)
		}
		if report.MissingCount != 1 {
			t.Fatalf("MissingCount = %d, want 1", report.MissingCount)
		}

		// Sync again: f-gone is already missing so MissingCount must be 0.
		report2, err := uc.Sync(context.Background())
		if err != nil || report2.MissingCount != 0 {
			t.Fatalf("second Sync = (%+v, %v), want MissingCount=0", report2, err)
		}

		purged, err := uc.PurgeMissing(context.Background())
		if err != nil {
			t.Fatalf("PurgeMissing failed: %v", err)
		}
		if purged != 3 || len(maintainer.backupSuffixes) != 1 || maintainer.backupSuffixes[0] != fmt.Sprintf("pre_purge_%d", now.Unix()) {
			t.Fatalf("purged=%d suffixes=%v", purged, maintainer.backupSuffixes)
		}

		latest, err := uc.Status(context.Background())
		if err != nil || latest.ID != report2.ID {
			t.Fatalf("Status = (%+v, %v)", latest, err)
		}
	})

	t.Run("preflight abort leaves active files intact and marks run failed", func(t *testing.T) {
		for _, tc := range []struct {
			name    string
			walkErr error
		}{
			{name: "walk error", walkErr: errors.New("permission denied")},
			{name: "empty scan with active db records", walkErr: nil},
		} {
			t.Run(tc.name, func(t *testing.T) {
				store := newFakePhotoStore()
				store.seedPhoto(domain.Photo{
					ID: "p1", Status: domain.FileStatusActive,
					Files: []domain.PhotoFile{{ID: "f1", PhotoID: "p1", RelativePath: "A/1.jpg", Status: domain.FileStatusActive}},
				})
				scanner := newFakeMediaScanner(nil, nil)
				scanner.walkErr = tc.walkErr
				runs := newFakeSyncRunStore()

				uc := usecase.NewSyncLibraryUseCase(store, scanner, nil).WithSyncRunStore(runs)
				rep, err := uc.Sync(context.Background())
				if !errors.Is(err, domain.ErrLibraryUnavailable) {
					t.Fatalf("err = %v, want ErrLibraryUnavailable", err)
				}
				if tc.walkErr != nil && !errors.Is(err, tc.walkErr) {
					t.Fatalf("err = %v, want wrapped walkErr", err)
				}
				if rep.Status != domain.SyncStatusFailed || store.markMissingCalled != 0 {
					t.Fatalf("rep=%+v markMissingCalled=%d", rep, store.markMissingCalled)
				}
				latest, _ := uc.Status(context.Background())
				if latest.Status != domain.SyncStatusFailed || latest.ErrorMessage == "" {
					t.Fatalf("latest run = %+v", latest)
				}
			})
		}
	})
}

func TestSyncLibraryUseCase_ConcurrencyStatusRecoverAndNilDefaults(t *testing.T) {
	now := time.Date(2026, 7, 15, 9, 0, 0, 0, time.UTC)

	t.Run("concurrent sync returns ErrSyncInProgress", func(t *testing.T) {
		store := newFakePhotoStore()
		started := make(chan struct{})
		release := make(chan struct{})
		scanner := newFakeMediaScanner(nil, nil)
		scanner.walkHook = func(context.Context) {
			close(started)
			<-release
		}
		uc := usecase.NewSyncLibraryUseCase(store, scanner, nil).
			WithClock(nil).
			WithIDGenerator(nil)

		errCh := make(chan error, 1)
		go func() {
			_, err := uc.Sync(context.Background())
			errCh <- err
		}()

		<-started
		if _, err := uc.Sync(context.Background()); !errors.Is(err, domain.ErrSyncInProgress) {
			t.Fatalf("concurrent Sync err = %v, want ErrSyncInProgress", err)
		}
		close(release)
		if err := <-errCh; err != nil {
			t.Fatalf("first Sync failed: %v", err)
		}
	})

	t.Run("nil runs Status and RecoverStaleRuns and default UUIDv4", func(t *testing.T) {
		store := newFakePhotoStore()
		scanner := newFakeMediaScanner(nil, nil)
		uc := usecase.NewSyncLibraryUseCase(store, scanner, nil)

		if _, err := uc.Status(context.Background()); !errors.Is(err, domain.ErrSyncNotFound) {
			t.Fatalf("Status err = %v, want ErrSyncNotFound", err)
		}
		if n, err := uc.RecoverStaleRuns(context.Background()); err != nil || n != 0 {
			t.Fatalf("RecoverStaleRuns nil = (%d, %v)", n, err)
		}
		rep, err := uc.Sync(context.Background())
		if err != nil || len(rep.ID) != 36 || strings.Count(rep.ID, "-") != 4 {
			t.Fatalf("expected UUIDv4 report ID, got (%+v, %v)", rep, err)
		}
	})

	t.Run("RecoverStaleRuns marks running jobs failed", func(t *testing.T) {
		runs := newFakeSyncRunStore()
		_ = runs.CreateSyncRun(context.Background(), domain.SyncReport{ID: "stale-1", Status: domain.SyncStatusRunning, StartedAt: now})
		uc := usecase.NewSyncLibraryUseCase(newFakePhotoStore(), newFakeMediaScanner(nil, nil), nil).
			WithSyncRunStore(runs).
			WithClock(func() time.Time { return now })

		n, err := uc.RecoverStaleRuns(context.Background())
		if err != nil || n != 1 || runs.lastReason != "interrupted by server restart" {
			t.Fatalf("RecoverStaleRuns = (%d, %v), reason=%q", n, err, runs.lastReason)
		}
	})

	t.Run("missing photo does not absorb new stack key", func(t *testing.T) {
		store := newFakePhotoStore()
		store.seedPhoto(domain.Photo{
			ID: "p-miss", StackKey: "Trip/photo/img", BaseName: "IMG", FolderPath: "Trip",
			MediaType: domain.MediaTypePhoto, Status: domain.FileStatusMissing,
			Files: []domain.PhotoFile{
				{ID: "f-miss", PhotoID: "p-miss", ContentHash: "h-old", RelativePath: "Trip/IMG.CR3", FileName: "IMG.CR3", FolderPath: "Trip", Format: domain.FormatCR3, FileSize: 100, ModTime: now, Status: domain.FileStatusMissing},
			},
		})
		scanner := newFakeMediaScanner([]domain.ScannedFile{
			{RelativePath: "Trip/IMG.jpg", Format: domain.FormatJPG, FileSize: 50, ModTime: now},
		}, map[string]string{"Trip/IMG.jpg": "h-new"})
		uc := usecase.NewSyncLibraryUseCase(store, scanner, nil).WithIDGenerator(testSeqIDs())

		rep, err := uc.Sync(context.Background())
		if err != nil || rep.AddedCount != 1 || rep.StackedCount != 0 {
			t.Fatalf("expected brand-new photo instead of stacking onto missing photo: (%+v, %v)", rep, err)
		}
	})
}

func TestSyncLibraryUseCase_FiveFileIncrementalWorkflow(t *testing.T) {
	now := time.Date(2026, 7, 16, 10, 0, 0, 0, time.UTC)
	later := now.Add(time.Hour)

	store := newFakePhotoStore()
	initialFiles := []domain.ScannedFile{
		{RelativePath: "IMG_001.CR3", Format: domain.FormatCR3, FileSize: 5000, ModTime: now},
		{RelativePath: "IMG_002.JPG", Format: domain.FormatJPG, FileSize: 1000, ModTime: now},
		{RelativePath: "IMG_003.JPG", Format: domain.FormatJPG, FileSize: 1200, ModTime: now},
		{RelativePath: "IMG_004.JPG", Format: domain.FormatJPG, FileSize: 1100, ModTime: now},
		{RelativePath: "clip.mp4", Format: domain.FormatMP4, FileSize: 8000, ModTime: now},
	}
	hashes := map[string]string{
		"IMG_001.CR3": "h-001",
		"IMG_002.JPG": "h-002",
		"IMG_003.JPG": "h-003",
		"IMG_004.JPG": "h-004",
		"clip.mp4":    "h-clip",
	}
	scanner := newFakeMediaScanner(initialFiles, hashes)
	previews := newFakePreviewExtractor()
	uc := usecase.NewSyncLibraryUseCase(store, scanner, previews).
		WithClock(testClock(now)).
		WithIDGenerator(testSeqIDs())

	rep1, err := uc.Sync(context.Background())
	if err != nil || rep1.AddedCount != 5 {
		t.Fatalf("initial Sync = (%+v, %v), want AddedCount=5", rep1, err)
	}

	// Simulate IMG_001.CR3 viewed 4 times and tagged.
	var p1ID, p3ID string
	for _, f := range store.files {
		switch f.RelativePath {
		case "IMG_001.CR3":
			p1ID = f.PhotoID
		case "IMG_003.JPG":
			p3ID = f.PhotoID
		}
	}
	p1 := store.photos[p1ID]
	p1.ViewCount = 4
	p1.EmojiTags = []string{"❤"}
	store.photos[p1ID] = p1

	// Edit IMG_003.JPG in place, rename IMG_001.CR3 -> subfolder/RENAMED.CR3, delete IMG_002.JPG.
	scanner.walkFiles = []domain.ScannedFile{
		{RelativePath: "subfolder/RENAMED.CR3", Format: domain.FormatCR3, FileSize: 5000, ModTime: now},
		{RelativePath: "IMG_003.JPG", Format: domain.FormatJPG, FileSize: 1350, ModTime: later},
		{RelativePath: "IMG_004.JPG", Format: domain.FormatJPG, FileSize: 1100, ModTime: now},
		{RelativePath: "clip.mp4", Format: domain.FormatMP4, FileSize: 8000, ModTime: now},
	}
	scanner.hashesByRel = map[string]string{
		"subfolder/RENAMED.CR3": "h-001",
		"IMG_003.JPG":           "h-003-edited",
	}
	scanner.hashCalls = 0
	scanner.hashedInput = nil

	rep2, err := uc.Sync(context.Background())
	if err != nil {
		t.Fatalf("incremental Sync failed: %v", err)
	}
	if rep2.ScannedCount != 4 || rep2.UnchangedCount != 2 || rep2.UpdatedCount != 1 || rep2.MovedCount != 1 || rep2.MissingCount != 1 {
		t.Fatalf("unexpected incremental report: %+v", rep2)
	}
	if scanner.hashCalls != 1 || len(scanner.hashedInput) != 2 {
		t.Fatalf("expected exactly 2 files hashed (0 untouched re-hashed), got calls=%d files=%+v", scanner.hashCalls, scanner.hashedInput)
	}

	got1, _ := store.GetByID(context.Background(), p1ID)
	if got1.ViewCount != 4 || !slices.Equal(got1.EmojiTags, []string{"❤"}) || got1.Files[0].RelativePath != "subfolder/RENAMED.CR3" {
		t.Fatalf("moved IMG_001.CR3 did not preserve views/tags/path: %+v", got1)
	}
	got3, _ := store.GetByID(context.Background(), p3ID)
	if got3.ID != p3ID || got3.Files[0].ContentHash != "h-003-edited" {
		t.Fatalf("edited IMG_003.JPG did not preserve PhotoID/new hash: %+v", got3)
	}
	for _, f := range store.files {
		if f.RelativePath == "IMG_002.JPG" && f.Status != domain.FileStatusMissing {
			t.Fatalf("deleted IMG_002.JPG status = %q, want missing", f.Status)
		}
	}
}

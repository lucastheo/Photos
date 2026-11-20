package sqlite_test

import (
	"context"
	"errors"
	"math"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/lucastheo/photos/internal/adapter/sqlite"
	"github.com/lucastheo/photos/internal/domain"
)

func intPtr(v int) *int              { return &v }
func floatPtr(v float64) *float64    { return &v }
func boolPtr(v bool) *bool           { return &v }
func timePtr(v time.Time) *time.Time { return &v }

func makeSamplePhoto(id, folder, base string, mt domain.MediaType, files []domain.PhotoFile, now time.Time) domain.Photo {
	var fmtHint domain.MediaFormat
	if len(files) > 0 {
		fmtHint = files[0].Format
	}
	return domain.Photo{
		ID:         id,
		StackKey:   domain.ComputeStackKey(folder, mt, base, fmtHint),
		BaseName:   base,
		FolderPath: folder,
		MediaType:  mt,
		Files:      files,
		Status:     domain.FileStatusActive,
		CreatedAt:  now,
		UpdatedAt:  now,
	}
}

func TestPhotoRepository_Integration_CreateStackAndDuplicates(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	ctx := context.Background()
	db := openTestDB(t, filepath.Join(t.TempDir(), "photos.db"))
	repo := sqlite.NewPhotoRepository(db.SQL())
	now := time.Date(2026, 5, 1, 12, 0, 0, 0, time.UTC)

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

	got, err := repo.GetByStackKey(ctx, "2026/Trip/photo/img_0042")
	if err != nil {
		t.Fatalf("GetByStackKey failed: %v", err)
	}
	if len(got.Files) != 2 || got.Files[0].Format != domain.FormatCR3 || got.Files[1].Format != domain.FormatJPG {
		t.Fatalf("expected CR3 before JPG, got %+v", got.Files)
	}
	if !slices.Equal(got.Formats, []domain.MediaFormat{domain.FormatCR3, domain.FormatJPG}) {
		t.Fatalf("Formats = %v, want [cr3 jpg]", got.Formats)
	}

	// Attach an exact duplicate of the JPG in another folder.
	if err := repo.AttachFile(ctx, "p-stack", domain.PhotoFile{
		ID: "f-dup", ContentHash: "h-jpg", RelativePath: "Backup/IMG_0042.jpg",
		FileName: "IMG_0042.jpg", FolderPath: "Backup", Format: domain.FormatJPG,
		FileSize: 1200, ModTime: now,
	}); err != nil {
		t.Fatalf("AttachFile duplicate failed: %v", err)
	}

	byHash, err := repo.FindFilesByContentHash(ctx, "h-jpg")
	if err != nil || len(byHash) != 2 {
		t.Fatalf("FindFilesByContentHash = (%d, %v), want 2", len(byHash), err)
	}
	allFiles, err := repo.ListAllFiles(ctx)
	if err != nil || len(allFiles) != 3 {
		t.Fatalf("ListAllFiles = (%d, %v), want 3", len(allFiles), err)
	}

	dups, err := repo.ListDuplicates(ctx)
	if err != nil || len(dups) != 1 || dups[0].FileCount != 2 || dups[0].ContentHash != "h-jpg" {
		t.Fatalf("ListDuplicates = (%+v, %v)", dups, err)
	}

	if err := repo.AttachFile(ctx, "unknown", domain.PhotoFile{ID: "x"}); !errors.Is(err, domain.ErrPhotoNotFound) {
		t.Fatalf("AttachFile unknown photo err = %v, want ErrPhotoNotFound", err)
	}
	if _, err := repo.GetByID(ctx, "unknown"); !errors.Is(err, domain.ErrPhotoNotFound) {
		t.Fatalf("GetByID unknown err = %v, want ErrPhotoNotFound", err)
	}
	if _, err := repo.GetByStackKey(ctx, "unknown"); !errors.Is(err, domain.ErrPhotoNotFound) {
		t.Fatalf("GetByStackKey unknown err = %v, want ErrPhotoNotFound", err)
	}
}

func TestPhotoRepository_Integration_InPlaceEditAndMoves(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	ctx := context.Background()
	db := openTestDB(t, filepath.Join(t.TempDir(), "photos.db"))
	repo := sqlite.NewPhotoRepository(db.SQL())
	viewRepo := sqlite.NewViewRepository(db.SQL())
	now := time.Date(2026, 5, 2, 10, 0, 0, 0, time.UTC)

	for _, tc := range []struct {
		id, folder, file, hash string
	}{
		{"p1", "Inbox", "A.jpg", "ha"},
		{"p2", "Inbox", "B.jpg", "hb"},
		{"p3", "Inbox", "C.jpg", "hc"},
	} {
		err := repo.CreatePhoto(ctx, makeSamplePhoto(tc.id, tc.folder, tc.file[:1], domain.MediaTypePhoto, []domain.PhotoFile{
			{
				ID: "f-" + tc.id, ContentHash: tc.hash, RelativePath: tc.folder + "/" + tc.file,
				FileName: tc.file, FolderPath: tc.folder, Format: domain.FormatJPG,
				FileSize: 500, ModTime: now, Status: domain.FileStatusActive,
			},
		}, now))
		if err != nil {
			t.Fatalf("CreatePhoto %s failed: %v", tc.id, err)
		}
	}
	if _, err := viewRepo.AdjustViewCount(ctx, "p1", domain.ViewActionIncrement, now); err != nil {
		t.Fatalf("AdjustViewCount failed: %v", err)
	}
	if err := repo.AddTag(ctx, "p1", "❤️", now); err != nil {
		t.Fatalf("AddTag failed: %v", err)
	}

	// In-place edit updates hash/size/mtime while keeping photo_id, views, and tags.
	later := now.Add(time.Hour)
	if err := repo.UpdateFileInPlace(ctx, domain.PhotoFile{
		RelativePath: "Inbox/A.jpg",
		ContentHash:  "ha-v2",
		FileSize:     777,
		ModTime:      later,
	}); err != nil {
		t.Fatalf("UpdateFileInPlace failed: %v", err)
	}
	p1, err := repo.GetByID(ctx, "p1")
	if err != nil || p1.ViewCount != 1 || !slices.Equal(p1.EmojiTags, []string{"\u2764"}) || p1.Files[0].ContentHash != "ha-v2" {
		t.Fatalf("after in-place edit p1 = %+v, err=%v", p1, err)
	}
	if err := repo.UpdateFileInPlace(ctx, domain.PhotoFile{ID: "nonexistent"}); !errors.Is(err, domain.ErrFileNotFound) {
		t.Fatalf("UpdateFileInPlace unknown err = %v, want ErrFileNotFound", err)
	}

	// Cyclic 3-way rename chain: A->B, B->C, C->A in Archive/.
	err = repo.MoveFilesBatch(ctx, []sqlite.FileMove{
		{FileID: "f-p1", RelativePath: "Archive/B.jpg", FileName: "B.jpg", FolderPath: "Archive"},
		{FileID: "f-p2", RelativePath: "Archive/C.jpg", FileName: "C.jpg", FolderPath: "Archive"},
		{FileID: "f-p3", RelativePath: "Archive/A.jpg", FileName: "A.jpg", FolderPath: "Archive"},
	}, later)
	if err != nil {
		t.Fatalf("MoveFilesBatch cyclic failed: %v", err)
	}

	p1Moved, _ := repo.GetByID(ctx, "p1")
	if p1Moved.FolderPath != "Archive" || p1Moved.BaseName != "B" || p1Moved.StackKey != "Archive/photo/b" || p1Moved.ViewCount != 1 {
		t.Fatalf("unexpected moved parent metadata: %+v", p1Moved)
	}

	// Unknown file move fails and rolls back batch.
	err = repo.MoveFilesBatch(ctx, []sqlite.FileMove{
		{FileID: "f-p1", RelativePath: "Elsewhere/B.jpg", FolderPath: "Elsewhere"},
		{FileID: "f-missing", RelativePath: "Elsewhere/X.jpg", FolderPath: "Elsewhere"},
	}, later)
	if !errors.Is(err, domain.ErrFileNotFound) {
		t.Fatalf("expected ErrFileNotFound, got %v", err)
	}
	p1AfterFail, _ := repo.GetByID(ctx, "p1")
	if p1AfterFail.Files[0].RelativePath != "Archive/B.jpg" {
		t.Fatalf("expected rollback on failed batch, got %q", p1AfterFail.Files[0].RelativePath)
	}
}

func TestPhotoRepository_Integration_MarkMissingAndPurge(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	ctx := context.Background()
	db := openTestDB(t, filepath.Join(t.TempDir(), "photos.db"))
	repo := sqlite.NewPhotoRepository(db.SQL())
	now := time.Date(2026, 5, 3, 9, 0, 0, 0, time.UTC)

	// Photo 1 has CR3 + JPG (partial missing test).
	if err := repo.CreatePhoto(ctx, makeSamplePhoto("p-partial", "Trip", "IMG_1", domain.MediaTypePhoto, []domain.PhotoFile{
		{ID: "f1-cr3", ContentHash: "h1", RelativePath: "Trip/IMG_1.CR3", FileName: "IMG_1.CR3", FolderPath: "Trip", Format: domain.FormatCR3, FileSize: 100, ModTime: now},
		{ID: "f1-jpg", ContentHash: "h2", RelativePath: "Trip/IMG_1.jpg", FileName: "IMG_1.jpg", FolderPath: "Trip", Format: domain.FormatJPG, FileSize: 50, ModTime: now},
	}, now)); err != nil {
		t.Fatalf("CreatePhoto p-partial failed: %v", err)
	}

	// Photo 2 has 1 file (full missing test).
	if err := repo.CreatePhoto(ctx, makeSamplePhoto("p-full", "Trip", "IMG_2", domain.MediaTypePhoto, []domain.PhotoFile{
		{ID: "f2-jpg", ContentHash: "h3", RelativePath: "Trip/IMG_2.jpg", FileName: "IMG_2.jpg", FolderPath: "Trip", Format: domain.FormatJPG, FileSize: 50, ModTime: now},
	}, now)); err != nil {
		t.Fatalf("CreatePhoto p-full failed: %v", err)
	}

	if err := repo.MarkFilesMissing(ctx, []string{"f1-jpg", "f2-jpg"}, now); err != nil {
		t.Fatalf("MarkFilesMissing failed: %v", err)
	}

	partial, _ := repo.GetByID(ctx, "p-partial")
	full, _ := repo.GetByID(ctx, "p-full")
	if partial.Status != domain.FileStatusActive || full.Status != domain.FileStatusMissing {
		t.Fatalf("statuses: partial=%q full=%q, want active/missing", partial.Status, full.Status)
	}

	missingList, err := repo.ListMissing(ctx)
	if err != nil || len(missingList) != 2 {
		t.Fatalf("ListMissing = (%d, %v), want 2", len(missingList), err)
	}

	deleted, err := repo.PurgeMissing(ctx)
	if err != nil || deleted != 3 { // 2 missing photo_files + 1 orphaned photo (p-full)
		t.Fatalf("PurgeMissing = (%d, %v), want 3", deleted, err)
	}

	partialAfter, err := repo.GetByID(ctx, "p-partial")
	if err != nil || len(partialAfter.Files) != 1 || partialAfter.Files[0].ID != "f1-cr3" {
		t.Fatalf("partialAfter = (%+v, %v)", partialAfter, err)
	}
	if _, err := repo.GetByID(ctx, "p-full"); !errors.Is(err, domain.ErrPhotoNotFound) {
		t.Fatalf("expected p-full to be purged, err=%v", err)
	}
}

func TestPhotoRepository_Integration_TagsAndSearch(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	ctx := context.Background()
	db := openTestDB(t, filepath.Join(t.TempDir(), "photos.db"))
	repo := sqlite.NewPhotoRepository(db.SQL())
	viewRepo := sqlite.NewViewRepository(db.SQL())

	t1 := time.Date(2025, 6, 1, 10, 0, 0, 0, time.UTC)
	t2 := time.Date(2025, 7, 1, 10, 0, 0, 0, time.UTC)
	t3 := time.Date(2025, 8, 1, 10, 0, 0, 0, time.UTC)

	seed := []domain.Photo{
		{
			ID: "p1", StackKey: "2025/photo/beach_100%", BaseName: "beach_100%", FolderPath: "2025",
			MediaType: domain.MediaTypePhoto, TakenAt: timePtr(t1),
			EXIF: domain.EXIFMetadata{Orientation: 1, GPSLatitude: floatPtr(10.0), GPSLongitude: floatPtr(20.0)},
			Files: []domain.PhotoFile{
				{ID: "f1", ContentHash: "h1", RelativePath: "2025/beach_100%.CR3", FileName: "beach_100%.CR3", FolderPath: "2025", Format: domain.FormatCR3, FileSize: 10, ModTime: t1},
			},
			EmojiTags: []string{"❤️", "🏖️"},
			CreatedAt: t1, UpdatedAt: t1,
		},
		{
			ID: "p2", StackKey: "2025/sub/photo/sunset", BaseName: "sunset", FolderPath: "2025/sub",
			MediaType: domain.MediaTypePhoto, TakenAt: timePtr(t2),
			EXIF: domain.EXIFMetadata{Orientation: 1, GPSLatitude: floatPtr(45.0), GPSLongitude: floatPtr(-70.0)},
			Files: []domain.PhotoFile{
				{ID: "f2", ContentHash: "h2", RelativePath: "2025/sub/sunset.jpg", FileName: "sunset.jpg", FolderPath: "2025/sub", Format: domain.FormatJPG, FileSize: 10, ModTime: t2},
			},
			EmojiTags: []string{"❤️"},
			CreatedAt: t2, UpdatedAt: t2,
		},
		{
			ID: "p3", StackKey: "20250/video/clip.mp4", BaseName: "clip", FolderPath: "20250",
			MediaType: domain.MediaTypeVideo, DurationSec: 12.5, TakenAt: timePtr(t3),
			Files: []domain.PhotoFile{
				{ID: "f3", ContentHash: "h3", RelativePath: "20250/clip.mp4", FileName: "clip.mp4", FolderPath: "20250", Format: domain.FormatMP4, FileSize: 10, ModTime: t3},
			},
			CreatedAt: t3, UpdatedAt: t3,
		},
	}
	for _, p := range seed {
		if err := repo.CreatePhoto(ctx, p); err != nil {
			t.Fatalf("seed CreatePhoto %s failed: %v", p.ID, err)
		}
	}

	if _, err := viewRepo.AdjustViewCount(ctx, "p1", domain.ViewActionIncrement, t3); err != nil {
		t.Fatalf("increment p1 failed: %v", err)
	}
	if _, err := viewRepo.AdjustViewCount(ctx, "p2", domain.ViewActionIncrement, t3.Add(time.Minute)); err != nil {
		t.Fatalf("increment p2 failed: %v", err)
	}
	if _, err := viewRepo.AdjustViewCount(ctx, "p2", domain.ViewActionIncrement, t3.Add(2*time.Minute)); err != nil {
		t.Fatalf("increment p2 second time failed: %v", err)
	}

	// Tag operations & normalization.
	if err := repo.AddTag(ctx, "p3", "bad-text", t3); !errors.Is(err, domain.ErrInvalidEmojiTag) {
		t.Fatalf("AddTag invalid err = %v, want ErrInvalidEmojiTag", err)
	}
	if err := repo.AddTag(ctx, "missing-id", "❤️", t3); !errors.Is(err, domain.ErrPhotoNotFound) {
		t.Fatalf("AddTag missing photo err = %v, want ErrPhotoNotFound", err)
	}
	if err := repo.AddTag(ctx, "p3", "⭐", t3); err != nil {
		t.Fatalf("AddTag p3 failed: %v", err)
	}
	if err := repo.RemoveTag(ctx, "p3", "⭐"); err != nil {
		t.Fatalf("RemoveTag p3 failed: %v", err)
	}
	tags, err := repo.ListTags(ctx)
	if err != nil || len(tags) != 2 || tags[0].Emoji != "\u2764" || tags[0].Count != 2 {
		t.Fatalf("ListTags = (%+v, %v)", tags, err)
	}

	cases := []struct {
		name    string
		filter  domain.PhotoFilter
		wantIDs []string
	}{
		{name: "exact views 0", filter: domain.PhotoFilter{ExactViews: intPtr(0)}, wantIDs: []string{"p3"}},
		{name: "min max views 1..2", filter: domain.PhotoFilter{MinViews: intPtr(1), MaxViews: intPtr(2)}, wantIDs: []string{"p1", "p2"}},
		{name: "emoji AND filter", filter: domain.PhotoFilter{EmojiTags: []string{"❤️", "🏖️"}}, wantIDs: []string{"p1"}},
		{name: "untagged", filter: domain.PhotoFilter{Untagged: true}, wantIDs: []string{"p3"}},
		{name: "media type video", filter: domain.PhotoFilter{MediaType: domain.MediaTypeVideo}, wantIDs: []string{"p3"}},
		{name: "formats cr3", filter: domain.PhotoFilter{Formats: []domain.MediaFormat{domain.FormatCR3}}, wantIDs: []string{"p1"}},
		{name: "folder prefix isolation", filter: domain.PhotoFilter{Folder: "2025"}, wantIDs: []string{"p1", "p2"}},
		{name: "has gps true", filter: domain.PhotoFilter{HasGPS: boolPtr(true)}, wantIDs: []string{"p1", "p2"}},
		{name: "has gps false", filter: domain.PhotoFilter{HasGPS: boolPtr(false)}, wantIDs: []string{"p3"}},
		{
			name:    "gps bbox",
			filter:  domain.PhotoFilter{MinLat: floatPtr(0), MaxLat: floatPtr(20), MinLng: floatPtr(10), MaxLng: floatPtr(30)},
			wantIDs: []string{"p1"},
		},
		{name: "date range", filter: domain.PhotoFilter{FromDate: timePtr(t2), ToDate: timePtr(t3)}, wantIDs: []string{"p3", "p2"}},
		{name: "query escapes percent and underscore", filter: domain.PhotoFilter{Query: "100%"}, wantIDs: []string{"p1"}},
		{
			name:    "sort date_taken desc",
			filter:  domain.PhotoFilter{SortBy: domain.SortByDateTaken, SortOrder: domain.SortOrderDesc},
			wantIDs: []string{"p3", "p2", "p1"},
		},
		{
			name:    "sort last_viewed desc",
			filter:  domain.PhotoFilter{SortBy: domain.SortByLastViewed, SortOrder: domain.SortOrderDesc},
			wantIDs: []string{"p2", "p1", "p3"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			res, err := repo.Search(ctx, tc.filter)
			if err != nil {
				t.Fatalf("Search failed: %v", err)
			}
			gotIDs := make([]string, 0, len(res.Items))
			for _, item := range res.Items {
				gotIDs = append(gotIDs, item.ID)
			}
			if !slices.Equal(gotIDs, tc.wantIDs) {
				t.Fatalf("got IDs %v, want %v (total=%d)", gotIDs, tc.wantIDs, res.Total)
			}
		})
	}

	// Deterministic random pagination across pages.
	page1, err := repo.Search(ctx, domain.PhotoFilter{SortBy: domain.SortByRandom, Seed: 77, Page: 1, PageSize: 2})
	if err != nil {
		t.Fatalf("random page1 failed: %v", err)
	}
	page2, err := repo.Search(ctx, domain.PhotoFilter{SortBy: domain.SortByRandom, Seed: 77, Page: 2, PageSize: 2})
	if err != nil {
		t.Fatalf("random page2 failed: %v", err)
	}
	page1Repeat, err := repo.Search(ctx, domain.PhotoFilter{SortBy: domain.SortByRandom, Seed: 77, Page: 1, PageSize: 2})
	if err != nil {
		t.Fatalf("random page1Repeat failed: %v", err)
	}
	if len(page1.Items) != 2 || len(page2.Items) != 1 || page1.Total != 3 {
		t.Fatalf("unexpected random pagination counts: p1=%d p2=%d total=%d", len(page1.Items), len(page2.Items), page1.Total)
	}
	if page1.Items[0].ID != page1Repeat.Items[0].ID || page1.Items[1].ID != page1Repeat.Items[1].ID {
		t.Fatal("random sort with identical seed was not deterministic")
	}
	seen := map[string]bool{page1.Items[0].ID: true, page1.Items[1].ID: true, page2.Items[0].ID: true}
	if len(seen) != 3 {
		t.Fatalf("random pagination overlapped across pages: %v", seen)
	}

	if _, err := repo.Search(ctx, domain.PhotoFilter{SortBy: "invalid"}); !errors.Is(err, domain.ErrInvalidFilter) {
		t.Fatalf("invalid filter err = %v, want ErrInvalidFilter", err)
	}

	// Extreme page numbers must never overflow int offset and wrap to Page 1.
	for _, bigPage := range []int{math.MaxInt, math.MaxInt/50 + 2} {
		pastEnd, err := repo.Search(ctx, domain.PhotoFilter{Page: bigPage, PageSize: 50})
		if err != nil {
			t.Fatalf("Search(page=%d) failed: %v", bigPage, err)
		}
		if pastEnd.Total != 3 || len(pastEnd.Items) != 0 {
			t.Fatalf("Search(page=%d) = (total=%d, len=%d), want (3, 0)", bigPage, pastEnd.Total, len(pastEnd.Items))
		}
	}

	// Case-sensitive folder prefix range: "2025/SUB" must not match "2025/sub".
	upperFolder, err := repo.Search(ctx, domain.PhotoFilter{Folder: "2025/SUB"})
	if err != nil || upperFolder.Total != 0 {
		t.Fatalf("Search(Folder=2025/SUB) = (%d, %v), want 0", upperFolder.Total, err)
	}

	// Verify folder range predicate can use idx_photos_folder (rather than LIKE full-scan).
	planRows, err := db.SQL().QueryContext(
		ctx,
		`EXPLAIN QUERY PLAN SELECT id FROM photos p WHERE p.folder_path = ? OR (p.folder_path >= ? AND p.folder_path < ?)`,
		"2025", "2025/", "20250",
	)
	if err != nil {
		t.Fatalf("EXPLAIN QUERY PLAN failed: %v", err)
	}
	defer func() { _ = planRows.Close() }()
	var plans []string
	for planRows.Next() {
		var id, parent, notused int
		var detail string
		if err := planRows.Scan(&id, &parent, &notused, &detail); err != nil {
			t.Fatalf("scan query plan failed: %v", err)
		}
		plans = append(plans, detail)
	}
	if !strings.Contains(strings.Join(plans, "\n"), "idx_photos_folder") {
		t.Fatalf("expected idx_photos_folder in query plan, got: %v", plans)
	}
}

func TestPhotoRepository_Integration_GetLibraryStats(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	ctx := context.Background()
	db := openTestDB(t, filepath.Join(t.TempDir(), "photos.db"))
	repo := sqlite.NewPhotoRepository(db.SQL())
	viewRepo := sqlite.NewViewRepository(db.SQL())
	now := time.Date(2026, 5, 4, 12, 0, 0, 0, time.UTC)

	if err := repo.CreatePhoto(ctx, makeSamplePhoto("p1", "A", "IMG_1", domain.MediaTypePhoto, []domain.PhotoFile{
		{ID: "f1", ContentHash: "dup", RelativePath: "A/IMG_1.jpg", FileName: "IMG_1.jpg", FolderPath: "A", Format: domain.FormatJPG, FileSize: 10, ModTime: now},
		{ID: "f2", ContentHash: "dup", RelativePath: "B/IMG_1.jpg", FileName: "IMG_1.jpg", FolderPath: "B", Format: domain.FormatJPG, FileSize: 10, ModTime: now},
	}, now)); err != nil {
		t.Fatalf("CreatePhoto p1 failed: %v", err)
	}
	if err := repo.CreatePhoto(ctx, makeSamplePhoto("p2", "A", "IMG_2", domain.MediaTypePhoto, []domain.PhotoFile{
		{ID: "f3", ContentHash: "uniq", RelativePath: "A/IMG_2.CR3", FileName: "IMG_2.CR3", FolderPath: "A", Format: domain.FormatCR3, FileSize: 20, ModTime: now},
	}, now)); err != nil {
		t.Fatalf("CreatePhoto p2 failed: %v", err)
	}
	if err := repo.CreatePhoto(ctx, makeSamplePhoto("p3", "C", "IMG_3", domain.MediaTypePhoto, []domain.PhotoFile{
		{ID: "f4", ContentHash: "miss", RelativePath: "C/IMG_3.jpg", FileName: "IMG_3.jpg", FolderPath: "C", Format: domain.FormatJPG, FileSize: 10, ModTime: now},
	}, now)); err != nil {
		t.Fatalf("CreatePhoto p3 failed: %v", err)
	}
	if err := repo.MarkFilesMissing(ctx, []string{"f4"}, now); err != nil {
		t.Fatalf("MarkFilesMissing failed: %v", err)
	}
	if _, err := viewRepo.AdjustViewCount(ctx, "p1", domain.ViewActionIncrement, now); err != nil {
		t.Fatalf("AdjustViewCount failed: %v", err)
	}

	stats, err := repo.GetLibraryStats(ctx)
	if err != nil {
		t.Fatalf("GetLibraryStats failed: %v", err)
	}
	if stats.TotalPhotos != 2 || stats.UnseenPhotos != 1 || stats.SeenPhotos != 1 || stats.TotalViews != 1 {
		t.Fatalf("unexpected photo/view stats: %+v", stats)
	}
	if stats.TotalFiles != 3 || stats.DuplicateGroups != 1 || stats.DuplicateFiles != 2 {
		t.Fatalf("unexpected file/dup stats: %+v", stats)
	}
	if stats.MissingPhotos != 1 || stats.MissingFiles != 1 {
		t.Fatalf("unexpected missing stats: %+v", stats)
	}
	if len(stats.ByFormat) != 2 || len(stats.ByFolder) != 1 || stats.ByFolder[0].FolderPath != "A" || stats.ByFolder[0].Count != 2 {
		t.Fatalf("unexpected breakdown stats: byFormat=%+v byFolder=%+v", stats.ByFormat, stats.ByFolder)
	}
}

func TestPhotoRepository_Integration_EdgeAndErrorBranches(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	ctx := context.Background()
	db := openTestDB(t, filepath.Join(t.TempDir(), "photos.db"))
	repo := sqlite.NewPhotoRepository(db.SQL())
	now := time.Date(2026, 5, 5, 12, 0, 0, 0, time.UTC)

	// Empty duplicates & empty search results.
	dups, err := repo.ListDuplicates(ctx)
	if err != nil || len(dups) != 0 {
		t.Fatalf("empty ListDuplicates = (%+v, %v)", dups, err)
	}
	emptySearch, err := repo.Search(ctx, domain.PhotoFilter{Query: "nothing"})
	if err != nil || emptySearch.Total != 0 || len(emptySearch.Items) != 0 {
		t.Fatalf("empty Search = (%+v, %v)", emptySearch, err)
	}

	// CreatePhoto with invalid emoji tag rolls back.
	if err := repo.CreatePhoto(ctx, domain.Photo{
		ID: "p-bad-tag", BaseName: "bad", MediaType: domain.MediaTypePhoto, EmojiTags: []string{"ascii"},
	}); !errors.Is(err, domain.ErrInvalidEmojiTag) {
		t.Fatalf("CreatePhoto invalid tag err = %v, want ErrInvalidEmojiTag", err)
	}

	// CreatePhoto using zero timestamps, empty folder/stackKey, and mark missing -> reactivate via UpdateFileInPlace & AttachFile.
	if err := repo.CreatePhoto(ctx, domain.Photo{
		ID:        "p-edge",
		BaseName:  "IMG_1",
		MediaType: domain.MediaTypePhoto,
		Files: []domain.PhotoFile{
			{
				ID: "f-cr3", ContentHash: "h1", RelativePath: "Trip/IMG_1.CR3",
				FileName: "IMG_1.CR3", FolderPath: "Trip", Format: domain.FormatCR3, FileSize: 100, ModTime: now,
			},
			{
				ID: "f-jpg", ContentHash: "h2", RelativePath: "Trip/IMG_1.jpg",
				FileName: "IMG_1.jpg", FolderPath: "Trip", Format: domain.FormatJPG, FileSize: 50, ModTime: now,
			},
		},
	}); err != nil {
		t.Fatalf("CreatePhoto minimal failed: %v", err)
	}

	// No-op missing & empty moves.
	if err := repo.MarkFilesMissing(ctx, []string{"", ""}, now); err != nil {
		t.Fatalf("MarkFilesMissing empty failed: %v", err)
	}
	if err := repo.MoveFilesBatch(ctx, nil, time.Time{}); err != nil {
		t.Fatalf("MoveFilesBatch nil failed: %v", err)
	}
	if err := repo.MoveFilesBatch(ctx, []sqlite.FileMove{{FileID: "", RelativePath: "x"}}, now); err == nil {
		t.Fatal("expected error for empty FileID in MoveFilesBatch")
	}

	// Mark both missing, then reactivate via UpdateFileInPlace by ID; test invalid & missing transitions.
	if err := repo.UpdateFileInPlace(ctx, domain.PhotoFile{ID: "f-cr3", Status: "bogus"}); err == nil {
		t.Fatal("expected error for invalid FileStatus in UpdateFileInPlace")
	}
	if err := repo.MarkFilesMissing(ctx, []string{"f-cr3", "f-jpg", "f-cr3"}, now); err != nil {
		t.Fatalf("MarkFilesMissing failed: %v", err)
	}
	if err := repo.UpdateFileInPlace(ctx, domain.PhotoFile{
		ID: "f-cr3", ContentHash: "h1-new", FileSize: 120, ModTime: now,
	}); err != nil {
		t.Fatalf("UpdateFileInPlace by ID failed: %v", err)
	}
	if err := repo.UpdateFileInPlace(ctx, domain.PhotoFile{}); !errors.Is(err, domain.ErrFileNotFound) {
		t.Fatalf("UpdateFileInPlace empty err = %v, want ErrFileNotFound", err)
	}
	reactivated, _ := repo.GetByID(ctx, "p-edge")
	if reactivated.Status != domain.FileStatusActive {
		t.Fatalf("expected active status after UpdateFileInPlace, got %q", reactivated.Status)
	}
	if err := repo.UpdateFileInPlace(ctx, domain.PhotoFile{
		ID: "f-cr3", ContentHash: "h1-new", FileSize: 120, ModTime: now, Status: domain.FileStatusMissing,
	}); err != nil {
		t.Fatalf("UpdateFileInPlace to missing failed: %v", err)
	}
	demoted, _ := repo.GetByID(ctx, "p-edge")
	if demoted.Status != domain.FileStatusMissing {
		t.Fatalf("expected missing parent status when sole active file updated to missing, got %q", demoted.Status)
	}

	// Move stacked/duplicate files: omitted FolderPath derives from RelativePath, NFD FileName normalizes, and parent leaves inactive folder.
	if err := repo.AttachFile(ctx, "p-edge", domain.PhotoFile{
		ID: "f-dup", ContentHash: "h1-new", RelativePath: "Backup/IMG_1.CR3",
		FileName: "IMG_1.CR3", FolderPath: "Backup", Format: domain.FormatCR3, FileSize: 120, ModTime: now,
	}); err != nil {
		t.Fatalf("AttachFile reactivation failed: %v", err)
	}
	if err := repo.MoveFilesBatch(ctx, []sqlite.FileMove{
		{FileID: "f-cr3", RelativePath: "Trip2/REN_1.CR3", FileName: "Cafe\u0301_1.CR3"},
	}, time.Time{}); err != nil {
		t.Fatalf("MoveFilesBatch cross-folder dup failed: %v", err)
	}
	movedParent, _ := repo.GetByID(ctx, "p-edge")
	if movedParent.FolderPath == "Trip" {
		t.Fatalf("expected parent FolderPath to move off inactive Trip folder, got %q", movedParent.FolderPath)
	}
	var movedFile domain.PhotoFile
	for _, f := range movedParent.Files {
		if f.ID == "f-cr3" {
			movedFile = f
		}
	}
	if movedFile.FolderPath != "Trip2" || movedFile.FileName != "Café_1.CR3" {
		t.Fatalf("expected derived FolderPath=Trip2 and NFC FileName=Café_1.CR3, got %+v", movedFile)
	}

	// RemoveTag error cases.
	if err := repo.RemoveTag(ctx, "p-edge", "not-emoji"); !errors.Is(err, domain.ErrInvalidEmojiTag) {
		t.Fatalf("RemoveTag invalid err = %v, want ErrInvalidEmojiTag", err)
	}
	if err := repo.RemoveTag(ctx, "missing", "❤️"); !errors.Is(err, domain.ErrPhotoNotFound) {
		t.Fatalf("RemoveTag missing photo err = %v, want ErrPhotoNotFound", err)
	}

	// Closed DB propagates errors cleanly across all repository methods.
	if err := db.Close(); err != nil {
		t.Fatalf("db.Close failed: %v", err)
	}
	if _, err := repo.GetByID(ctx, "p-edge"); err == nil {
		t.Fatal("expected GetByID error on closed DB")
	}
	if _, err := repo.GetByStackKey(ctx, "x"); err == nil {
		t.Fatal("expected GetByStackKey error on closed DB")
	}
	if _, err := repo.FindFilesByContentHash(ctx, "h1"); err == nil {
		t.Fatal("expected FindFilesByContentHash error on closed DB")
	}
	if _, err := repo.ListAllFiles(ctx); err == nil {
		t.Fatal("expected ListAllFiles error on closed DB")
	}
	if err := repo.CreatePhoto(ctx, domain.Photo{ID: "x"}); err == nil {
		t.Fatal("expected CreatePhoto error on closed DB")
	}
	if err := repo.AttachFile(ctx, "p-edge", domain.PhotoFile{ID: "x"}); err == nil {
		t.Fatal("expected AttachFile error on closed DB")
	}
	if err := repo.UpdateFileInPlace(ctx, domain.PhotoFile{ID: "f-cr3"}); err == nil {
		t.Fatal("expected UpdateFileInPlace error on closed DB")
	}
	if err := repo.MoveFilesBatch(ctx, []sqlite.FileMove{{FileID: "f-cr3", RelativePath: "a/b.jpg", FolderPath: "a"}}, now); err == nil {
		t.Fatal("expected MoveFilesBatch error on closed DB")
	}
	if err := repo.MarkFilesMissing(ctx, []string{"f-cr3"}, now); err == nil {
		t.Fatal("expected MarkFilesMissing error on closed DB")
	}
	if _, err := repo.Search(ctx, domain.PhotoFilter{}); err == nil {
		t.Fatal("expected Search error on closed DB")
	}
	if err := repo.AddTag(ctx, "p-edge", "❤️", now); err == nil {
		t.Fatal("expected AddTag error on closed DB")
	}
	if err := repo.RemoveTag(ctx, "p-edge", "❤️"); err == nil {
		t.Fatal("expected RemoveTag error on closed DB")
	}
	if _, err := repo.ListTags(ctx); err == nil {
		t.Fatal("expected ListTags error on closed DB")
	}
	if _, err := repo.ListDuplicates(ctx); err == nil {
		t.Fatal("expected ListDuplicates error on closed DB")
	}
	if _, err := repo.ListMissing(ctx); err == nil {
		t.Fatal("expected ListMissing error on closed DB")
	}
	if _, err := repo.PurgeMissing(ctx); err == nil {
		t.Fatal("expected PurgeMissing error on closed DB")
	}
	if _, err := repo.GetLibraryStats(ctx); err == nil {
		t.Fatal("expected GetLibraryStats error on closed DB")
	}
}

func TestPhotoRepository_Integration_ConstraintRollbacksAndDuplicateSorting(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	ctx := context.Background()
	db := openTestDB(t, filepath.Join(t.TempDir(), "photos.db"))
	repo := sqlite.NewPhotoRepository(db.SQL())
	now := time.Date(2026, 5, 6, 12, 0, 0, 0, time.UTC)

	// Seed 3 duplicate groups with differing counts and hashes to test sorting & tie-breaks.
	for _, tc := range []struct {
		id, hash, path string
	}{
		{"p1", "h-b", "d1/1.jpg"},
		{"p2", "h-a", "d2/1.jpg"},
		{"p3", "h-c", "d3/1.jpg"},
	} {
		err := repo.CreatePhoto(ctx, makeSamplePhoto(tc.id, "d", tc.id, domain.MediaTypePhoto, []domain.PhotoFile{
			{ID: "f-" + tc.id, ContentHash: tc.hash, RelativePath: tc.path, FileName: "1.jpg", FolderPath: "d", Format: domain.FormatJPG, FileSize: 10, ModTime: now},
		}, now))
		if err != nil {
			t.Fatalf("seed %s failed: %v", tc.id, err)
		}
	}
	for i, extra := range []struct {
		pid, fid, hash, path string
	}{
		{"p1", "e1", "h-b", "d1/copy1.jpg"},
		{"p1", "e2", "h-b", "d1/copy2.jpg"},
		{"p2", "e3", "h-a", "d2/copy1.jpg"},
		{"p3", "e4", "h-c", "d3/copy1.jpg"},
	} {
		if err := repo.AttachFile(ctx, extra.pid, domain.PhotoFile{
			ID: extra.fid, ContentHash: extra.hash, RelativePath: extra.path,
			FileName: "copy.jpg", FolderPath: "d", Format: domain.FormatJPG, FileSize: 10, ModTime: now,
		}); err != nil {
			t.Fatalf("attach #%d failed: %v", i, err)
		}
	}

	dups, err := repo.ListDuplicates(ctx)
	if err != nil || len(dups) != 3 {
		t.Fatalf("ListDuplicates = (%d, %v), want 3", len(dups), err)
	}
	if dups[0].ContentHash != "h-b" || dups[0].FileCount != 3 || dups[1].ContentHash != "h-a" || dups[2].ContentHash != "h-c" {
		t.Fatalf("unexpected duplicate group order: %+v", dups)
	}

	// Duplicate photo ID and duplicate relative_path roll back CreatePhoto.
	if err := repo.CreatePhoto(ctx, domain.Photo{ID: "p1", StackKey: "x", BaseName: "x", MediaType: domain.MediaTypePhoto}); err == nil {
		t.Fatal("expected duplicate photo ID error")
	}
	if err := repo.CreatePhoto(ctx, makeSamplePhoto("p-dup-path", "d1", "dup", domain.MediaTypePhoto, []domain.PhotoFile{
		{ID: "f-dup-path", ContentHash: "hx", RelativePath: "d1/1.jpg", FileName: "1.jpg", FolderPath: "d1", Format: domain.FormatJPG, FileSize: 10, ModTime: now},
	}, now)); err == nil {
		t.Fatal("expected duplicate relative_path rollback in CreatePhoto")
	}

	// MoveFilesBatch collision against an existing file rolls back temp stage cleanly.
	if err := repo.MoveFilesBatch(ctx, []sqlite.FileMove{
		{FileID: "f-p1", RelativePath: "d2/1.jpg", FileName: "1.jpg", FolderPath: "d2"},
	}, now); err == nil {
		t.Fatal("expected UNIQUE(relative_path) collision error in MoveFilesBatch")
	}
	p1After, _ := repo.GetByID(ctx, "p1")
	if p1After.Files[0].RelativePath != "d1/1.jpg" {
		t.Fatalf("expected rollback of d1/1.jpg, got %q", p1After.Files[0].RelativePath)
	}

	// Abort trigger on photos delete verifies PurgeMissing transactional rollback.
	if err := repo.MarkFilesMissing(ctx, []string{"f-p3", "e4"}, now); err != nil {
		t.Fatalf("MarkFilesMissing p3 failed: %v", err)
	}
	if _, err := db.SQL().ExecContext(ctx, `
		CREATE TRIGGER block_photo_delete BEFORE DELETE ON photos
		BEGIN SELECT RAISE(ABORT, 'blocked'); END`); err != nil {
		t.Fatalf("create trigger failed: %v", err)
	}
	if _, err := repo.PurgeMissing(ctx); err == nil {
		t.Fatal("expected PurgeMissing error when delete trigger aborts")
	}
	// Missing files from step 1 of PurgeMissing must be rolled back!
	if files, _ := repo.FindFilesByContentHash(ctx, "h-c"); len(files) != 2 {
		t.Fatalf("expected PurgeMissing rollback keeping 2 files, got %d", len(files))
	}

	// Trigger failures on photo_files delete, photo_tags, and photo updates verify remaining rollback paths.
	if err := repo.AddTag(ctx, "p1", "❤️", now); err != nil {
		t.Fatalf("seed tag for delete trigger failed: %v", err)
	}
	if _, err := db.SQL().ExecContext(ctx, `
		CREATE TRIGGER block_photo_update BEFORE UPDATE ON photos
		BEGIN SELECT RAISE(ABORT, 'photo update blocked'); END;
		CREATE TRIGGER block_tag_insert BEFORE INSERT ON photo_tags
		BEGIN SELECT RAISE(ABORT, 'tag insert blocked'); END;
		CREATE TRIGGER block_tag_delete BEFORE DELETE ON photo_tags
		BEGIN SELECT RAISE(ABORT, 'tag delete blocked'); END`); err != nil {
		t.Fatalf("create triggers failed: %v", err)
	}
	if err := repo.AttachFile(ctx, "p1", domain.PhotoFile{
		ID: "f-block", ContentHash: "hb", RelativePath: "d1/block.jpg", FileName: "block.jpg", FolderPath: "d1", Format: domain.FormatJPG, FileSize: 10, ModTime: now,
	}); err == nil {
		t.Fatal("expected AttachFile error when photo update trigger aborts")
	}
	if err := repo.UpdateFileInPlace(ctx, domain.PhotoFile{ID: "f-p1", ContentHash: "hx", FileSize: 99, ModTime: now}); err == nil {
		t.Fatal("expected UpdateFileInPlace error when photo update trigger aborts")
	}
	if err := repo.MoveFilesBatch(ctx, []sqlite.FileMove{{FileID: "f-p1", RelativePath: "d1/moved.jpg", FileName: "moved.jpg", FolderPath: "d1"}}, now); err == nil {
		t.Fatal("expected MoveFilesBatch error when parent update trigger aborts")
	}
	if err := repo.MarkFilesMissing(ctx, []string{"f-p1"}, now); err == nil {
		t.Fatal("expected MarkFilesMissing error when parent update trigger aborts")
	}
	if err := repo.AddTag(ctx, "p1", "⭐", now); err == nil {
		t.Fatal("expected AddTag error when tag insert trigger aborts")
	}
	if err := repo.RemoveTag(ctx, "p1", "❤️"); err == nil {
		t.Fatal("expected RemoveTag error when tag delete trigger aborts")
	}
	if _, err := db.SQL().ExecContext(ctx, `
		CREATE TRIGGER block_file_delete BEFORE DELETE ON photo_files
		BEGIN SELECT RAISE(ABORT, 'file delete blocked'); END`); err != nil {
		t.Fatalf("create file delete trigger failed: %v", err)
	}
	if _, err := repo.PurgeMissing(ctx); err == nil {
		t.Fatal("expected PurgeMissing error when file delete trigger aborts")
	}
	if _, err := db.SQL().ExecContext(ctx, `DROP TABLE photo_files`); err != nil {
		t.Fatalf("drop photo_files failed: %v", err)
	}
	if _, err := repo.GetLibraryStats(ctx); err == nil {
		t.Fatal("expected GetLibraryStats error when photo_files is missing")
	}
}

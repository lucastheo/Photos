package sqlite

import (
	"database/sql"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/lucastheo/photos/internal/domain"
)

func TestBuildDSN(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "in memory",
			input: ":memory:",
			want:  "file::memory:?" + dsnParams,
		},
		{
			name:  "trimmed in memory",
			input: "  :memory:  ",
			want:  "file::memory:?" + dsnParams,
		},
		{
			name:  "standard path",
			input: "/var/lib/photos/photos.db",
			want:  "file:/var/lib/photos/photos.db?" + dsnParams,
		},
		{
			name:  "escapes percent question and hash",
			input: "/tmp/100% legit?/album#1/photos.db",
			want:  "file:/tmp/100%25 legit%3F/album%231/photos.db?" + dsnParams,
		},
		{
			name:  "relative path via filepath.Join",
			input: filepath.Join("data", "sub", "photos.db"),
			want:  "file:data/sub/photos.db?" + dsnParams,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := BuildDSN(tc.input); got != tc.want {
				t.Fatalf("BuildDSN(%q) = %q, want %q", tc.input, got, tc.want)
			}
		})
	}
}

func TestPhotoEntityMapping(t *testing.T) {
	t.Parallel()

	takenAt := time.Date(2025, 8, 14, 10, 30, 0, 0, time.UTC)
	lastViewed := time.Date(2026, 1, 5, 18, 40, 12, 123456789, time.UTC)
	createdAt := time.Date(2026, 1, 1, 9, 0, 0, 0, time.UTC)
	updatedAt := time.Date(2026, 1, 5, 18, 41, 0, 0, time.UTC)
	iso := 400
	fNum := 2.8
	focal := 50.0
	lat := -23.5505
	lng := -46.6333

	t.Run("full photo round trip with priority sorting", func(t *testing.T) {
		t.Parallel()
		files := []domain.PhotoFile{
			{
				ID:           "f-jpg",
				PhotoID:      "p1",
				ContentHash:  "hash-jpg",
				RelativePath: "2025/Trip/IMG_1.JPG",
				FileName:     "IMG_1.JPG",
				FolderPath:   "2025/Trip",
				Format:       domain.FormatJPG,
				FileSize:     1024,
				ModTime:      createdAt,
				Status:       domain.FileStatusActive,
				CreatedAt:    createdAt,
				UpdatedAt:    updatedAt,
			},
			{
				ID:           "f-cr3",
				PhotoID:      "p1",
				ContentHash:  "hash-cr3",
				RelativePath: "2025/Trip/IMG_1.CR3",
				FileName:     "IMG_1.CR3",
				FolderPath:   "2025/Trip",
				Format:       domain.FormatCR3,
				FileSize:     20480,
				ModTime:      createdAt,
				Status:       domain.FileStatusActive,
				CreatedAt:    createdAt,
				UpdatedAt:    updatedAt,
			},
		}
		in := domain.Photo{
			ID:          "p1",
			StackKey:    "2025/Trip/photo/img_1",
			BaseName:    "IMG_1",
			FolderPath:  "/2025/Trip/",
			MediaType:   domain.MediaTypePhoto,
			Width:       6000,
			Height:      4000,
			DurationSec: 0,
			TakenAt:     &takenAt,
			EXIF: domain.EXIFMetadata{
				Orientation:   6,
				CameraMake:    "Canon",
				CameraModel:   "EOS R5",
				LensModel:     "RF 50mm",
				ISO:           &iso,
				FNumber:       &fNum,
				ExposureTime:  "1/500",
				FocalLengthMM: &focal,
				GPSLatitude:   &lat,
				GPSLongitude:  &lng,
			},
			ViewCount:    3,
			LastViewedAt: &lastViewed,
			Status:       domain.FileStatusActive,
			CreatedAt:    createdAt,
			UpdatedAt:    updatedAt,
		}

		entity := photoToEntity(in)
		if entity.FolderPath != "2025/Trip" || entity.Orientation != 6 || !entity.ISO.Valid || entity.ISO.Int64 != 400 {
			t.Fatalf("unexpected photoEntity: %+v", entity)
		}

		got := photoFromEntity(entity, files, []string{"❤", "🏖"})
		if got.ID != in.ID || got.FolderPath != "2025/Trip" || got.ViewCount != 3 {
			t.Fatalf("unexpected domain.Photo header: %+v", got)
		}
		if len(got.Files) != 2 || got.Files[0].Format != domain.FormatCR3 || got.Files[1].Format != domain.FormatJPG {
			t.Fatalf("files not sorted by priority: %+v", got.Files)
		}
		if !slices.Equal(got.Formats, []domain.MediaFormat{domain.FormatCR3, domain.FormatJPG}) {
			t.Fatalf("Formats = %v, want [cr3 jpg]", got.Formats)
		}
		if !slices.Equal(got.EmojiTags, []string{"❤", "🏖"}) {
			t.Fatalf("EmojiTags = %v", got.EmojiTags)
		}
		if got.TakenAt == nil || !got.TakenAt.Equal(takenAt) {
			t.Fatalf("TakenAt = %v, want %v", got.TakenAt, takenAt)
		}
		if got.LastViewedAt == nil || !got.LastViewedAt.Equal(lastViewed) {
			t.Fatalf("LastViewedAt = %v, want %v", got.LastViewedAt, lastViewed)
		}
		if got.EXIF.ISO == nil || *got.EXIF.ISO != 400 || got.EXIF.GPSLatitude == nil || *got.EXIF.GPSLatitude != lat {
			t.Fatalf("unexpected EXIF: %+v", got.EXIF)
		}
	})

	t.Run("zero defaults and null fields", func(t *testing.T) {
		t.Parallel()
		entity := photoToEntity(domain.Photo{
			ID:        "p2",
			StackKey:  "photo/img_2",
			BaseName:  "IMG_2",
			MediaType: domain.MediaTypePhoto,
		})
		if entity.Orientation != 1 || entity.Status != string(domain.FileStatusActive) {
			t.Fatalf("unexpected defaults: orientation=%d status=%q", entity.Orientation, entity.Status)
		}
		if entity.TakenAtUnix.Valid || entity.CameraMake.Valid || entity.ISO.Valid || entity.GPSLatitude.Valid || entity.LastViewedUnixNs.Valid {
			t.Fatalf("expected NULL fields on minimal entity: %+v", entity)
		}

		got := photoFromEntity(entity, nil, nil)
		if got.TakenAt != nil || got.LastViewedAt != nil || got.EXIF.ISO != nil || got.EXIF.FNumber != nil || got.EXIF.GPSLongitude != nil {
			t.Fatalf("expected nil pointer fields: %+v", got)
		}
		if !got.CreatedAt.IsZero() || !got.UpdatedAt.IsZero() {
			t.Fatalf("expected zero CreatedAt/UpdatedAt: %v / %v", got.CreatedAt, got.UpdatedAt)
		}
	})
}

func TestPhotoFileEntityMapping(t *testing.T) {
	t.Parallel()

	modTime := time.Date(2026, 2, 1, 12, 0, 0, 500, time.UTC)
	created := time.Date(2026, 2, 2, 8, 0, 0, 0, time.UTC)

	t.Run("normalizes NFD path and round-trips", func(t *testing.T) {
		t.Parallel()
		in := domain.PhotoFile{
			ID:           "f1",
			PhotoID:      "p1",
			ContentHash:  "abc123",
			RelativePath: "/Cafe\u0301/IMG.JPG",
			FileName:     "IMG.JPG",
			FolderPath:   `\Cafe` + "\u0301" + `\`,
			Format:       domain.FormatJPG,
			FileSize:     4096,
			ModTime:      modTime,
			Status:       domain.FileStatusMissing,
			CreatedAt:    created,
			UpdatedAt:    created,
		}
		e := photoFileToEntity(in)
		if e.RelativePath != "Café/IMG.JPG" || e.FolderPath != "Café" {
			t.Fatalf("expected NFC normalized paths, got rel=%q folder=%q", e.RelativePath, e.FolderPath)
		}
		got := photoFileFromEntity(e)
		if got.RelativePath != "Café/IMG.JPG" || got.Status != domain.FileStatusMissing || !got.ModTime.Equal(modTime) {
			t.Fatalf("unexpected mapped PhotoFile: %+v", got)
		}
	})

	t.Run("defaults empty status to active", func(t *testing.T) {
		t.Parallel()
		e := photoFileToEntity(domain.PhotoFile{ID: "f2"})
		if e.Status != string(domain.FileStatusActive) {
			t.Fatalf("Status = %q, want active", e.Status)
		}
		got := photoFileFromEntity(e)
		if !got.ModTime.IsZero() || !got.CreatedAt.IsZero() {
			t.Fatalf("expected zero times, got mod=%v created=%v", got.ModTime, got.CreatedAt)
		}
	})
}

func TestViewEventAndSyncReportEntityMapping(t *testing.T) {
	t.Parallel()

	viewedAt := time.Date(2026, 3, 1, 15, 0, 0, 987654321, time.UTC)
	startedAt := time.Date(2026, 3, 2, 10, 0, 0, 0, time.UTC)
	finishedAt := startedAt.Add(30 * time.Second)

	t.Run("view event with and without view_id", func(t *testing.T) {
		t.Parallel()
		withID := viewEventToEntity(domain.ViewEvent{
			ID:       7,
			ViewID:   "uuid-1",
			PhotoID:  "p1",
			ViewedAt: viewedAt,
			Source:   domain.ViewSourceDwell,
		})
		if !withID.ViewID.Valid || withID.ViewID.String != "uuid-1" {
			t.Fatalf("expected valid ViewID, got %+v", withID.ViewID)
		}
		got := viewEventFromEntity(withID)
		if got.ID != 7 || got.ViewID != "uuid-1" || !got.ViewedAt.Equal(viewedAt) || got.Source != domain.ViewSourceDwell {
			t.Fatalf("unexpected ViewEvent: %+v", got)
		}

		withoutID := viewEventToEntity(domain.ViewEvent{
			PhotoID: "p1",
			Source:  domain.ViewSourceManual,
		})
		if withoutID.ViewID.Valid || withoutID.ViewedAtUnixNs != 0 {
			t.Fatalf("expected NULL ViewID and zero ViewedAtUnixNs, got %+v", withoutID)
		}
		gotNoID := viewEventFromEntity(withoutID)
		if gotNoID.ViewID != "" || !gotNoID.ViewedAt.IsZero() {
			t.Fatalf("unexpected ViewEvent without ID: %+v", gotNoID)
		}
	})

	t.Run("sync report running and failed", func(t *testing.T) {
		t.Parallel()
		running := syncReportToEntity(domain.SyncReport{
			ID:        "run-1",
			StartedAt: startedAt,
			Status:    domain.SyncStatusRunning,
		})
		if running.FinishedAtUnix.Valid || running.ErrorMessage.Valid {
			t.Fatalf("expected NULL finished_at and error_message: %+v", running)
		}
		gotRunning := syncReportFromEntity(running)
		if gotRunning.FinishedAt != nil || gotRunning.ErrorMessage != "" || !gotRunning.StartedAt.Equal(startedAt) {
			t.Fatalf("unexpected running SyncReport: %+v", gotRunning)
		}

		failed := syncReportToEntity(domain.SyncReport{
			ID:             "run-2",
			StartedAt:      startedAt,
			FinishedAt:     &finishedAt,
			Status:         domain.SyncStatusFailed,
			ScannedCount:   10,
			AddedCount:     4,
			UpdatedCount:   1,
			MovedCount:     2,
			StackedCount:   1,
			DuplicateCount: 1,
			UnchangedCount: 1,
			MissingCount:   3,
			ErrorMessage:   "disk unplugged",
		})
		gotFailed := syncReportFromEntity(failed)
		if gotFailed.FinishedAt == nil || !gotFailed.FinishedAt.Equal(finishedAt) || gotFailed.ErrorMessage != "disk unplugged" {
			t.Fatalf("unexpected failed SyncReport: %+v", gotFailed)
		}
		if gotFailed.MissingCount != 3 || gotFailed.MovedCount != 2 {
			t.Fatalf("unexpected counters: %+v", gotFailed)
		}
	})
}

func TestNullHelpers(t *testing.T) {
	t.Parallel()

	if got := nullString(""); got.Valid {
		t.Fatalf("nullString empty should be invalid: %+v", got)
	}
	if got := nullIntPtrFromEntity(sql.NullInt64{}); got != nil {
		t.Fatalf("expected nil int ptr: %v", got)
	}
	if got := nullFloatPtrFromEntity(sql.NullFloat64{}); got != nil {
		t.Fatalf("expected nil float ptr: %v", got)
	}
	if got := nullUnixToPtrTime(sql.NullInt64{}); got != nil {
		t.Fatalf("expected nil time ptr: %v", got)
	}
	if got := nullUnixNsToPtrTime(sql.NullInt64{}); got != nil {
		t.Fatalf("expected nil ns time ptr: %v", got)
	}
}

func TestMigrationParsingAndValidation(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name    string
		file    string
		wantVer int
		wantErr bool
	}{
		{name: "valid 001", file: "001_initial_schema.sql", wantVer: 1},
		{name: "valid 012", file: "012_add_index.sql", wantVer: 12},
		{name: "missing underscore", file: "001initial.sql", wantErr: true},
		{name: "non numeric", file: "abc_init.sql", wantErr: true},
		{name: "zero version", file: "000_init.sql", wantErr: true},
		{name: "negative version", file: "-1_init.sql", wantErr: true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, err := parseMigrationVersion(tc.file)
			if (err != nil) != tc.wantErr {
				t.Fatalf("parseMigrationVersion(%q) err=%v, wantErr=%v", tc.file, err, tc.wantErr)
			}
			if !tc.wantErr && got != tc.wantVer {
				t.Fatalf("parseMigrationVersion(%q) = %d, want %d", tc.file, got, tc.wantVer)
			}
		})
	}

	sorted, err := validateAndSortMigrations([]Migration{
		{Version: 3, Name: "c"},
		{Version: 1, Name: "a"},
		{Version: 2, Name: "b"},
	})
	if err != nil {
		t.Fatalf("validateAndSortMigrations failed: %v", err)
	}
	if sorted[0].Version != 1 || sorted[1].Version != 2 || sorted[2].Version != 3 {
		t.Fatalf("unexpected sorted order: %+v", sorted)
	}
}

func TestPhotoHelperBranches(t *testing.T) {
	t.Parallel()

	if got := sqlPlaceholders(0); got != "" {
		t.Fatalf("sqlPlaceholders(0) = %q, want empty", got)
	}
	if got := sqlPlaceholders(3); got != "?,?,?" {
		t.Fatalf("sqlPlaceholders(3) = %q, want ?,?,?", got)
	}

	dedup := uniqueNonEmptyStrings([]string{"", "a", "b", "a", ""})
	if !slices.Equal(dedup, []string{"a", "b"}) {
		t.Fatalf("uniqueNonEmptyStrings = %v, want [a b]", dedup)
	}

	prepared := preparePhotoForInsert(domain.Photo{
		ID:        "p1",
		BaseName:  "IMG_9",
		MediaType: domain.MediaTypePhoto,
		Files: []domain.PhotoFile{
			{FolderPath: "2026/Trips", Format: domain.FormatCR3, Status: domain.FileStatusActive},
		},
	})
	if prepared.CreatedAt.IsZero() || prepared.UpdatedAt.IsZero() || prepared.Status != domain.FileStatusActive {
		t.Fatalf("expected defaults populated: %+v", prepared)
	}
	if prepared.FolderPath != "2026/Trips" || prepared.StackKey != "2026/Trips/photo/img_9" {
		t.Fatalf("expected derived FolderPath/StackKey, got folder=%q stack=%q", prepared.FolderPath, prepared.StackKey)
	}

	f, b, sk := computeMovedParentAttrs(domain.MediaTypePhoto, "orig", "origDir", "origDir/photo/orig", nil)
	if f != "origDir" || b != "orig" || sk != "origDir/photo/orig" {
		t.Fatalf("empty active files mutated parent attrs: %q %q %q", f, b, sk)
	}

	f2, b2, sk2 := computeMovedParentAttrs(domain.MediaTypePhoto, "orig", "origDir", "origDir/photo/orig", []activeFileMeta{
		{folderPath: "a", fileName: "A.jpg", format: domain.FormatJPG},
		{folderPath: "b", fileName: "A.jpg", format: domain.FormatJPG},
	})
	if f2 != "origDir" || b2 != "orig" || sk2 != "origDir/photo/orig" {
		t.Fatalf("cross-folder duplicates mutated parent attrs: %q %q %q", f2, b2, sk2)
	}

	f3, b3, sk3 := computeMovedParentAttrs(domain.MediaTypePhoto, "orig", "origDir", "origDir/photo/orig", []activeFileMeta{
		{folderPath: "same", fileName: "A.jpg", format: domain.FormatJPG},
		{folderPath: "same", fileName: "B.CR3", format: domain.FormatCR3},
	})
	if f3 != "same" || b3 != "orig" || sk3 != "same/photo/orig" {
		t.Fatalf("ambiguous stems in same folder = (%q, %q, %q)", f3, b3, sk3)
	}

	emptyHydrated, err := hydratePhotos(t.Context(), nil, nil)
	if err != nil || len(emptyHydrated) != 0 {
		t.Fatalf("hydratePhotos(nil) = (%+v, %v)", emptyHydrated, err)
	}

	dir := t.TempDir()
	if err := copyFileAtomic(filepath.Join(dir, "does-not-exist.db"), filepath.Join(dir, "out.db")); err == nil {
		t.Fatal("expected copyFileAtomic error for missing source")
	}
	src := filepath.Join(dir, "src.db")
	if err := os.WriteFile(src, []byte("sqlite-bytes"), 0o600); err != nil {
		t.Fatalf("WriteFile src failed: %v", err)
	}
	if err := copyFileAtomic(src, filepath.Join(dir, "missing-subdir", "out.db")); err == nil {
		t.Fatal("expected copyFileAtomic error for missing destination directory")
	}
}

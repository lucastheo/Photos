package media_test

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/lucastheo/photos/internal/adapter/media"
	"github.com/lucastheo/photos/internal/domain"
)

type trackedRecord struct {
	photoID   string
	viewCount int
	file      domain.PhotoFile
}

func TestMediaPipelineFourStepSyncIntegration(t *testing.T) {
	t.Parallel()
	if testing.Short() {
		t.Skip("skipping integration test in -short mode")
	}

	libDir := t.TempDir()
	cacheDir := t.TempDir()

	// Step 1: Populate initial 5 library files + ignored noise files.
	cr3Data := buildCR3Fixture(t, tiffFixtureSpec{
		orientation:  6,
		makeName:     "Canon",
		modelName:    "EOS R5",
		iso:          200,
		fnumNum:      28,
		fnumDen:      10,
		expNum:       1,
		expDen:       250,
		focalNum:     35,
		focalDen:     1,
		width:        600,
		height:       400,
		dateTimeOrig: "2026:04:10 11:00:00",
	}, encodeTestJPEG(t, 80, 60), encodeTestJPEG(t, 600, 400))

	writeTestFile(t, libDir, "IMG_001.CR3", cr3Data)
	writeTestFile(t, libDir, "IMG_001.JPG", encodeTestJPEG(t, 600, 400))
	writeTestFile(t, libDir, "IMG_002.JPG", encodeTestJPEG(t, 320, 240))
	writeTestFile(t, libDir, "IMG_003.JPG", injectJPEGAPP1(encodeTestJPEG(t, 500, 300), buildTIFFFixture(tiffFixtureSpec{
		littleEndian: true,
		orientation:  1,
		makeName:     "Sony",
		modelName:    "A7C",
		iso:          100,
		fnumNum:      40,
		fnumDen:      10,
		expNum:       1,
		expDen:       125,
		focalNum:     50,
		focalDen:     1,
		width:        500,
		height:       300,
		dateTimeOrig: "2026:04:11 12:00:00",
	})))
	writeTestFile(t, libDir, "clip.mp4", buildMP4Fixture(0, 1000, 8250, 1280, 720))
	writeTestFile(t, libDir, "._IMG_001.CR3", []byte("appledouble"))
	writeTestFile(t, libDir, ".DS_Store", []byte("finder"))
	writeTestFile(t, libDir, "zero.jpg", nil)

	scanner := media.NewScanner(libDir, 2)
	exifExt := media.NewEXIFExtractor(libDir).WithExifToolPath("")
	prevExt := media.NewPreviewExtractor(libDir, cacheDir, 2).WithTools("", "", "")

	scanned1, err := scanner.Walk(t.Context())
	if err != nil {
		t.Fatalf("initial Walk failed: %v", err)
	}
	if len(scanned1) != 5 {
		t.Fatalf("expected 5 valid media files, got %d: %+v", len(scanned1), scanned1)
	}

	hashed1, err := scanner.HashFiles(t.Context(), scanned1)
	if err != nil {
		t.Fatalf("initial HashFiles failed: %v", err)
	}

	records := make(map[string]*trackedRecord, len(hashed1))
	for i, sf := range hashed1 {
		meta, exErr := exifExt.Extract(t.Context(), sf.RelativePath, sf.Format)
		if exErr != nil {
			t.Fatalf("Extract(%q) failed: %v", sf.RelativePath, exErr)
		}
		if err := prevExt.EnsureCache(t.Context(), sf.RelativePath, sf.ContentHash, sf.Format, meta.EXIF.Orientation); err != nil {
			t.Fatalf("EnsureCache(%q) failed: %v", sf.RelativePath, err)
		}
		records[sf.RelativePath] = &trackedRecord{
			photoID: "photo-" + string(rune('a'+i)),
			file: domain.PhotoFile{
				RelativePath: sf.RelativePath,
				ContentHash:  sf.ContentHash,
				Format:       sf.Format,
				FileSize:     sf.FileSize,
				ModTime:      sf.ModTime,
				Status:       domain.FileStatusActive,
			},
		}
	}

	// Step 2: Simulate viewing IMG_001.CR3 4 times.
	records["IMG_001.CR3"].viewCount = 4
	img003PhotoID := records["IMG_003.JPG"].photoID
	img003OldHash := records["IMG_003.JPG"].file.ContentHash
	img001CR3Hash := records["IMG_001.CR3"].file.ContentHash

	// Step 3: In-place edit IMG_003.JPG, move/rename IMG_001.CR3, delete IMG_002.JPG.
	editedBytes := encodeTestJPEG(t, 700, 500)
	editedPath := writeTestFile(t, libDir, "IMG_003.JPG", editedBytes)
	newTime := time.Now().UTC().Add(2 * time.Second)
	if err := os.Chtimes(editedPath, newTime, newTime); err != nil {
		t.Fatalf("chtimes IMG_003.JPG: %v", err)
	}
	subDir := filepath.Join(libDir, "subfolder")
	if err := os.MkdirAll(subDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(filepath.Join(libDir, "IMG_001.CR3"), filepath.Join(subDir, "RENAMED.CR3")); err != nil {
		t.Fatalf("rename IMG_001.CR3: %v", err)
	}
	if err := os.Remove(filepath.Join(libDir, "IMG_002.JPG")); err != nil {
		t.Fatalf("remove IMG_002.JPG: %v", err)
	}

	// Step 4: Incremental rescan & metadata fast-skip verification.
	scanned2, err := scanner.Walk(t.Context())
	if err != nil {
		t.Fatalf("rescan Walk failed: %v", err)
	}
	if len(scanned2) != 4 {
		t.Fatalf("expected 4 active files after deletion, got %d", len(scanned2))
	}

	var toHash []domain.ScannedFile
	seenPaths := make(map[string]bool)
	unchangedCount := 0
	for _, sf := range scanned2 {
		seenPaths[sf.RelativePath] = true
		if rec, ok := records[sf.RelativePath]; ok && scanner.FileMatchesMetadata(sf, rec.file) {
			unchangedCount++
			continue
		}
		toHash = append(toHash, sf)
	}
	if unchangedCount != 2 {
		t.Fatalf("expected 2 fast-skipped unchanged files (IMG_001.JPG, clip.mp4), got %d", unchangedCount)
	}
	if len(toHash) != 2 {
		t.Fatalf("expected 2 files needing hashing (IMG_003.JPG, subfolder/RENAMED.CR3), got %d", len(toHash))
	}

	rehashed, err := scanner.HashFiles(t.Context(), toHash)
	if err != nil {
		t.Fatalf("rehash failed: %v", err)
	}

	missingByHash := make(map[string]*trackedRecord)
	for rel, rec := range records {
		if !seenPaths[rel] {
			missingByHash[rec.file.ContentHash] = rec
		}
	}

	for _, sf := range rehashed {
		switch sf.RelativePath {
		case "IMG_003.JPG":
			rec := records[sf.RelativePath]
			if rec.photoID != img003PhotoID {
				t.Fatalf("expected preserved PhotoID %q", img003PhotoID)
			}
			if sf.ContentHash == img003OldHash {
				t.Fatal("expected new ContentHash for in-place edited IMG_003.JPG")
			}
			if err := prevExt.EnsureCache(t.Context(), sf.RelativePath, sf.ContentHash, sf.Format, 1); err != nil {
				t.Fatalf("EnsureCache edited IMG_003.JPG failed: %v", err)
			}
			assertJPEGDims(t, filepath.Join(cacheDir, media.ThumbnailFilename(sf.ContentHash)), 400, 286)
		case "subfolder/RENAMED.CR3":
			if sf.ContentHash != img001CR3Hash {
				t.Fatalf("expected moved CR3 hash %q, got %q", img001CR3Hash, sf.ContentHash)
			}
			movedFrom, ok := missingByHash[sf.ContentHash]
			if !ok || movedFrom.viewCount != 4 {
				t.Fatalf("expected moved CR3 to relink with viewCount=4, got ok=%v rec=%+v", ok, movedFrom)
			}
			delete(missingByHash, sf.ContentHash)
		default:
			t.Fatalf("unexpected rehashed path %q", sf.RelativePath)
		}
	}

	if len(missingByHash) != 1 {
		t.Fatalf("expected 1 remaining missing file (IMG_002.JPG), got %d", len(missingByHash))
	}
	for _, rem := range missingByHash {
		if rem.file.RelativePath != "IMG_002.JPG" {
			t.Fatalf("expected missing IMG_002.JPG, got %q", rem.file.RelativePath)
		}
	}
}

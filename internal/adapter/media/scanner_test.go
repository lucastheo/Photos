package media_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/lucastheo/photos/internal/adapter/media"
	"github.com/lucastheo/photos/internal/domain"
)

func TestScannerWalk(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	outside := t.TempDir()

	writeTestFile(t, root, "z_root.JPG", []byte("rootjpg"))
	writeTestFile(t, root, "Trip/a_raw.CR3", []byte("cr3"))
	writeTestFile(t, root, "Trip/Sub/clip.mp4", []byte("mp4"))
	writeTestFile(t, root, "Cafe\u0301/pic.jpeg", []byte("jpeg"))
	// Ignored files and directories
	writeTestFile(t, root, "empty.jpg", nil)
	writeTestFile(t, root, "notes.txt", []byte("text"))
	writeTestFile(t, root, ".hidden.jpg", []byte("hidden"))
	writeTestFile(t, root, "._IMG_001.JPG", []byte("appledouble"))
	writeTestFile(t, root, ".git/ignored.jpg", []byte("git"))
	writeTestFile(t, root, "@eaDir/thumb.jpg", []byte("synology"))

	extTarget := writeTestFile(t, outside, "link.jpg", []byte("outside"))
	if err := os.Symlink(extTarget, filepath.Join(root, "symlink.jpg")); err != nil {
		t.Skipf("symlink unsupported: %v", err)
	}

	s := media.NewScanner(root, 2)
	got, err := s.Walk(t.Context())
	if err != nil {
		t.Fatalf("Walk failed: %v", err)
	}

	wantPaths := []string{
		"Café/pic.jpeg",
		"Trip/Sub/clip.mp4",
		"Trip/a_raw.CR3",
		"z_root.JPG",
	}
	if len(got) != len(wantPaths) {
		t.Fatalf("expected %d scanned files, got %d: %+v", len(wantPaths), len(got), got)
	}
	for i, wantRel := range wantPaths {
		if got[i].RelativePath != wantRel {
			t.Fatalf("index %d: got RelativePath %q, want %q", i, got[i].RelativePath, wantRel)
		}
		if got[i].ModTime.Location() != time.UTC {
			t.Fatalf("index %d: expected UTC ModTime, got %v", i, got[i].ModTime.Location())
		}
	}

	if got[0].FolderPath != "Café" || got[0].Format != domain.FormatJPG || got[0].BaseName != "pic" {
		t.Fatalf("unexpected NFC/jpeg entry: %+v", got[0])
	}
	if got[1].MediaType != domain.MediaTypeVideo || got[1].Format != domain.FormatMP4 {
		t.Fatalf("unexpected video entry: %+v", got[1])
	}
	if got[2].Format != domain.FormatCR3 || got[2].BaseName != "a_raw" {
		t.Fatalf("unexpected CR3 entry: %+v", got[2])
	}
	if got[3].FolderPath != "" {
		t.Fatalf("expected empty FolderPath for root file, got %q", got[3].FolderPath)
	}
}

func TestScannerWalkErrorsAndEmptyDir(t *testing.T) {
	t.Parallel()

	t.Run("empty dir returns empty slice", func(t *testing.T) {
		t.Parallel()
		s := media.NewScanner(t.TempDir(), 0)
		got, err := s.Walk(t.Context())
		if err != nil {
			t.Fatalf("expected nil error on empty dir, got %v", err)
		}
		if len(got) != 0 {
			t.Fatalf("expected 0 files, got %d", len(got))
		}
	})

	t.Run("missing or empty library path returns ErrLibraryUnavailable", func(t *testing.T) {
		t.Parallel()
		for _, p := range []string{"", filepath.Join(t.TempDir(), "missing")} {
			s := media.NewScanner(p, 1)
			_, err := s.Walk(t.Context())
			if !errors.Is(err, domain.ErrLibraryUnavailable) {
				t.Fatalf("path %q: expected ErrLibraryUnavailable, got %v", p, err)
			}
		}
	})

	t.Run("regular file as library path returns ErrLibraryUnavailable", func(t *testing.T) {
		t.Parallel()
		f := writeTestFile(t, t.TempDir(), "notadir", []byte("x"))
		s := media.NewScanner(f, 1)
		_, err := s.Walk(t.Context())
		if !errors.Is(err, domain.ErrLibraryUnavailable) {
			t.Fatalf("expected ErrLibraryUnavailable, got %v", err)
		}
	})

	t.Run("canceled context", func(t *testing.T) {
		t.Parallel()
		s := media.NewScanner(t.TempDir(), 1)
		ctx, cancel := context.WithCancel(t.Context())
		cancel()
		if _, err := s.Walk(ctx); !errors.Is(err, context.Canceled) {
			t.Fatalf("expected context.Canceled, got %v", err)
		}
	})

	t.Run("unreadable subdir returns walk error", func(t *testing.T) {
		t.Parallel()
		if os.Geteuid() == 0 {
			t.Skip("skipping permission test as root")
		}
		root := t.TempDir()
		locked := filepath.Join(root, "locked")
		if err := os.MkdirAll(locked, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.Chmod(locked, 0o000); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = os.Chmod(locked, 0o755) })
		s := media.NewScanner(root, 1)
		if _, err := s.Walk(t.Context()); err == nil {
			t.Fatal("expected walk error for unreadable subdirectory")
		}
	})
}

func TestScannerFileMatchesMetadataAndHashDelegation(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	writeTestFile(t, root, "a.jpg", []byte("abc"))
	s := media.NewScanner(root, 2)

	sum, err := s.HashFile(t.Context(), "a.jpg")
	if err != nil || len(sum) != 64 {
		t.Fatalf("HashFile failed: sum=%q err=%v", sum, err)
	}
	files, err := s.Walk(t.Context())
	if err != nil {
		t.Fatalf("Walk failed: %v", err)
	}
	hashed, err := s.HashFiles(t.Context(), files)
	if err != nil || len(hashed) != 1 || hashed[0].ContentHash != sum {
		t.Fatalf("HashFiles failed: hashed=%+v err=%v", hashed, err)
	}

	ts := time.Date(2026, 5, 1, 12, 0, 0, 123456789, time.UTC)
	locMinus3 := time.FixedZone("BRT", -3*3600)
	scanned := domain.ScannedFile{
		RelativePath: "Café/IMG_1.JPG",
		FileSize:     1024,
		ModTime:      ts,
	}
	existing := domain.PhotoFile{
		RelativePath: "Cafe\u0301/IMG_1.JPG",
		FileSize:     1024,
		ModTime:      ts.In(locMinus3),
	}
	if !s.FileMatchesMetadata(scanned, existing) {
		t.Fatal("expected NFC/NFD and timezone-equivalent metadata to match")
	}

	existingDiffSize := existing
	existingDiffSize.FileSize = 1025
	if s.FileMatchesMetadata(scanned, existingDiffSize) {
		t.Fatal("expected size mismatch to return false")
	}

	existingDiffPath := existing
	existingDiffPath.RelativePath = "Other/IMG_1.JPG"
	if s.FileMatchesMetadata(scanned, existingDiffPath) {
		t.Fatal("expected path mismatch to return false")
	}

	existingDiffTime := existing
	existingDiffTime.ModTime = ts.Add(time.Nanosecond)
	if s.FileMatchesMetadata(scanned, existingDiffTime) {
		t.Fatal("expected modtime mismatch to return false")
	}
}

package media_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/lucastheo/photos/internal/adapter/media"
	"github.com/lucastheo/photos/internal/domain"
)

func TestNewHasherDefaultsWorkers(t *testing.T) {
	t.Parallel()
	hDefault := media.NewHasher(t.TempDir(), 0)
	if hDefault.Workers() != max(1, runtime.NumCPU()) {
		t.Fatalf("expected default workers %d, got %d", max(1, runtime.NumCPU()), hDefault.Workers())
	}
	hNeg := media.NewHasher(t.TempDir(), -3)
	if hNeg.Workers() != max(1, runtime.NumCPU()) {
		t.Fatalf("expected default workers for negative input, got %d", hNeg.Workers())
	}
	hExplicit := media.NewHasher(t.TempDir(), 3)
	if hExplicit.Workers() != 3 {
		t.Fatalf("expected explicit workers 3, got %d", hExplicit.Workers())
	}
}

func TestHasherHashFile(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	outsideDir := t.TempDir()

	smallData := []byte("hello photo tracker")
	largeData := []byte(strings.Repeat("0123456789abcdef", 10*1024)) // 160 KiB > 64 KiB chunk
	writeTestFile(t, root, "sub/small.jpg", smallData)
	writeTestFile(t, root, "large.cr3", largeData)
	writeTestFile(t, root, "Café/photo.jpg", []byte("nfc"))
	outsideFile := writeTestFile(t, outsideDir, "secret.jpg", []byte("secret"))
	if err := os.Symlink(outsideFile, filepath.Join(root, "escape.jpg")); err != nil {
		t.Skipf("symlink unsupported: %v", err)
	}

	sumHex := func(b []byte) string {
		s := sha256.Sum256(b)
		return hex.EncodeToString(s[:])
	}

	h := media.NewHasher(root, 2)
	tests := []struct {
		name    string
		relPath string
		want    string
		wantErr bool
	}{
		{name: "small nested file", relPath: "sub/small.jpg", want: sumHex(smallData)},
		{name: "large multi-chunk file", relPath: "large.cr3", want: sumHex(largeData)},
		{name: "nfd path normalizes to nfc", relPath: "Cafe\u0301/photo.jpg", want: sumHex([]byte("nfc"))},
		{name: "empty path rejected", relPath: "", wantErr: true},
		{name: "dot path rejected", relPath: ".", wantErr: true},
		{name: "traversal rejected", relPath: "../secret.jpg", wantErr: true},
		{name: "symlink escape rejected", relPath: "escape.jpg", wantErr: true},
		{name: "directory rejected", relPath: "sub", wantErr: true},
		{name: "missing file rejected", relPath: "nope.jpg", wantErr: true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, err := h.HashFile(t.Context(), tc.relPath)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected error for %q, got hash %q", tc.relPath, got)
				}
				return
			}
			if err != nil {
				t.Fatalf("HashFile(%q) failed: %v", tc.relPath, err)
			}
			if got != tc.want {
				t.Fatalf("hash mismatch: got %q, want %q", got, tc.want)
			}
		})
	}
}

func TestHasherHashFileErrorsAndCancellation(t *testing.T) {
	t.Parallel()

	t.Run("missing library root returns ErrLibraryUnavailable", func(t *testing.T) {
		t.Parallel()
		h := media.NewHasher(filepath.Join(t.TempDir(), "missing"), 2)
		_, err := h.HashFile(t.Context(), "a.jpg")
		if !errors.Is(err, domain.ErrLibraryUnavailable) {
			t.Fatalf("expected ErrLibraryUnavailable, got %v", err)
		}
	})

	t.Run("canceled context", func(t *testing.T) {
		t.Parallel()
		root := t.TempDir()
		writeTestFile(t, root, "a.jpg", []byte("abc"))
		h := media.NewHasher(root, 2)
		ctx, cancel := context.WithCancel(t.Context())
		cancel()
		_, err := h.HashFile(ctx, "a.jpg")
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("expected context.Canceled, got %v", err)
		}
	})
}

func TestHasherHashFiles(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	files := []domain.ScannedFile{
		{RelativePath: "b.jpg"},
		{RelativePath: "a.cr3"},
		{RelativePath: "c/d.mp4"},
	}
	contents := map[string][]byte{
		"b.jpg":   []byte("beta"),
		"a.cr3":   []byte("alpha"),
		"c/d.mp4": []byte("gamma"),
	}
	for rel, b := range contents {
		writeTestFile(t, root, rel, b)
	}

	h := media.NewHasher(root, 2)
	empty, err := h.HashFiles(t.Context(), nil)
	if err != nil || len(empty) != 0 {
		t.Fatalf("expected empty slice with nil error, got (%v, %v)", empty, err)
	}

	got, err := h.HashFiles(t.Context(), files)
	if err != nil {
		t.Fatalf("HashFiles failed: %v", err)
	}
	if len(got) != len(files) {
		t.Fatalf("expected %d files, got %d", len(files), len(got))
	}
	for i, f := range got {
		if f.RelativePath != files[i].RelativePath {
			t.Fatalf("order changed at %d: got %q, want %q", i, f.RelativePath, files[i].RelativePath)
		}
		if files[i].ContentHash != "" {
			t.Fatalf("input slice was mutated at %d", i)
		}
		sum := sha256.Sum256(contents[f.RelativePath])
		if f.ContentHash != hex.EncodeToString(sum[:]) {
			t.Fatalf("hash mismatch for %q: got %q", f.RelativePath, f.ContentHash)
		}
	}

	_, err = h.HashFiles(t.Context(), []domain.ScannedFile{{RelativePath: "a.cr3"}, {RelativePath: "missing.jpg"}})
	if err == nil {
		t.Fatal("expected error when one file in batch is missing")
	}

	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := h.HashFiles(ctx, files); !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context.Canceled, got %v", err)
	}
}

package media_test

import (
	"context"
	"errors"
	"image"
	"image/color"
	"image/jpeg"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/lucastheo/photos/internal/adapter/media"
	"github.com/lucastheo/photos/internal/domain"
)

func TestApplyOrientation(t *testing.T) {
	t.Parallel()
	// 2x3 image with distinct pixel colors at all corners:
	// (0,0)=Red, (1,0)=Green, (0,2)=Blue, (1,2)=Yellow
	src := image.NewNRGBA(image.Rect(10, 20, 12, 23)) // non-zero bounds Min=(10,20)
	red := color.NRGBA{R: 255, A: 255}
	green := color.NRGBA{G: 255, A: 255}
	blue := color.NRGBA{B: 255, A: 255}
	yellow := color.NRGBA{R: 255, G: 255, A: 255}
	midL := color.NRGBA{R: 100, A: 255}
	midR := color.NRGBA{G: 100, A: 255}
	src.SetNRGBA(10, 20, red)
	src.SetNRGBA(11, 20, green)
	src.SetNRGBA(10, 21, midL)
	src.SetNRGBA(11, 21, midR)
	src.SetNRGBA(10, 22, blue)
	src.SetNRGBA(11, 22, yellow)

	if media.ApplyOrientation(nil, domain.FormatJPG, 6) != nil {
		t.Fatal("expected nil for nil image")
	}
	heicOut := media.ApplyOrientation(src, domain.FormatHEIC, 6)
	if heicOut.Bounds().Dx() != 2 || heicOut.Bounds().Dy() != 3 {
		t.Fatalf("HEIC must skip rotation, got %dx%d", heicOut.Bounds().Dx(), heicOut.Bounds().Dy())
	}
	heifOut := media.ApplyOrientation(src, domain.FormatHEIF, 8)
	if heifOut.Bounds().Dx() != 2 || heifOut.Bounds().Dy() != 3 {
		t.Fatalf("HEIF must skip rotation, got %dx%d", heifOut.Bounds().Dx(), heifOut.Bounds().Dy())
	}

	tests := []struct {
		ori     int
		wantW   int
		wantH   int
		checkX  int
		checkY  int
		wantCol color.NRGBA
	}{
		{ori: 1, wantW: 2, wantH: 3, checkX: 0, checkY: 0, wantCol: red},
		{ori: 2, wantW: 2, wantH: 3, checkX: 0, checkY: 0, wantCol: green},
		{ori: 3, wantW: 2, wantH: 3, checkX: 0, checkY: 0, wantCol: yellow},
		{ori: 4, wantW: 2, wantH: 3, checkX: 0, checkY: 0, wantCol: blue},
		{ori: 5, wantW: 3, wantH: 2, checkX: 2, checkY: 0, wantCol: blue},
		{ori: 6, wantW: 3, wantH: 2, checkX: 0, checkY: 0, wantCol: blue},
		{ori: 7, wantW: 3, wantH: 2, checkX: 0, checkY: 0, wantCol: yellow},
		{ori: 8, wantW: 3, wantH: 2, checkX: 0, checkY: 0, wantCol: green},
	}

	for _, tc := range tests {
		out := media.ApplyOrientation(src, domain.FormatJPG, tc.ori)
		if out.Bounds().Dx() != tc.wantW || out.Bounds().Dy() != tc.wantH {
			t.Fatalf("ori %d: got %dx%d, want %dx%d", tc.ori, out.Bounds().Dx(), out.Bounds().Dy(), tc.wantW, tc.wantH)
		}
		got := color.NRGBAModel.Convert(out.At(out.Bounds().Min.X+tc.checkX, out.Bounds().Min.Y+tc.checkY)).(color.NRGBA)
		if got != tc.wantCol {
			t.Fatalf("ori %d at (%d,%d): got %+v, want %+v", tc.ori, tc.checkX, tc.checkY, got, tc.wantCol)
		}
	}
}

func TestPreviewExtractorPureGoAndCacheSkip(t *testing.T) {
	t.Parallel()
	lib := t.TempDir()
	cache := t.TempDir()

	if media.ThumbnailFilename("abc") != "abc_thumb.jpg" || media.PreviewFilename("abc") != "abc_preview.jpg" {
		t.Fatal("unexpected cache filename helpers")
	}

	// 800x600 JPEG with Orientation 6 -> should rotate to 600x800, thumb=300x400, preview=600x800.
	jpgBytes := encodeTestJPEG(t, 800, 600)
	writeTestFile(t, lib, "tall.jpg", jpgBytes)

	// CR3 with embedded small + large JPEG previews.
	cr3Bytes := buildCR3Fixture(t, tiffFixtureSpec{orientation: 6, width: 600, height: 400}, encodeTestJPEG(t, 80, 60), encodeTestJPEG(t, 600, 400))
	writeTestFile(t, lib, "raw.cr3", cr3Bytes)

	// HEIC with embedded preview JPEG; orientation=6 must NOT double-rotate.
	heicBytes := buildHEICFixture(500, 300, nil, encodeTestJPEG(t, 500, 300))
	writeTestFile(t, lib, "iphone.heic", heicBytes)

	// MP4 video without ffmpeg -> synthesized poster frame.
	writeTestFile(t, lib, "clip.mp4", buildMP4Fixture(0, 1000, 5000, 1280, 720))

	p := media.NewPreviewExtractor(lib, cache, 0).WithTools("", "", "")

	if err := p.EnsureCache(t.Context(), "tall.jpg", "hashjpg", domain.FormatJPG, 6); err != nil {
		t.Fatalf("EnsureCache tall.jpg failed: %v", err)
	}
	assertJPEGDims(t, filepath.Join(cache, media.ThumbnailFilename("hashjpg")), 300, 400)
	assertJPEGDims(t, filepath.Join(cache, media.PreviewFilename("hashjpg")), 600, 800)

	// Remove source and re-run: must hit cache and skip without touching source!
	if err := os.Remove(filepath.Join(lib, "tall.jpg")); err != nil {
		t.Fatal(err)
	}
	if err := p.EnsureCache(t.Context(), "tall.jpg", "hashjpg", domain.FormatJPG, 6); err != nil {
		t.Fatalf("expected cache hit to skip regeneration: %v", err)
	}

	if err := p.EnsureCache(t.Context(), "raw.cr3", "hashcr3", domain.FormatCR3, 6); err != nil {
		t.Fatalf("EnsureCache raw.cr3 failed: %v", err)
	}
	assertJPEGDims(t, filepath.Join(cache, media.ThumbnailFilename("hashcr3")), 267, 400)
	assertJPEGDims(t, filepath.Join(cache, media.PreviewFilename("hashcr3")), 400, 600)

	if err := p.EnsureCache(t.Context(), "iphone.heic", "hashheic", domain.FormatHEIC, 6); err != nil {
		t.Fatalf("EnsureCache iphone.heic failed: %v", err)
	}
	// Landscape 500x300 stays landscape (no double-rotation).
	assertJPEGDims(t, filepath.Join(cache, media.ThumbnailFilename("hashheic")), 400, 240)
	assertJPEGDims(t, filepath.Join(cache, media.PreviewFilename("hashheic")), 500, 300)

	if err := p.EnsureCache(t.Context(), "clip.mp4", "hashmp4", domain.FormatMP4, 1); err != nil {
		t.Fatalf("EnsureCache clip.mp4 failed: %v", err)
	}
	assertJPEGDims(t, filepath.Join(cache, media.ThumbnailFilename("hashmp4")), 400, 225)
	assertJPEGDims(t, filepath.Join(cache, media.PreviewFilename("hashmp4")), 640, 360)

	// Confirm no leftover .tmp files exist in cache directory.
	entries, err := os.ReadDir(cache)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if strings.Contains(e.Name(), ".tmp.") {
			t.Fatalf("unexpected leftover temp file %q", e.Name())
		}
	}
}

func TestPreviewExtractorMockToolsTimeoutAndConcurrency(t *testing.T) {
	t.Parallel()
	lib := t.TempDir()
	cache := t.TempDir()
	bin := t.TempDir()

	sampleJPG := filepath.Join(bin, "sample.jpg")
	if err := os.WriteFile(sampleJPG, encodeTestJPEG(t, 3000, 2000), 0o644); err != nil {
		t.Fatal(err)
	}

	writeTestFile(t, lib, "a.cr3", []byte("not-a-jpeg-raw"))
	writeTestFile(t, lib, "b.heic", []byte("not-a-jpeg-heic"))
	writeTestFile(t, lib, "c.mov", []byte("not-a-jpeg-mov"))

	mockExif := writeExecScript(t, bin, "exiftool", "cat "+sampleJPG)
	mockHeif := writeExecScript(t, bin, "heif-convert", `cp `+sampleJPG+` "$2"`)
	mockFFmpeg := writeExecScript(t, bin, "ffmpeg", "cat "+sampleJPG)

	p := media.NewPreviewExtractor(lib, cache, 2).WithTools(mockExif, mockHeif, mockFFmpeg).WithTimeout(5 * time.Second)
	if err := p.EnsureCache(t.Context(), "a.cr3", "hcr3", domain.FormatCR3, 1); err != nil {
		t.Fatalf("mock exiftool preview failed: %v", err)
	}
	assertJPEGDims(t, filepath.Join(cache, media.ThumbnailFilename("hcr3")), 400, 267)
	assertJPEGDims(t, filepath.Join(cache, media.PreviewFilename("hcr3")), 2560, 1707)

	if err := p.EnsureCache(t.Context(), "b.heic", "hheic", domain.FormatHEIC, 6); err != nil {
		t.Fatalf("mock heif-convert preview failed: %v", err)
	}
	assertJPEGDims(t, filepath.Join(cache, media.PreviewFilename("hheic")), 2560, 1707)

	if err := p.EnsureCache(t.Context(), "c.mov", "hmov", domain.FormatMOV, 1); err != nil {
		t.Fatalf("mock ffmpeg preview failed: %v", err)
	}
	assertJPEGDims(t, filepath.Join(cache, media.ThumbnailFilename("hmov")), 400, 267)

	// Timeout test on slow tool.
	slowTool := writeExecScript(t, bin, "slow", "exec sleep 2")
	pSlow := media.NewPreviewExtractor(lib, cache, 1).WithTools(slowTool, slowTool, slowTool).WithTimeout(30 * time.Millisecond)
	if err := pSlow.EnsureCache(t.Context(), "a.cr3", "slow1", domain.FormatCR3, 1); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("expected context.DeadlineExceeded, got %v", err)
	}
	if err := pSlow.EnsureCache(t.Context(), "b.heic", "slow2", domain.FormatHEIC, 1); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("expected context.DeadlineExceeded for heic, got %v", err)
	}
	if err := pSlow.EnsureCache(t.Context(), "c.mov", "slow3", domain.FormatMOV, 1); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("expected context.DeadlineExceeded for mov, got %v", err)
	}

	// Concurrent EnsureCache on identical contentHash must be race-free.
	writeTestFile(t, lib, "same.jpg", encodeTestJPEG(t, 120, 90))
	pRace := media.NewPreviewExtractor(lib, cache, 4).WithTools("", "", "")
	var wg sync.WaitGroup
	for range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := pRace.EnsureCache(t.Context(), "same.jpg", "racehash", domain.FormatJPG, 1); err != nil {
				t.Errorf("concurrent EnsureCache failed: %v", err)
			}
		}()
	}
	wg.Wait()
}

func TestPreviewExtractorValidationAndErrors(t *testing.T) {
	t.Parallel()
	lib := t.TempDir()
	cache := t.TempDir()
	writeTestFile(t, lib, "empty.cr3", []byte("no-jpeg"))

	p := media.NewPreviewExtractor(lib, cache, 1).WithTools("", "", "")

	if err := p.EnsureCache(t.Context(), "empty.cr3", "../evil", domain.FormatCR3, 1); err == nil {
		t.Fatal("expected invalid content hash error")
	}
	if err := p.EnsureCache(t.Context(), "../evil.jpg", "validhash", domain.FormatJPG, 1); err == nil {
		t.Fatal("expected path traversal error")
	}
	if err := p.EnsureCache(t.Context(), "empty.cr3", "validhash", domain.MediaFormat("xyz"), 1); !errors.Is(err, domain.ErrUnsupportedFormat) {
		t.Fatalf("expected ErrUnsupportedFormat, got %v", err)
	}
	if err := p.EnsureCache(t.Context(), "empty.cr3", "validhash", domain.FormatCR3, 1); err == nil {
		t.Fatal("expected error when CR3 has no embedded JPEG and no tools")
	}

	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if err := p.EnsureCache(ctx, "empty.cr3", "validhash", domain.FormatJPG, 1); !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context.Canceled, got %v", err)
	}

	pEmptyCache := media.NewPreviewExtractor(lib, "", 1)
	if err := pEmptyCache.EnsureCache(t.Context(), "empty.cr3", "validhash", domain.FormatJPG, 1); err == nil {
		t.Fatal("expected empty cachePath error")
	}
	pBadLib := media.NewPreviewExtractor(filepath.Join(t.TempDir(), "missing"), cache, 1).WithTools("", "", "")
	if err := pBadLib.EnsureCache(t.Context(), "empty.cr3", "badlib", domain.FormatJPG, 1); !errors.Is(err, domain.ErrLibraryUnavailable) {
		t.Fatalf("expected ErrLibraryUnavailable, got %v", err)
	}
}

func TestPreviewExtractorToolFallbacksAndPartialCache(t *testing.T) {
	t.Parallel()
	lib := t.TempDir()
	cache := t.TempDir()
	bin := t.TempDir()

	sampleTall := filepath.Join(bin, "tall.jpg")
	if err := os.WriteFile(sampleTall, encodeTestJPEG(t, 450, 900), 0o644); err != nil {
		t.Fatal(err)
	}
	writeTestFile(t, lib, "a.cr3", []byte("not-jpeg"))
	writeTestFile(t, lib, "b.heic", []byte("not-jpeg"))
	writeTestFile(t, lib, "c.webp", []byte("RIFF\x00\x00\x00\x00WEBP"))
	writeTestFile(t, lib, "cover.mp4", append(buildMP4Fixture(0, 1000, 2000, 320, 240), encodeTestJPEG(t, 320, 240)...))
	writeTestFile(t, lib, "short.mp4", []byte("short-video"))

	failTool := writeExecScript(t, bin, "fail", "exit 1")
	heifSeq := writeExecScript(t, bin, "heif-seq", `cp `+sampleTall+` "$(dirname "$2")/preview-1.jpg"`)
	ffSeekFallback := writeExecScript(t, bin, "ff-noseek", `
for a in "$@"; do
  if [ "$a" = "-ss" ]; then
    exit 1
  fi
done
cat `+sampleTall)

	// RAW: exiftool fails -> falls back to ffmpeg.
	pRawFF := media.NewPreviewExtractor(lib, cache, 1).WithTools(failTool, "", ffSeekFallback)
	if err := pRawFF.EnsureCache(t.Context(), "a.cr3", "rawff", domain.FormatCR3, 1); err != nil {
		t.Fatalf("RAW ffmpeg fallback failed: %v", err)
	}
	assertJPEGDims(t, filepath.Join(cache, media.ThumbnailFilename("rawff")), 200, 400)

	// HEIC: heif-convert writes preview-1.jpg (glob path), and failing heif-convert falls back to ffmpeg.
	pHeifSeq := media.NewPreviewExtractor(lib, cache, 1).WithTools("", heifSeq, "")
	if err := pHeifSeq.EnsureCache(t.Context(), "b.heic", "heifseq", domain.FormatHEIC, 1); err != nil {
		t.Fatalf("heif-convert sequence fallback failed: %v", err)
	}
	pHeicFF := media.NewPreviewExtractor(lib, cache, 1).WithTools("", failTool, ffSeekFallback)
	if err := pHeicFF.EnsureCache(t.Context(), "b.heic", "heifff", domain.FormatHEIC, 1); err != nil {
		t.Fatalf("HEIC ffmpeg fallback failed: %v", err)
	}

	// Standard WebP non-stdlib falls back to ffmpeg; without ffmpeg fails.
	if err := pRawFF.EnsureCache(t.Context(), "c.webp", "webpff", domain.FormatWebP, 1); err != nil {
		t.Fatalf("WebP ffmpeg fallback failed: %v", err)
	}
	pNoTools := media.NewPreviewExtractor(lib, cache, 1).WithTools("", "", "")
	if err := pNoTools.EnsureCache(t.Context(), "c.webp", "webpfail", domain.FormatWebP, 1); err == nil {
		t.Fatal("expected error for WebP without ffmpeg")
	}

	// Video with -ss failing falls back to seekFirst=false; video without ffmpeg uses embedded JPEG cover art.
	if err := pRawFF.EnsureCache(t.Context(), "short.mp4", "vidnoseek", domain.FormatMP4, 1); err != nil {
		t.Fatalf("video seek fallback failed: %v", err)
	}
	if err := pNoTools.EnsureCache(t.Context(), "cover.mp4", "vidcover", domain.FormatMP4, 1); err != nil {
		t.Fatalf("video embedded cover failed: %v", err)
	}

	// Partial cache: delete only _preview.jpg and re-run EnsureCache.
	if err := os.Remove(filepath.Join(cache, media.PreviewFilename("rawff"))); err != nil {
		t.Fatal(err)
	}
	if err := pRawFF.EnsureCache(t.Context(), "a.cr3", "rawff", domain.FormatCR3, 1); err != nil {
		t.Fatalf("partial cache repair failed: %v", err)
	}
	assertJPEGDims(t, filepath.Join(cache, media.PreviewFilename("rawff")), 450, 900)

	// Partial cache: delete only _thumb.jpg and re-run EnsureCache.
	if err := os.Remove(filepath.Join(cache, media.ThumbnailFilename("rawff"))); err != nil {
		t.Fatal(err)
	}
	if err := pRawFF.EnsureCache(t.Context(), "a.cr3", "rawff", domain.FormatCR3, 1); err != nil {
		t.Fatalf("partial thumb repair failed: %v", err)
	}
	assertJPEGDims(t, filepath.Join(cache, media.ThumbnailFilename("rawff")), 200, 400)

	// RAW: exiftool returns garbage for -PreviewImage, valid JPEG for -JpgFromRaw.
	exifJpgFromRaw := writeExecScript(t, bin, "exif-jpgfromraw", `
if [ "$2" = "-JpgFromRaw" ]; then
  cat `+sampleTall+`
else
  printf 'garbage'
fi`)
	pJpgFromRaw := media.NewPreviewExtractor(lib, cache, 1).WithTools(exifJpgFromRaw, "", "")
	if err := pJpgFromRaw.EnsureCache(t.Context(), "a.cr3", "jpgfromraw", domain.FormatCR3, 1); err != nil {
		t.Fatalf("exiftool -JpgFromRaw fallback failed: %v", err)
	}

	// heif-convert exits 0 without producing any output file -> falls back to ffmpeg / error.
	heifNoOut := writeExecScript(t, bin, "heif-noout", "exit 0")
	pHeifNoOut := media.NewPreviewExtractor(lib, cache, 1).WithTools("", heifNoOut, "")
	if err := pHeifNoOut.EnsureCache(t.Context(), "b.heic", "heifnoout", domain.FormatHEIC, 1); err == nil {
		t.Fatal("expected error when heif-convert creates no file and no embedded jpeg")
	}

	// cachePath pointing to a regular file fails openCacheRoot.
	fileAsCache := writeTestFile(t, t.TempDir(), "filecache", []byte("x"))
	pBadCache := media.NewPreviewExtractor(lib, fileAsCache, 1).WithTools("", "", "")
	if err := pBadCache.EnsureCache(t.Context(), "cover.mp4", "badcache", domain.FormatMP4, 1); err == nil {
		t.Fatal("expected error when cachePath is a file")
	}

	// 1px tall banner exercises dstLen==1 / srcLen==1 resize path and invalid orientation clamp.
	writeTestFile(t, lib, "banner.jpg", encodeTestJPEG(t, 500, 1))
	if err := pNoTools.EnsureCache(t.Context(), "banner.jpg", "bannerhash", domain.FormatJPG, 9); err != nil {
		t.Fatalf("EnsureCache 500x1 banner failed: %v", err)
	}
	assertJPEGDims(t, filepath.Join(cache, media.ThumbnailFilename("bannerhash")), 400, 1)

	// Read-only cache directory fails atomic write without leaving .tmp files (when non-root).
	if os.Geteuid() != 0 {
		roCache := t.TempDir()
		if err := os.Chmod(roCache, 0o555); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = os.Chmod(roCache, 0o755) })
		pRO := media.NewPreviewExtractor(lib, roCache, 1).WithTools("", "", "")
		if err := pRO.EnsureCache(t.Context(), "banner.jpg", "rohash", domain.FormatJPG, 1); err == nil {
			t.Fatal("expected write failure in read-only cache dir")
		}
	}

	// Garbage ffmpeg output falls back to embedded JPEG for RAW/HEIC, and ffmpeg timeout propagates for RAW/HEIC.
	garbageTool := writeExecScript(t, bin, "garbage", `printf 'not-a-jpeg'`)
	writeTestFile(t, lib, "embed.cr3", append(makeISOBox("ftyp", []byte("crx \x00\x00\x00\x00crx ")), encodeTestJPEG(t, 40, 30)...))
	writeTestFile(t, lib, "embed.heic", append(makeISOBox("ftyp", []byte("heic\x00\x00\x00\x00heic")), encodeTestJPEG(t, 40, 30)...))
	pGarbageFF := media.NewPreviewExtractor(lib, cache, 1).WithTools(garbageTool, garbageTool, garbageTool)
	if err := pGarbageFF.EnsureCache(t.Context(), "embed.cr3", "garbcr3", domain.FormatCR3, 1); err != nil {
		t.Fatalf("garbage tool fallback for cr3 failed: %v", err)
	}
	if err := pGarbageFF.EnsureCache(t.Context(), "embed.heic", "garbheic", domain.FormatHEIC, 1); err != nil {
		t.Fatalf("garbage tool fallback for heic failed: %v", err)
	}

	slowFF := writeExecScript(t, bin, "slowff", "exec sleep 2")
	pSlowFF := media.NewPreviewExtractor(lib, cache, 1).WithTools("", "", slowFF).WithTimeout(25 * time.Millisecond)
	if err := pSlowFF.EnsureCache(t.Context(), "embed.cr3", "slowffcr3", domain.FormatCR3, 1); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("expected DeadlineExceeded from ffmpeg on cr3, got %v", err)
	}
	if err := pSlowFF.EnsureCache(t.Context(), "embed.heic", "slowffheic", domain.FormatHEIC, 1); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("expected DeadlineExceeded from ffmpeg on heic, got %v", err)
	}
}

func assertJPEGDims(t *testing.T, path string, wantW, wantH int) {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatalf("open %q: %v", path, err)
	}
	defer func() { _ = f.Close() }()
	cfg, err := jpeg.DecodeConfig(f)
	if err != nil {
		t.Fatalf("decode jpeg config %q: %v", path, err)
	}
	if cfg.Width != wantW || cfg.Height != wantH {
		t.Fatalf("%s: got %dx%d, want %dx%d", filepath.Base(path), cfg.Width, cfg.Height, wantW, wantH)
	}
}

package media_test

import (
	"context"
	"encoding/binary"
	"errors"
	"math"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/lucastheo/photos/internal/adapter/media"
	"github.com/lucastheo/photos/internal/domain"
)

func TestEXIFExtractorNativeJPEGAndStandards(t *testing.T) {
	t.Parallel()
	root := t.TempDir()

	tiff := buildTIFFFixture(tiffFixtureSpec{
		littleEndian: true,
		orientation:  6,
		makeName:     "Canon",
		modelName:    "EOS R5",
		lensModel:    "RF 24-70mm F2.8L",
		iso:          400,
		fnumNum:      28,
		fnumDen:      10,
		expNum:       1,
		expDen:       250,
		focalNum:     50,
		focalDen:     1,
		width:        40,
		height:       20,
		dateTimeOrig: "2026:05:14 10:30:45",
		includeGPS:   true,
		latRef:       "S",
		latDMS:       [3][2]uint32{{23, 1}, {33, 1}, {180, 100}},
		lngRef:       "W",
		lngDMS:       [3][2]uint32{{46, 1}, {38, 1}, {0, 1}},
	})
	jpgWithEXIF := injectJPEGAPP1(encodeTestJPEG(t, 40, 20), tiff)
	writeTestFile(t, root, "orient6.jpg", jpgWithEXIF)
	writeTestFile(t, root, "plain.jpg", encodeTestJPEG(t, 32, 24))
	writeTestFile(t, root, "sample.png", encodeTestPNGWithEXIF(t, 30, 15, buildTIFFFixture(tiffFixtureSpec{
		littleEndian: false,
		orientation:  1,
		makeName:     "PNGCam",
		modelName:    "M1",
		iso:          100,
		fnumNum:      4,
		fnumDen:      1,
		expNum:       1,
		expDen:       60,
		focalNum:     35,
		focalDen:     1,
		width:        30,
		height:       15,
		dateTimeOrig: "2026:01:02 03:04:05",
	})))
	writeTestFile(t, root, "anim.gif", encodeTestGIF(t, 18, 12))

	// WebP VP8X (24-bit W-1, H-1) + EXIF
	vp8x := []byte("RIFF\x22\x00\x00\x00WEBPVP8X\x0a\x00\x00\x00\x00\x00\x00\x00\x63\x00\x00\x31\x00\x00")
	writeTestFile(t, root, "pic.webp", vp8x)

	ext := media.NewEXIFExtractor(root).WithExifToolPath("")

	meta, err := ext.Extract(t.Context(), "orient6.jpg", domain.FormatJPG)
	if err != nil {
		t.Fatalf("Extract orient6.jpg failed: %v", err)
	}
	if meta.Width != 20 || meta.Height != 40 {
		t.Fatalf("expected swapped post-orientation dims 20x40, got %dx%d", meta.Width, meta.Height)
	}
	if meta.EXIF.Orientation != 6 || meta.EXIF.CameraMake != "Canon" || meta.EXIF.CameraModel != "EOS R5" {
		t.Fatalf("unexpected camera/orientation metadata: %+v", meta.EXIF)
	}
	if meta.EXIF.LensModel != "RF 24-70mm F2.8L" || meta.EXIF.ExposureTime != "1/250" {
		t.Fatalf("unexpected lens/exposure: %+v", meta.EXIF)
	}
	if meta.EXIF.ISO == nil || *meta.EXIF.ISO != 400 || meta.EXIF.FNumber == nil || math.Abs(*meta.EXIF.FNumber-2.8) > 0.01 {
		t.Fatalf("unexpected ISO/FNumber: %+v", meta.EXIF)
	}
	if meta.EXIF.FocalLengthMM == nil || *meta.EXIF.FocalLengthMM != 50 {
		t.Fatalf("unexpected FocalLengthMM: %+v", meta.EXIF.FocalLengthMM)
	}
	if meta.EXIF.GPSLatitude == nil || *meta.EXIF.GPSLatitude >= -23.5 || *meta.EXIF.GPSLatitude <= -23.6 {
		t.Fatalf("unexpected GPSLatitude: %+v", meta.EXIF.GPSLatitude)
	}
	if meta.EXIF.GPSLongitude == nil || *meta.EXIF.GPSLongitude >= -46.6 || *meta.EXIF.GPSLongitude <= -46.7 {
		t.Fatalf("unexpected GPSLongitude: %+v", meta.EXIF.GPSLongitude)
	}
	if meta.TakenAt == nil || meta.TakenAt.Year() != 2026 || meta.TakenAt.Month() != 5 {
		t.Fatalf("unexpected TakenAt: %v", meta.TakenAt)
	}

	plainMeta, err := ext.Extract(t.Context(), "plain.jpg", domain.FormatJPG)
	if err != nil || plainMeta.Width != 32 || plainMeta.Height != 24 || plainMeta.EXIF.Orientation != 1 {
		t.Fatalf("unexpected plain.jpg metadata: %+v err=%v", plainMeta, err)
	}
	pngMeta, err := ext.Extract(t.Context(), "sample.png", domain.FormatPNG)
	if err != nil || pngMeta.Width != 30 || pngMeta.Height != 15 || pngMeta.EXIF.CameraMake != "PNGCam" {
		t.Fatalf("unexpected png metadata: %+v err=%v", pngMeta, err)
	}
	gifMeta, err := ext.Extract(t.Context(), "anim.gif", domain.FormatGIF)
	if err != nil || gifMeta.Width != 18 || gifMeta.Height != 12 {
		t.Fatalf("unexpected gif metadata: %+v err=%v", gifMeta, err)
	}
	webpMeta, err := ext.Extract(t.Context(), "pic.webp", domain.FormatWebP)
	if err != nil || webpMeta.Width != 100 || webpMeta.Height != 50 {
		t.Fatalf("unexpected webp metadata: %+v err=%v", webpMeta, err)
	}
}

func TestEXIFExtractorNativeContainersCR3HEICMP4RAW(t *testing.T) {
	t.Parallel()
	root := t.TempDir()

	cr3Bytes := buildCR3Fixture(t, tiffFixtureSpec{
		orientation:  8,
		makeName:     "Canon",
		modelName:    "EOS R6",
		lensModel:    "RF 50mm F1.8",
		iso:          800,
		fnumNum:      18,
		fnumDen:      10,
		expNum:       1,
		expDen:       500,
		focalNum:     50,
		focalDen:     1,
		width:        60,
		height:       40,
		dateTimeOrig: "2026:07:04 14:00:00",
		includeGPS:   true,
		latRef:       "N",
		latDMS:       [3][2]uint32{{37, 1}, {46, 1}, {0, 1}},
		lngRef:       "W",
		lngDMS:       [3][2]uint32{{122, 1}, {25, 1}, {0, 1}},
	}, encodeTestJPEG(t, 16, 12), encodeTestJPEG(t, 60, 40))
	writeTestFile(t, root, "shot.cr3", cr3Bytes)

	heicTIFF := buildTIFFFixture(tiffFixtureSpec{
		littleEndian: false,
		orientation:  6, // Must NOT swap HEIC dimensions!
		makeName:     "Apple",
		modelName:    "iPhone 16 Pro",
		iso:          64,
		fnumNum:      178,
		fnumDen:      100,
		expNum:       1,
		expDen:       1000,
		focalNum:     24,
		focalDen:     1,
		width:        4032,
		height:       3024,
		dateTimeOrig: "2026:08:01 09:15:30",
	})
	writeTestFile(t, root, "iphone.heic", buildHEICFixture(4032, 3024, heicTIFF, encodeTestJPEG(t, 40, 30)))
	writeTestFile(t, root, "clip.mp4", buildMP4Fixture(0, 1000, 12500, 1920, 1080))
	writeTestFile(t, root, "clip64.mov", buildMP4Fixture(1, 600, 3600, 3840, 2160))
	writeTestFile(t, root, "raw.dng", buildTIFFFixture(tiffFixtureSpec{
		littleEndian: true,
		orientation:  5,
		makeName:     "Leica",
		modelName:    "Q3",
		iso:          200,
		fnumNum:      20,
		fnumDen:      10,
		expNum:       1,
		expDen:       125,
		focalNum:     28,
		focalDen:     1,
		width:        100,
		height:       50,
		dateTimeOrig: "2026:03:10 16:20:00",
	}))

	ext := media.NewEXIFExtractor(root).WithExifToolPath("")

	cr3Meta, err := ext.Extract(t.Context(), "shot.cr3", domain.FormatCR3)
	if err != nil {
		t.Fatalf("Extract cr3 failed: %v", err)
	}
	if cr3Meta.Width != 40 || cr3Meta.Height != 60 || cr3Meta.EXIF.Orientation != 8 {
		t.Fatalf("expected swapped CR3 dims 40x60 orient 8, got %dx%d orient=%d", cr3Meta.Width, cr3Meta.Height, cr3Meta.EXIF.Orientation)
	}
	if cr3Meta.EXIF.CameraMake != "Canon" || cr3Meta.EXIF.GPSLongitude == nil || *cr3Meta.EXIF.GPSLongitude > -122 {
		t.Fatalf("unexpected CR3 metadata: %+v", cr3Meta)
	}

	heicMeta, err := ext.Extract(t.Context(), "iphone.heic", domain.FormatHEIC)
	if err != nil {
		t.Fatalf("Extract heic failed: %v", err)
	}
	if heicMeta.Width != 4032 || heicMeta.Height != 3024 {
		t.Fatalf("HEIC dimensions must NOT be swapped (no double-rotation): got %dx%d", heicMeta.Width, heicMeta.Height)
	}
	if heicMeta.EXIF.CameraMake != "Apple" || heicMeta.EXIF.CameraModel != "iPhone 16 Pro" {
		t.Fatalf("unexpected HEIC EXIF: %+v", heicMeta.EXIF)
	}

	mp4Meta, err := ext.Extract(t.Context(), "clip.mp4", domain.FormatMP4)
	if err != nil || mp4Meta.DurationSec != 12.5 || mp4Meta.Width != 1920 || mp4Meta.Height != 1080 {
		t.Fatalf("unexpected mp4 metadata: %+v err=%v", mp4Meta, err)
	}
	movMeta, err := ext.Extract(t.Context(), "clip64.mov", domain.FormatMOV)
	if err != nil || movMeta.DurationSec != 6.0 || movMeta.Width != 3840 || movMeta.Height != 2160 {
		t.Fatalf("unexpected mov64 metadata: %+v err=%v", movMeta, err)
	}
	dngMeta, err := ext.Extract(t.Context(), "raw.dng", domain.FormatDNG)
	if err != nil || dngMeta.Width != 50 || dngMeta.Height != 100 || dngMeta.EXIF.CameraMake != "Leica" {
		t.Fatalf("unexpected dng metadata: %+v err=%v", dngMeta, err)
	}
}

func TestEXIFExtractorMockCLIAndTimeoutFallback(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	binDir := t.TempDir()

	writeTestFile(t, root, "photo.jpg", encodeTestJPEG(t, 30, 20))
	writeTestFile(t, root, "photo.heic", buildHEICFixture(100, 80, nil, encodeTestJPEG(t, 20, 10)))

	okScript := writeExecScript(t, binDir, "exiftool-ok", `
printf '%s\n' '[{
  "ImageWidth": 4000,
  "ImageHeight": 3000,
  "Orientation": 6,
  "Make": "Sony",
  "Model": "A7IV",
  "LensModel": "FE 35mm F1.4",
  "ISO": 160,
  "FNumber": 1.4,
  "ExposureTime": 0.004,
  "FocalLength": 35.0,
  "GPSLatitude": 48.8566,
  "GPSLatitudeRef": "N",
  "GPSLongitude": 2.3522,
  "GPSLongitudeRef": "W",
  "DateTimeOriginal": "2026:06:01 12:00:00",
  "Duration": 14.5
}]'
`)
	failScript := writeExecScript(t, binDir, "exiftool-fail", "exit 1")
	slowScript := writeExecScript(t, binDir, "exiftool-slow", "exec sleep 2")

	extOK := media.NewEXIFExtractor(root).WithExifToolPath(okScript).WithTimeout(5 * time.Second)
	meta, err := extOK.Extract(t.Context(), "photo.jpg", domain.FormatJPG)
	if err != nil {
		t.Fatalf("mock exiftool failed: %v", err)
	}
	if meta.Width != 3000 || meta.Height != 4000 || meta.EXIF.CameraMake != "Sony" || meta.EXIF.ExposureTime != "1/250" {
		t.Fatalf("unexpected exiftool parsed metadata: %+v", meta)
	}
	if meta.EXIF.GPSLongitude == nil || *meta.EXIF.GPSLongitude > -2.35 {
		t.Fatalf("expected negative GPSLongitude for W ref, got %+v", meta.EXIF.GPSLongitude)
	}
	if meta.DurationSec != 14.5 {
		t.Fatalf("expected duration 14.5, got %v", meta.DurationSec)
	}

	// HEIC via exiftool must NOT swap width/height.
	heicMeta, err := extOK.Extract(t.Context(), "photo.heic", domain.FormatHEIC)
	if err != nil || heicMeta.Width != 4000 || heicMeta.Height != 3000 {
		t.Fatalf("expected unswapped HEIC 4000x3000, got %+v err=%v", heicMeta, err)
	}

	// Failing exiftool falls back to native parser.
	extFail := media.NewEXIFExtractor(root).WithExifToolPath(failScript)
	fallbackMeta, err := extFail.Extract(t.Context(), "photo.jpg", domain.FormatJPG)
	if err != nil || fallbackMeta.Width != 30 || fallbackMeta.Height != 20 {
		t.Fatalf("expected pure-Go fallback 30x20, got %+v err=%v", fallbackMeta, err)
	}

	// Timeout returns DeadlineExceeded promptly.
	extSlow := media.NewEXIFExtractor(root).WithExifToolPath(slowScript).WithTimeout(30 * time.Millisecond)
	start := time.Now()
	_, err = extSlow.Extract(t.Context(), "photo.jpg", domain.FormatJPG)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("expected context.DeadlineExceeded, got %v", err)
	}
	if time.Since(start) > time.Second {
		t.Fatalf("timeout took too long: %v", time.Since(start))
	}
}

func TestEXIFExtractorErrorsAndSecurity(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	outside := t.TempDir()
	writeTestFile(t, root, "a.jpg", encodeTestJPEG(t, 10, 10))
	extTarget := writeTestFile(t, outside, "out.jpg", encodeTestJPEG(t, 10, 10))
	if err := os.Symlink(extTarget, filepath.Join(root, "sym.jpg")); err != nil {
		t.Skipf("symlink unsupported: %v", err)
	}

	ext := media.NewEXIFExtractor(root).WithExifToolPath("")

	if _, err := ext.Extract(t.Context(), "a.jpg", domain.MediaFormat("bmp")); !errors.Is(err, domain.ErrUnsupportedFormat) {
		t.Fatalf("expected ErrUnsupportedFormat, got %v", err)
	}
	if _, err := ext.Extract(t.Context(), "../out.jpg", domain.FormatJPG); err == nil {
		t.Fatal("expected traversal error")
	}
	if _, err := ext.Extract(t.Context(), "sym.jpg", domain.FormatJPG); err == nil {
		t.Fatal("expected symlink escape error")
	}

	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := ext.Extract(ctx, "a.jpg", domain.FormatJPG); !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context.Canceled, got %v", err)
	}

	// Verify helper behavior on big-endian malformed TIFF doesn't panic.
	badTIFF := make([]byte, 16)
	copy(badTIFF, "MM")
	binary.BigEndian.PutUint16(badTIFF[2:4], 42)
	binary.BigEndian.PutUint32(badTIFF[4:8], 9999)
	writeTestFile(t, root, "bad.dng", badTIFF)
	if _, err := ext.Extract(t.Context(), "bad.dng", domain.FormatDNG); err != nil {
		t.Fatalf("malformed TIFF should safely return default metadata, got %v", err)
	}
}

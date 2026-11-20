package media

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"image"
	"image/color"
	_ "image/gif"  // Register standard GIF decoder.
	_ "image/jpeg" // Register standard JPEG decoder.
	_ "image/png"  // Register standard PNG decoder.
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync/atomic"
	"time"

	"github.com/lucastheo/photos/internal/domain"
)

var lastTmpNano atomic.Int64

const (
	defaultToolTimeout = 15 * time.Second
	thumbMaxDim        = 400
	previewMaxDim      = 2560
	thumbJPEGQuality   = 85
	previewJPEGQuality = 90
)

// PreviewExtractor generates cached 400px thumbnails and 2560px web previews
// using a memory-bounded worker pool, os.Root confinement, and atomic writes.
type PreviewExtractor struct {
	libraryPath    string
	cachePath      string
	workers        int
	timeout        time.Duration
	exiftoolBin    string
	heifConvertBin string
	ffmpegBin      string
	sem            chan struct{}
}

// NewPreviewExtractor creates a PreviewExtractor reading from libraryPath and writing
// cached JPEGs to cachePath. Non-positive workers default to min(4, max(1, NumCPU/2)).
func NewPreviewExtractor(libraryPath, cachePath string, workers int) *PreviewExtractor {
	if workers <= 0 {
		workers = min(4, max(1, runtime.NumCPU()/2))
	}
	exifBin, _ := exec.LookPath("exiftool")
	heifBin, _ := exec.LookPath("heif-convert")
	ffBin, _ := exec.LookPath("ffmpeg")
	return &PreviewExtractor{
		libraryPath:    libraryPath,
		cachePath:      cachePath,
		workers:        workers,
		timeout:        defaultToolTimeout,
		exiftoolBin:    exifBin,
		heifConvertBin: heifBin,
		ffmpegBin:      ffBin,
		sem:            make(chan struct{}, workers),
	}
}

// WithTools overrides the paths to exiftool, heif-convert, and ffmpeg binaries.
// Passing empty strings disables the corresponding external subprocess tool.
func (p *PreviewExtractor) WithTools(exiftool, heifConvert, ffmpeg string) *PreviewExtractor {
	p.exiftoolBin = strings.TrimSpace(exiftool)
	p.heifConvertBin = strings.TrimSpace(heifConvert)
	p.ffmpegBin = strings.TrimSpace(ffmpeg)
	return p
}

// WithTimeout configures the per-file timeout for external media subprocesses.
func (p *PreviewExtractor) WithTimeout(d time.Duration) *PreviewExtractor {
	if d > 0 {
		p.timeout = d
	}
	return p
}

// ThumbnailFilename returns the deterministic cache filename for a 400px grid thumbnail.
func ThumbnailFilename(contentHash string) string {
	return strings.TrimSpace(contentHash) + "_thumb.jpg"
}

// PreviewFilename returns the deterministic cache filename for a 2560px web preview.
func PreviewFilename(contentHash string) string {
	return strings.TrimSpace(contentHash) + "_preview.jpg"
}

// EnsureCache ensures both <contentHash>_thumb.jpg and <contentHash>_preview.jpg exist
// in cachePath, skipping generation if both files are already cached on disk.
func (p *PreviewExtractor) EnsureCache(
	ctx context.Context,
	relativePath, contentHash string,
	format domain.MediaFormat,
	orientation int,
) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	thumbName, previewName, err := validateCacheTargets(contentHash)
	if err != nil {
		return err
	}
	if _, err := localizeLibraryPath(relativePath); err != nil {
		return err
	}
	cacheRoot, err := openCacheRoot(p.cachePath)
	if err != nil {
		return err
	}
	defer func() { _ = cacheRoot.Close() }()

	if cachePairExists(cacheRoot, thumbName, previewName) {
		return nil
	}
	return p.generateWithWorkerSlot(ctx, cacheRoot, relativePath, thumbName, previewName, format, orientation)
}

func (p *PreviewExtractor) generateWithWorkerSlot(
	ctx context.Context,
	cacheRoot *os.Root,
	relativePath, thumbName, previewName string,
	format domain.MediaFormat,
	orientation int,
) error {
	select {
	case p.sem <- struct{}{}:
		defer func() { <-p.sem }()
	case <-ctx.Done():
		return ctx.Err()
	}

	if cachePairExists(cacheRoot, thumbName, previewName) {
		return nil
	}

	img, err := p.loadSourceImage(ctx, relativePath, format)
	if err != nil {
		return err
	}
	oriented := ApplyOrientation(img, format, orientation)
	return writeMissingCacheVariants(cacheRoot, oriented, thumbName, previewName)
}

func writeMissingCacheVariants(cacheRoot *os.Root, oriented image.Image, thumbName, previewName string) error {
	if !cacheFileExists(cacheRoot, thumbName) {
		thumbImg := resizeMaxDim(oriented, thumbMaxDim)
		if err := writeJPEGAtomic(cacheRoot, thumbName, thumbImg, thumbJPEGQuality); err != nil {
			return fmt.Errorf("write thumbnail %q: %w", thumbName, err)
		}
	}
	if !cacheFileExists(cacheRoot, previewName) {
		previewImg := resizeMaxDim(oriented, previewMaxDim)
		if err := writeJPEGAtomic(cacheRoot, previewName, previewImg, previewJPEGQuality); err != nil {
			return fmt.Errorf("write preview %q: %w", previewName, err)
		}
	}
	return nil
}

func validateCacheTargets(contentHash string) (string, string, error) {
	trimmed := strings.TrimSpace(contentHash)
	if trimmed == "" || strings.ContainsAny(trimmed, `/\`) {
		return "", "", fmt.Errorf("invalid content hash %q", contentHash)
	}
	thumb := ThumbnailFilename(trimmed)
	preview := PreviewFilename(trimmed)
	if _, err := filepath.Localize(thumb); err != nil {
		return "", "", fmt.Errorf("invalid thumbnail filename %q: %w", thumb, err)
	}
	if _, err := filepath.Localize(preview); err != nil {
		return "", "", fmt.Errorf("invalid preview filename %q: %w", preview, err)
	}
	return thumb, preview, nil
}

func openCacheRoot(cachePath string) (*os.Root, error) {
	if strings.TrimSpace(cachePath) == "" {
		return nil, errors.New("cache path cannot be empty")
	}
	if err := os.MkdirAll(cachePath, 0o755); err != nil {
		return nil, fmt.Errorf("create cache directory: %w", err)
	}
	root, err := os.OpenRoot(cachePath)
	if err != nil {
		return nil, fmt.Errorf("open cache root: %w", err)
	}
	return root, nil
}

func cachePairExists(cacheRoot *os.Root, thumbName, previewName string) bool {
	return cacheFileExists(cacheRoot, thumbName) && cacheFileExists(cacheRoot, previewName)
}

func cacheFileExists(cacheRoot *os.Root, name string) bool {
	info, err := cacheRoot.Stat(name)
	return err == nil && info.Mode().IsRegular() && info.Size() > 0
}

func writeJPEGAtomic(cacheRoot *os.Root, finalName string, img image.Image, quality int) error {
	payload, err := encodeJPEGBytes(img, quality)
	if err != nil {
		return err
	}
	tmpName := fmt.Sprintf("%s.tmp.%d", finalName, nextUniqueNano())
	f, err := cacheRoot.OpenFile(tmpName, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
	if err != nil {
		return err
	}
	if err := writeSyncClose(f, payload); err != nil {
		_ = cacheRoot.Remove(tmpName)
		return err
	}
	if err := cacheRoot.Rename(tmpName, finalName); err != nil {
		_ = cacheRoot.Remove(tmpName)
		return err
	}
	return nil
}

func nextUniqueNano() int64 {
	for {
		now := time.Now().UnixNano()
		prev := lastTmpNano.Load()
		next := now
		if next <= prev {
			next = prev + 1
		}
		if lastTmpNano.CompareAndSwap(prev, next) {
			return next
		}
	}
}

func writeSyncClose(f *os.File, payload []byte) error {
	if _, err := f.Write(payload); err != nil {
		_ = f.Close()
		return err
	}
	if err := f.Sync(); err != nil {
		_ = f.Close()
		return err
	}
	return f.Close()
}

func (p *PreviewExtractor) loadSourceImage(
	ctx context.Context,
	relativePath string,
	format domain.MediaFormat,
) (image.Image, error) {
	libRoot, err := openLibraryRoot(p.libraryPath)
	if err != nil {
		return nil, err
	}
	defer func() { _ = libRoot.Close() }()

	f, local, err := openRegularInRoot(libRoot, relativePath)
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()

	fullPath := resolveSubprocessPath(p.libraryPath, local)
	mediaType, ok := domain.MediaTypeForFormat(format)
	if !ok {
		return nil, fmt.Errorf("%w: %q", domain.ErrUnsupportedFormat, format)
	}
	if mediaType == domain.MediaTypeVideo {
		return p.loadVideoFrame(ctx, f, fullPath)
	}
	if domain.IsRAWFormat(format) {
		return p.loadRAWPreview(ctx, f, fullPath)
	}
	if format == domain.FormatHEIC || format == domain.FormatHEIF {
		return p.loadHEICPreview(ctx, f, fullPath)
	}
	return p.loadStandardImage(ctx, f, fullPath)
}

func (p *PreviewExtractor) loadRAWPreview(ctx context.Context, f *os.File, fullPath string) (image.Image, error) {
	if p.exiftoolBin != "" {
		if img, err := p.extractRAWViaExifTool(ctx, fullPath); err != nil {
			if isCtxTimeoutOrCancel(err) {
				return nil, err
			}
		} else {
			return img, nil
		}
	}
	if p.ffmpegBin != "" {
		if img, err := p.extractFrameViaFFmpeg(ctx, fullPath, false); err != nil {
			if isCtxTimeoutOrCancel(err) {
				return nil, err
			}
		} else {
			return img, nil
		}
	}
	return decodeEmbeddedJPEGFromFile(f)
}

func (p *PreviewExtractor) loadHEICPreview(ctx context.Context, f *os.File, fullPath string) (image.Image, error) {
	if p.heifConvertBin != "" {
		if img, err := p.extractViaHeifConvert(ctx, fullPath); err != nil {
			if isCtxTimeoutOrCancel(err) {
				return nil, err
			}
		} else {
			return img, nil
		}
	}
	if p.ffmpegBin != "" {
		if img, err := p.extractFrameViaFFmpeg(ctx, fullPath, false); err != nil {
			if isCtxTimeoutOrCancel(err) {
				return nil, err
			}
		} else {
			return img, nil
		}
	}
	return decodeEmbeddedJPEGFromFile(f)
}

func (p *PreviewExtractor) loadStandardImage(ctx context.Context, f *os.File, fullPath string) (image.Image, error) {
	if _, err := f.Seek(0, io.SeekStart); err == nil {
		if img, _, decErr := image.Decode(f); decErr == nil {
			return img, nil
		}
	}
	if p.ffmpegBin != "" {
		return p.extractFrameViaFFmpeg(ctx, fullPath, false)
	}
	return nil, fmt.Errorf("decode image %q: unsupported or corrupt image bytes", fullPath)
}

func (p *PreviewExtractor) loadVideoFrame(ctx context.Context, f *os.File, fullPath string) (image.Image, error) {
	if p.ffmpegBin != "" {
		if img, err := p.extractFrameViaFFmpeg(ctx, fullPath, true); err != nil {
			if isCtxTimeoutOrCancel(err) {
				return nil, err
			}
		} else {
			return img, nil
		}
	}
	if img, err := decodeEmbeddedJPEGFromFile(f); err == nil {
		return img, nil
	}
	return synthesizeVideoPoster(f), nil
}

func (p *PreviewExtractor) extractRAWViaExifTool(ctx context.Context, fullPath string) (image.Image, error) {
	for _, tag := range []string{"-PreviewImage", "-JpgFromRaw"} {
		out, err := p.runToolBytes(ctx, p.exiftoolBin, "-b", tag, fullPath)
		if err != nil {
			if isCtxTimeoutOrCancel(err) {
				return nil, err
			}
			continue
		}
		if img, _, decErr := image.Decode(bytes.NewReader(out)); decErr == nil {
			return img, nil
		}
	}
	return nil, errors.New("exiftool returned no decodable RAW preview")
}

func (p *PreviewExtractor) extractViaHeifConvert(ctx context.Context, fullPath string) (image.Image, error) {
	tmpDir, err := os.MkdirTemp("", "photos-heif-*")
	if err != nil {
		return nil, fmt.Errorf("create heif temp dir: %w", err)
	}
	defer func() { _ = os.RemoveAll(tmpDir) }()

	outFile := filepath.Join(tmpDir, "preview.jpg")
	if _, err := p.runToolBytes(ctx, p.heifConvertBin, fullPath, outFile); err != nil {
		return nil, err
	}
	return decodeHeifConvertOutput(tmpDir, outFile)
}

func decodeHeifConvertOutput(tmpDir, primary string) (image.Image, error) {
	if img, ok := tryDecodeFile(primary); ok {
		return img, nil
	}
	matches, _ := filepath.Glob(filepath.Join(tmpDir, "preview*.jpg"))
	for _, m := range matches {
		if img, ok := tryDecodeFile(m); ok {
			return img, nil
		}
	}
	return nil, errors.New("heif-convert produced no readable JPEG output")
}

func tryDecodeFile(path string) (image.Image, bool) {
	f, err := os.Open(path)
	if err != nil {
		return nil, false
	}
	defer func() { _ = f.Close() }()
	img, _, err := image.Decode(f)
	return img, err == nil
}

func (p *PreviewExtractor) extractFrameViaFFmpeg(
	ctx context.Context,
	fullPath string,
	seekFirst bool,
) (image.Image, error) {
	args := []string{"-hide_banner", "-loglevel", "error", "-y"}
	if seekFirst {
		args = append(args, "-ss", "00:00:00.500")
	}
	args = append(args, "-i", fullPath, "-vframes", "1", "-f", "image2pipe", "-vcodec", "mjpeg", "pipe:1")
	out, err := p.runToolBytes(ctx, p.ffmpegBin, args...)
	if err == nil {
		if img, _, decErr := image.Decode(bytes.NewReader(out)); decErr == nil {
			return img, nil
		}
	}
	if err != nil && isCtxTimeoutOrCancel(err) {
		return nil, err
	}
	if seekFirst {
		return p.extractFrameViaFFmpeg(ctx, fullPath, false)
	}
	if err != nil {
		return nil, err
	}
	return nil, errors.New("ffmpeg produced empty or undecodable frame")
}

func (p *PreviewExtractor) runToolBytes(ctx context.Context, bin string, args ...string) ([]byte, error) {
	tctx, cancel := context.WithTimeout(ctx, p.timeout)
	defer cancel()

	cmd := exec.CommandContext(tctx, bin, args...)
	cmd.WaitDelay = 50 * time.Millisecond
	out, err := cmd.Output()
	if tctx.Err() != nil {
		return nil, fmt.Errorf("%s timeout or canceled: %w", filepath.Base(bin), tctx.Err())
	}
	if err != nil {
		return nil, fmt.Errorf("%s failed: %w", filepath.Base(bin), err)
	}
	return out, nil
}

func isCtxTimeoutOrCancel(err error) bool {
	return errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled)
}

func decodeEmbeddedJPEGFromFile(f *os.File) (image.Image, error) {
	if _, err := f.Seek(0, io.SeekStart); err != nil {
		return nil, fmt.Errorf("seek source file: %w", err)
	}
	data, err := io.ReadAll(io.LimitReader(f, maxEmbeddedScanBytes))
	if err != nil {
		return nil, fmt.Errorf("read source bytes: %w", err)
	}
	jpegBytes, ok := findLargestEmbeddedJPEG(data)
	if !ok {
		return nil, errors.New("no embedded JPEG preview found")
	}
	img, _, err := image.Decode(bytes.NewReader(jpegBytes))
	if err != nil {
		return nil, fmt.Errorf("decode embedded JPEG preview: %w", err)
	}
	return img, nil
}

func synthesizeVideoPoster(f *os.File) image.Image {
	w, h := 640, 360
	if _, err := f.Seek(0, io.SeekStart); err == nil {
		if meta, parseErr := parseISOBMFFMetadata(f, domain.FormatMP4); parseErr == nil && meta.Width > 0 && meta.Height > 0 {
			w, h = scaledDimensions(meta.Width, meta.Height, 640)
		}
	}
	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	fillPosterBackdrop(img, w, h)
	drawPlayTriangle(img, w, h)
	return img
}

func fillPosterBackdrop(img *image.NRGBA, w, h int) {
	for y := range h {
		shade := uint8(24 + (y*24)/max(1, h))
		rowColor := color.NRGBA{R: shade, G: shade + 4, B: shade + 12, A: 255}
		for x := range w {
			img.SetNRGBA(x, y, rowColor)
		}
	}
}

func drawPlayTriangle(img *image.NRGBA, w, h int) {
	cx, cy := w/2, h/2
	halfH := max(12, min(w, h)/6)
	triW := max(14, (halfH*8)/5)
	left := cx - triW/2
	white := color.NRGBA{R: 240, G: 244, B: 248, A: 255}

	for dy := -halfH; dy <= halfH; dy++ {
		span := (triW * (halfH - absInt(dy))) / halfH
		for dx := range span + 1 {
			px, py := left+dx, cy+dy
			if px >= 0 && px < w && py >= 0 && py < h {
				img.SetNRGBA(px, py, white)
			}
		}
	}
}

func absInt(v int) int {
	if v < 0 {
		return -v
	}
	return v
}

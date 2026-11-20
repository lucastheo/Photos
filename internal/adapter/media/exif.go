package media

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"io"
	"math"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"github.com/lucastheo/photos/internal/domain"
)

// EXIFExtractor reads dimensions, video duration, capture timestamps, camera
// exposure settings, orientation, and GPS coordinates from media files.
type EXIFExtractor struct {
	libraryPath string
	exiftoolBin string
	timeout     time.Duration
}

// NewEXIFExtractor creates an EXIFExtractor confined to libraryPath.
// If exiftool is available on PATH, it is enabled by default with a 15s timeout.
func NewEXIFExtractor(libraryPath string) *EXIFExtractor {
	bin, _ := exec.LookPath("exiftool")
	return &EXIFExtractor{
		libraryPath: libraryPath,
		exiftoolBin: bin,
		timeout:     defaultToolTimeout,
	}
}

// WithExifToolPath overrides the exiftool binary path. Passing an empty string
// disables exiftool and forces pure-Go native metadata extraction.
func (e *EXIFExtractor) WithExifToolPath(bin string) *EXIFExtractor {
	e.exiftoolBin = strings.TrimSpace(bin)
	return e
}

// WithTimeout configures the per-file subprocess timeout for exiftool execution.
func (e *EXIFExtractor) WithTimeout(d time.Duration) *EXIFExtractor {
	if d > 0 {
		e.timeout = d
	}
	return e
}

// Extract extracts normalized MediaMetadata for relativePath within the library root.
func (e *EXIFExtractor) Extract(
	ctx context.Context,
	relativePath string,
	format domain.MediaFormat,
) (domain.MediaMetadata, error) {
	if err := ctx.Err(); err != nil {
		return domain.MediaMetadata{}, err
	}
	if !format.IsValid() {
		return domain.MediaMetadata{}, fmt.Errorf("%w: %q", domain.ErrUnsupportedFormat, format)
	}

	root, err := openLibraryRoot(e.libraryPath)
	if err != nil {
		return domain.MediaMetadata{}, err
	}
	defer func() { _ = root.Close() }()

	f, local, err := openRegularInRoot(root, relativePath)
	if err != nil {
		return domain.MediaMetadata{}, err
	}
	defer func() { _ = f.Close() }()

	meta, err := e.extractMetadata(ctx, f, resolveSubprocessPath(e.libraryPath, local), format)
	if err != nil {
		return domain.MediaMetadata{}, err
	}
	normalizeExtractedMetadata(&meta, format)
	return meta, nil
}

func (e *EXIFExtractor) extractMetadata(
	ctx context.Context,
	f *os.File,
	fullPath string,
	format domain.MediaFormat,
) (domain.MediaMetadata, error) {
	if e.exiftoolBin != "" {
		meta, err := e.runExifTool(ctx, fullPath)
		if err == nil {
			fillMissingDimensionsFromFile(f, format, &meta)
			return meta, nil
		}
		if isCtxTimeoutOrCancel(err) {
			return domain.MediaMetadata{}, err
		}
	}
	return extractNativeMetadata(f, format)
}

func (e *EXIFExtractor) runExifTool(ctx context.Context, fullPath string) (domain.MediaMetadata, error) {
	tctx, cancel := context.WithTimeout(ctx, e.timeout)
	defer cancel()

	cmd := exec.CommandContext(tctx, e.exiftoolBin, "-j", "-n", fullPath)
	cmd.WaitDelay = 50 * time.Millisecond
	out, err := cmd.Output()
	if tctx.Err() != nil {
		return domain.MediaMetadata{}, fmt.Errorf("exiftool timeout or canceled: %w", tctx.Err())
	}
	if err != nil {
		return domain.MediaMetadata{}, fmt.Errorf("exiftool failed: %w", err)
	}
	return parseExifToolJSON(out)
}

func parseExifToolJSON(raw []byte) (domain.MediaMetadata, error) {
	var items []map[string]json.RawMessage
	if err := json.Unmarshal(bytes.TrimSpace(raw), &items); err != nil {
		return domain.MediaMetadata{}, fmt.Errorf("decode exiftool json: %w", err)
	}
	if len(items) == 0 {
		return domain.MediaMetadata{}, errors.New("exiftool returned empty array")
	}
	obj := items[0]
	var meta domain.MediaMetadata
	populateExifToolBasics(obj, &meta)
	populateExifToolExposure(obj, &meta)
	populateExifToolLocationAndTime(obj, &meta)
	return meta, nil
}

func populateExifToolBasics(obj map[string]json.RawMessage, meta *domain.MediaMetadata) {
	if w, ok := rawInt(obj["ImageWidth"]); ok && w > 0 {
		meta.Width = w
	}
	if h, ok := rawInt(obj["ImageHeight"]); ok && h > 0 {
		meta.Height = h
	}
	if ori, ok := rawOrientation(obj["Orientation"]); ok {
		meta.EXIF.Orientation = ori
	}
	meta.EXIF.CameraMake = rawString(obj["Make"])
	meta.EXIF.CameraModel = rawString(obj["Model"])
	meta.EXIF.LensModel = rawString(obj["LensModel"])
}

func populateExifToolExposure(obj map[string]json.RawMessage, meta *domain.MediaMetadata) {
	if iso, ok := rawInt(obj["ISO"]); ok && iso > 0 {
		meta.EXIF.ISO = &iso
	}
	if fn, ok := rawFloat(obj["FNumber"]); ok && fn > 0 {
		meta.EXIF.FNumber = &fn
	}
	if fl, ok := rawFloat(obj["FocalLength"]); ok && fl > 0 {
		meta.EXIF.FocalLengthMM = &fl
	}
	meta.EXIF.ExposureTime = parseExifToolExposure(obj["ExposureTime"])
	if dur, ok := rawDuration(obj["Duration"]); ok && dur > 0 {
		meta.DurationSec = dur
	}
}

func populateExifToolLocationAndTime(obj map[string]json.RawMessage, meta *domain.MediaMetadata) {
	if lat, ok := rawFloat(obj["GPSLatitude"]); ok {
		signed := applyCoordSign(lat, rawString(obj["GPSLatitudeRef"]), "S")
		if signed >= -90 && signed <= 90 {
			meta.EXIF.GPSLatitude = &signed
		}
	}
	if lng, ok := rawFloat(obj["GPSLongitude"]); ok {
		signed := applyCoordSign(lng, rawString(obj["GPSLongitudeRef"]), "W")
		if signed >= -180 && signed <= 180 {
			meta.EXIF.GPSLongitude = &signed
		}
	}
	for _, key := range []string{"DateTimeOriginal", "CreateDate"} {
		if ts := parseEXIFTimestamp(rawString(obj[key])); ts != nil {
			meta.TakenAt = ts
			break
		}
	}
}

func fillMissingDimensionsFromFile(f *os.File, format domain.MediaFormat, meta *domain.MediaMetadata) {
	if meta.Width > 0 && meta.Height > 0 {
		return
	}
	native, err := extractNativeMetadata(f, format)
	if err != nil {
		return
	}
	if meta.Width == 0 {
		meta.Width = native.Width
	}
	if meta.Height == 0 {
		meta.Height = native.Height
	}
	if meta.DurationSec == 0 {
		meta.DurationSec = native.DurationSec
	}
}

func extractNativeMetadata(f *os.File, format domain.MediaFormat) (domain.MediaMetadata, error) {
	if _, err := f.Seek(0, io.SeekStart); err != nil {
		return domain.MediaMetadata{}, fmt.Errorf("seek source file: %w", err)
	}
	switch format {
	case domain.FormatJPG:
		return extractJPEGMetadata(f)
	case domain.FormatPNG, domain.FormatGIF:
		return extractStdImageConfig(f, format)
	case domain.FormatWebP:
		return extractWebPMetadata(f)
	case domain.FormatCR3, domain.FormatHEIC, domain.FormatHEIF, domain.FormatAVIF,
		domain.FormatMP4, domain.FormatMOV, domain.FormatM4V:
		return parseISOBMFFMetadata(f, format)
	case domain.FormatCR2, domain.FormatDNG, domain.FormatNEF, domain.FormatARW,
		domain.FormatORF, domain.FormatRW2:
		return extractRAWMetadata(f)
	default:
		return domain.MediaMetadata{}, nil
	}
}

func extractStdImageConfig(f *os.File, format domain.MediaFormat) (domain.MediaMetadata, error) {
	var meta domain.MediaMetadata
	if format == domain.FormatPNG {
		data, err := io.ReadAll(io.LimitReader(f, maxMetadataScanBytes))
		if err == nil {
			parsePNGExifChunk(data, &meta)
		}
		if _, err := f.Seek(0, io.SeekStart); err != nil {
			return meta, fmt.Errorf("seek png: %w", err)
		}
	}
	cfg, _, err := image.DecodeConfig(f)
	if err != nil {
		return meta, fmt.Errorf("decode image config: %w", err)
	}
	meta.Width = cfg.Width
	meta.Height = cfg.Height
	return meta, nil
}

func normalizeExtractedMetadata(meta *domain.MediaMetadata, format domain.MediaFormat) {
	if meta.EXIF.Orientation < 1 || meta.EXIF.Orientation > 8 {
		meta.EXIF.Orientation = 1
	}
	if format == domain.FormatHEIC || format == domain.FormatHEIF {
		return
	}
	if meta.EXIF.Orientation >= 5 && meta.EXIF.Orientation <= 8 && meta.Width > 0 && meta.Height > 0 {
		meta.Width, meta.Height = meta.Height, meta.Width
	}
}

func rawString(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		return strings.TrimSpace(s)
	}
	var f float64
	if err := json.Unmarshal(raw, &f); err == nil && !math.IsNaN(f) && !math.IsInf(f, 0) {
		return strconv.FormatFloat(f, 'f', -1, 64)
	}
	return ""
}

func rawInt(raw json.RawMessage) (int, bool) {
	if f, ok := rawFloat(raw); ok {
		return int(math.Round(f)), true
	}
	return 0, false
}

func rawFloat(raw json.RawMessage) (float64, bool) {
	if len(raw) == 0 {
		return 0, false
	}
	var f float64
	if err := json.Unmarshal(raw, &f); err == nil {
		if math.IsNaN(f) || math.IsInf(f, 0) {
			return 0, false
		}
		return f, true
	}
	var s string
	if err := json.Unmarshal(raw, &s); err != nil {
		return 0, false
	}
	return parseLeadingFloat(s)
}

func parseLeadingFloat(s string) (float64, bool) {
	fields := strings.Fields(strings.TrimSpace(s))
	if len(fields) == 0 {
		return 0, false
	}
	val, err := strconv.ParseFloat(fields[0], 64)
	if err != nil || math.IsNaN(val) || math.IsInf(val, 0) {
		return 0, false
	}
	return val, true
}

func rawOrientation(raw json.RawMessage) (int, bool) {
	if v, ok := rawInt(raw); ok && v >= 1 && v <= 8 {
		return v, true
	}
	s := strings.ToLower(rawString(raw))
	switch {
	case strings.Contains(s, "90 cw"):
		return 6, true
	case strings.Contains(s, "180"):
		return 3, true
	case strings.Contains(s, "270 cw"):
		return 8, true
	default:
		return 0, false
	}
}

func parseExifToolExposure(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		return strings.TrimSpace(s)
	}
	var f float64
	if err := json.Unmarshal(raw, &f); err == nil && f > 0 && !math.IsNaN(f) && !math.IsInf(f, 0) {
		return formatExposureFloat(f)
	}
	return ""
}

func formatExposureFloat(sec float64) string {
	if sec <= 0 {
		return ""
	}
	if sec < 1 {
		denom := math.Round(1.0 / sec)
		if denom > 1 && math.Abs(sec-1.0/denom) < 1e-4 {
			return fmt.Sprintf("1/%d", int(denom))
		}
	}
	return strconv.FormatFloat(sec, 'g', 4, 64)
}

func rawDuration(raw json.RawMessage) (float64, bool) {
	if f, ok := rawFloat(raw); ok {
		return f, true
	}
	s := rawString(raw)
	parts := strings.Split(s, ":")
	if len(parts) != 3 {
		return 0, false
	}
	h, errH := strconv.ParseFloat(parts[0], 64)
	m, errM := strconv.ParseFloat(parts[1], 64)
	sec, errS := strconv.ParseFloat(parts[2], 64)
	if errH != nil || errM != nil || errS != nil {
		return 0, false
	}
	return h*3600 + m*60 + sec, true
}

func applyCoordSign(val float64, ref, negPrefix string) float64 {
	if strings.HasPrefix(strings.ToUpper(strings.TrimSpace(ref)), negPrefix) && val > 0 {
		return -val
	}
	return val
}

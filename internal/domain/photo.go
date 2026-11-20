package domain

import (
	"cmp"
	"path"
	"slices"
	"strings"
	"time"

	"golang.org/x/text/unicode/norm"
)

// MediaType classifies whether a media asset is a still photo or a video.
type MediaType string

const (
	// MediaTypePhoto represents a still image (standard, RAW, or HEIF/HEIC).
	MediaTypePhoto MediaType = "photo"

	// MediaTypeVideo represents a video asset.
	MediaTypeVideo MediaType = "video"
)

// IsValid reports whether m is a supported MediaType value.
func (m MediaType) IsValid() bool {
	return m == MediaTypePhoto || m == MediaTypeVideo
}

// MediaFormat identifies the canonical lowercase file format of a media file.
type MediaFormat string

const (
	// FormatJPG represents JPEG images (.jpg, .jpeg).
	FormatJPG MediaFormat = "jpg"
	// FormatPNG represents PNG images (.png).
	FormatPNG MediaFormat = "png"
	// FormatWebP represents WebP images (.webp).
	FormatWebP MediaFormat = "webp"
	// FormatAVIF represents AVIF images (.avif).
	FormatAVIF MediaFormat = "avif"
	// FormatGIF represents GIF images (.gif).
	FormatGIF MediaFormat = "gif"
	// FormatCR3 represents Canon CR3 RAW images (.cr3).
	FormatCR3 MediaFormat = "cr3"
	// FormatCR2 represents Canon CR2 RAW images (.cr2).
	FormatCR2 MediaFormat = "cr2"
	// FormatDNG represents Adobe Digital Negative RAW images (.dng).
	FormatDNG MediaFormat = "dng"
	// FormatNEF represents Nikon NEF RAW images (.nef).
	FormatNEF MediaFormat = "nef"
	// FormatARW represents Sony ARW RAW images (.arw).
	FormatARW MediaFormat = "arw"
	// FormatORF represents Olympus ORF RAW images (.orf).
	FormatORF MediaFormat = "orf"
	// FormatRW2 represents Panasonic RW2 RAW images (.rw2).
	FormatRW2 MediaFormat = "rw2"
	// FormatHEIC represents HEIC high-efficiency images (.heic).
	FormatHEIC MediaFormat = "heic"
	// FormatHEIF represents HEIF high-efficiency images (.heif).
	FormatHEIF MediaFormat = "heif"
	// FormatMP4 represents MP4 video files (.mp4).
	FormatMP4 MediaFormat = "mp4"
	// FormatMOV represents QuickTime MOV video files (.mov).
	FormatMOV MediaFormat = "mov"
	// FormatWebM represents WebM video files (.webm).
	FormatWebM MediaFormat = "webm"
	// FormatMKV represents Matroska MKV video files (.mkv).
	FormatMKV MediaFormat = "mkv"
	// FormatM4V represents M4V video files (.m4v).
	FormatM4V MediaFormat = "m4v"
)

// IsValid reports whether f is a supported MediaFormat constant.
func (f MediaFormat) IsValid() bool {
	_, ok := MediaTypeForFormat(f)
	return ok
}

// FileStatus tracks whether a physical file or logical photo is currently present on disk.
type FileStatus string

const (
	// FileStatusActive indicates at least one file path exists on disk.
	FileStatusActive FileStatus = "active"

	// FileStatusMissing indicates the file was not observed during the latest scan.
	FileStatusMissing FileStatus = "missing"
)

// IsValid reports whether s is a supported FileStatus value.
func (s FileStatus) IsValid() bool {
	return s == FileStatusActive || s == FileStatusMissing
}

// EXIFMetadata holds parsed camera, exposure, orientation, and GPS metadata.
type EXIFMetadata struct {
	// Orientation is the EXIF orientation tag (1-8).
	Orientation int
	// CameraMake is the camera manufacturer name.
	CameraMake string
	// CameraModel is the camera body model name.
	CameraModel string
	// LensModel is the lens model identifier.
	LensModel string
	// ISO is the ISO sensitivity value if present.
	ISO *int
	// FNumber is the aperture f-number if present.
	FNumber *float64
	// ExposureTime is the shutter speed text representation (for example "1/250").
	ExposureTime string
	// FocalLengthMM is the lens focal length in millimeters if present.
	FocalLengthMM *float64
	// GPSLatitude is the signed decimal latitude in [-90, 90] if present.
	GPSLatitude *float64
	// GPSLongitude is the signed decimal longitude in [-180, 180] if present.
	GPSLongitude *float64
}

// MediaMetadata holds dimensions, playback duration, capture timestamp, and EXIF metadata extracted from a media file.
type MediaMetadata struct {
	// Width is the post-orientation display width in pixels.
	Width int
	// Height is the post-orientation display height in pixels.
	Height int
	// DurationSec is the playback duration in seconds for videos.
	DurationSec float64
	// TakenAt is the capture timestamp from EXIF or container metadata.
	TakenAt *time.Time
	// EXIF contains parsed camera, exposure, orientation, and GPS metadata.
	EXIF EXIFMetadata
}

// PhotoFile represents a single physical file path on disk linked to a logical Photo.
type PhotoFile struct {
	// ID is the unique identifier for this physical file record.
	ID string
	// PhotoID is the ID of the owning logical Photo.
	PhotoID string
	// ContentHash is the hex-encoded SHA-256 hash of the file bytes.
	ContentHash string
	// RelativePath is the NFC-normalized path relative to the library root.
	RelativePath string
	// FileName is the base filename including extension.
	FileName string
	// FolderPath is the NFC-normalized folder path relative to the library root.
	FolderPath string
	// Format is the canonical lowercase media format.
	Format MediaFormat
	// FileSize is the file size in bytes.
	FileSize int64
	// ModTime is the filesystem modification timestamp.
	ModTime time.Time
	// Status indicates whether the file is active on disk or missing.
	Status FileStatus
	// CreatedAt is the creation timestamp of the file record.
	CreatedAt time.Time
	// UpdatedAt is the last update timestamp of the file record.
	UpdatedAt time.Time
}

// Photo represents a logical media item that owns view counts, tags, EXIF, and 1..N physical files.
type Photo struct {
	// ID is the unique surrogate identifier for the logical photo.
	ID string
	// StackKey groups RAW+JPEG still photo pairs in the same folder.
	StackKey string
	// BaseName is the filename without its extension.
	BaseName string
	// FolderPath is the primary folder path relative to the library root.
	FolderPath string
	// MediaType distinguishes still photos from videos.
	MediaType MediaType
	// Formats lists deduplicated formats ordered by preview priority.
	Formats []MediaFormat
	// Files holds the physical file variants and duplicates for this photo.
	Files []PhotoFile
	// Width is the post-orientation display width in pixels.
	Width int
	// Height is the post-orientation display height in pixels.
	Height int
	// DurationSec is the playback duration in seconds for videos.
	DurationSec float64
	// TakenAt is the capture timestamp from EXIF or file metadata.
	TakenAt *time.Time
	// EXIF contains camera and GPS metadata.
	EXIF EXIFMetadata
	// ViewCount is the total recorded view count.
	ViewCount int
	// EmojiTags holds canonical emoji tags attached to the photo.
	EmojiTags []string
	// LastViewedAt is the timestamp of the most recent view event.
	LastViewedAt *time.Time
	// Status is active if at least one file is active, otherwise missing.
	Status FileStatus
	// CreatedAt is the creation timestamp of the photo record.
	CreatedAt time.Time
	// UpdatedAt is the last update timestamp of the photo record.
	UpdatedAt time.Time
}

// FormatFromExtension resolves a file extension (with or without a leading dot)
// to its canonical MediaFormat and MediaType.
func FormatFromExtension(ext string) (MediaFormat, MediaType, bool) {
	clean := strings.ToLower(strings.TrimPrefix(strings.TrimSpace(ext), "."))
	if clean == "jpeg" {
		clean = string(FormatJPG)
	}
	f := MediaFormat(clean)
	mt, ok := MediaTypeForFormat(f)
	if !ok {
		return "", "", false
	}
	return f, mt, true
}

// MediaTypeForFormat returns the MediaType for a canonical MediaFormat.
func MediaTypeForFormat(f MediaFormat) (MediaType, bool) {
	switch f {
	case FormatJPG, FormatPNG, FormatWebP, FormatAVIF, FormatGIF,
		FormatCR3, FormatCR2, FormatDNG, FormatNEF, FormatARW, FormatORF, FormatRW2,
		FormatHEIC, FormatHEIF:
		return MediaTypePhoto, true
	case FormatMP4, FormatMOV, FormatWebM, FormatMKV, FormatM4V:
		return MediaTypeVideo, true
	default:
		return "", false
	}
}

// IsRAWFormat reports whether f is a camera RAW image format.
func IsRAWFormat(f MediaFormat) bool {
	switch f {
	case FormatCR3, FormatCR2, FormatDNG, FormatNEF, FormatARW, FormatORF, FormatRW2:
		return true
	default:
		return false
	}
}

// FormatPriority returns the preview/EXIF selection priority of a media format.
// Higher numbers have higher priority: CR3 > other RAW > HEIC/HEIF > JPG > PNG/WebP/AVIF/GIF > Video > unknown.
func FormatPriority(f MediaFormat) int {
	switch f {
	case FormatCR3:
		return 100
	case FormatCR2, FormatDNG, FormatNEF, FormatARW, FormatORF, FormatRW2:
		return 90
	case FormatHEIC, FormatHEIF:
		return 80
	case FormatJPG:
		return 70
	case FormatPNG, FormatWebP, FormatAVIF, FormatGIF:
		return 60
	case FormatMP4, FormatMOV, FormatWebM, FormatMKV, FormatM4V:
		return 10
	default:
		return 0
	}
}

func fileStatusRank(s FileStatus) int {
	switch s {
	case FileStatusActive:
		return 0
	case FileStatusMissing:
		return 1
	default:
		return 2
	}
}

// SortPhotoFilesByPriority returns a sorted copy of files placing active files
// first, then higher FormatPriority first, then RelativePath ascending.
func SortPhotoFilesByPriority(files []PhotoFile) []PhotoFile {
	out := slices.Clone(files)
	slices.SortFunc(out, func(a, b PhotoFile) int {
		if r := cmp.Compare(fileStatusRank(a.Status), fileStatusRank(b.Status)); r != 0 {
			return r
		}
		if r := cmp.Compare(FormatPriority(b.Format), FormatPriority(a.Format)); r != 0 {
			return r
		}
		if r := cmp.Compare(a.RelativePath, b.RelativePath); r != 0 {
			return r
		}
		return cmp.Compare(a.ID, b.ID)
	})
	return out
}

// UniqueSortedFormats returns deduplicated formats across active files (or all
// files if none are active) ordered by FormatPriority descending.
func UniqueSortedFormats(files []PhotoFile) []MediaFormat {
	candidates := make([]PhotoFile, 0, len(files))
	for _, f := range files {
		if f.Status == FileStatusActive {
			candidates = append(candidates, f)
		}
	}
	if len(candidates) == 0 {
		candidates = files
	}
	seen := make(map[MediaFormat]struct{}, len(candidates))
	out := make([]MediaFormat, 0, len(candidates))
	for _, f := range candidates {
		if f.Format == "" {
			continue
		}
		if _, exists := seen[f.Format]; exists {
			continue
		}
		seen[f.Format] = struct{}{}
		out = append(out, f.Format)
	}
	slices.SortFunc(out, func(a, b MediaFormat) int {
		if r := cmp.Compare(FormatPriority(b), FormatPriority(a)); r != 0 {
			return r
		}
		return cmp.Compare(a, b)
	})
	return out
}

// PrimaryFile returns the highest-priority file according to SortPhotoFilesByPriority.
func PrimaryFile(files []PhotoFile) (PhotoFile, bool) {
	sorted := SortPhotoFilesByPriority(files)
	if len(sorted) == 0 {
		return PhotoFile{}, false
	}
	return sorted[0], true
}

// NormalizeNFC returns s converted to Unicode Normalization Form C (NFC).
func NormalizeNFC(s string) string {
	return norm.NFC.String(s)
}

// NormalizeRelativePath converts a path to NFC Unicode normalization, replaces
// backslashes with forward slashes, trims leading slashes, and cleans it.
func NormalizeRelativePath(p string) string {
	s := norm.NFC.String(p)
	s = strings.ReplaceAll(s, "\\", "/")
	s = strings.TrimLeft(s, "/")
	if s == "" {
		return ""
	}
	s = path.Clean(s)
	s = strings.TrimLeft(s, "/")
	if s == "." {
		return ""
	}
	return s
}

// ComputeStackKey computes the deterministic grouping key for RAW+JPEG pairs.
// Still photos stack by folder and lowercase base name, while videos include
// their format so videos never stack with still photos or other video formats.
func ComputeStackKey(folderPath string, mediaType MediaType, baseName string, format MediaFormat) string {
	folder := NormalizeRelativePath(folderPath)
	lowerBase := strings.ToLower(norm.NFC.String(baseName))
	var key string
	if mediaType == MediaTypePhoto {
		key = string(MediaTypePhoto) + "/" + lowerBase
	} else {
		key = string(mediaType) + "/" + lowerBase
		if format != "" {
			key += "." + strings.ToLower(string(format))
		}
	}
	if folder == "" {
		return key
	}
	return folder + "/" + key
}

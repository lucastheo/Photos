package domain

import "time"

// SyncStatus represents the execution state of a library synchronization run.
type SyncStatus string

const (
	// SyncStatusRunning indicates that a library scan and reconciliation run is active.
	SyncStatusRunning SyncStatus = "running"

	// SyncStatusCompleted indicates that the sync run finished without fatal errors.
	SyncStatusCompleted SyncStatus = "completed"

	// SyncStatusFailed indicates that the sync run aborted or failed.
	SyncStatusFailed SyncStatus = "failed"
)

// IsValid reports whether s is a supported SyncStatus value.
func (s SyncStatus) IsValid() bool {
	return s == SyncStatusRunning || s == SyncStatusCompleted || s == SyncStatusFailed
}

// ScannedFile represents metadata discovered on disk for a media file during sync.
type ScannedFile struct {
	// RelativePath is the NFC-normalized path relative to the library root.
	RelativePath string
	// FileName is the base filename including extension.
	FileName string
	// FolderPath is the NFC-normalized folder path relative to the library root.
	FolderPath string
	// BaseName is the filename without its extension.
	BaseName string
	// Format is the canonical lowercase media format.
	Format MediaFormat
	// MediaType classifies whether the scanned file is a photo or video.
	MediaType MediaType
	// FileSize is the file size in bytes.
	FileSize int64
	// ModTime is the filesystem modification timestamp.
	ModTime time.Time
	// ContentHash is the SHA-256 hex digest when computed.
	ContentHash string
}

// DuplicateGroup lists physical files sharing an identical SHA-256 content hash.
type DuplicateGroup struct {
	// ContentHash is the shared SHA-256 hex digest.
	ContentHash string
	// PhotoID is the ID of the logical Photo owning the duplicate copies.
	PhotoID string
	// FileCount is the number of active file paths sharing ContentHash.
	FileCount int
	// Files contains the physical PhotoFile entries in this duplicate group.
	Files []PhotoFile
}

// SyncReport summarizes progress and reconciliation counters for a library sync run.
type SyncReport struct {
	// ID is the unique identifier for this sync run.
	ID string
	// StartedAt is the timestamp when the sync run began.
	StartedAt time.Time
	// FinishedAt is the timestamp when the sync run finished, or nil if running.
	FinishedAt *time.Time
	// Status is the current lifecycle state of the sync run.
	Status SyncStatus
	// ScannedCount is the total number of valid media files seen on disk.
	ScannedCount int
	// AddedCount is the number of newly created logical photos or files.
	AddedCount int
	// UpdatedCount is the number of in-place modified files updated.
	UpdatedCount int
	// MovedCount is the number of missing files relinked to moved/renamed paths.
	MovedCount int
	// StackedCount is the number of files attached to existing RAW+JPEG photo stacks.
	StackedCount int
	// DuplicateCount is the number of exact duplicate files attached to existing photos.
	DuplicateCount int
	// UnchangedCount is the number of files skipped via the metadata fast-check.
	UnchangedCount int
	// MissingCount is the number of unseen files marked missing in this run.
	MissingCount int
	// ErrorMessage describes why the sync failed when Status is SyncStatusFailed.
	ErrorMessage string
}

// FolderStat holds the active photo count for a single library folder.
type FolderStat struct {
	// FolderPath is the NFC-normalized folder path relative to the library root.
	FolderPath string
	// Count is the number of active logical photos in this folder.
	Count int64
}

// FormatStat holds the active file or photo count for a single media format.
type FormatStat struct {
	// Format is the canonical lowercase media format.
	Format MediaFormat
	// Count is the number of active items in this format.
	Count int64
}

// LibraryStats aggregates overview metrics for the indexed media library.
type LibraryStats struct {
	// TotalPhotos is the total number of active logical photos.
	TotalPhotos int64
	// UnseenPhotos is the count of active photos with view_count == 0.
	UnseenPhotos int64
	// SeenPhotos is the count of active photos with view_count >= 1.
	SeenPhotos int64
	// TotalViews is the sum of view counts across active photos.
	TotalViews int64
	// TotalFiles is the total number of active physical files.
	TotalFiles int64
	// DuplicateGroups is the number of SHA-256 hashes with multiple active files.
	DuplicateGroups int64
	// DuplicateFiles is the total number of active files belonging to duplicate groups.
	DuplicateFiles int64
	// MissingPhotos is the count of logical photos whose files are all missing.
	MissingPhotos int64
	// MissingFiles is the count of physical files currently marked missing.
	MissingFiles int64
	// ByFormat breaks down active counts by media format.
	ByFormat []FormatStat
	// ByFolder breaks down active photo counts by folder path.
	ByFolder []FolderStat
}

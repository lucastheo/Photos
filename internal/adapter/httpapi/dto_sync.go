package httpapi

// SyncReportResponse represents the status and counters of a library sync run.
type SyncReportResponse struct {
	ID             string  `json:"id,omitempty"`
	StartedAt      *string `json:"started_at,omitempty"`
	FinishedAt     *string `json:"finished_at,omitempty"`
	Status         string  `json:"status"`
	ScannedCount   int     `json:"scanned_count"`
	AddedCount     int     `json:"added_count"`
	UpdatedCount   int     `json:"updated_count"`
	MovedCount     int     `json:"moved_count"`
	StackedCount   int     `json:"stacked_count"`
	DuplicateCount int     `json:"duplicate_count"`
	UnchangedCount int     `json:"unchanged_count"`
	MissingCount   int     `json:"missing_count"`
	ErrorMessage   string  `json:"error_message,omitempty"`
}

// DuplicateGroupDTO represents a set of active duplicate files sharing one SHA-256 hash.
type DuplicateGroupDTO struct {
	ContentHash string         `json:"content_hash"`
	PhotoID     string         `json:"photo_id"`
	FileCount   int            `json:"file_count"`
	Files       []PhotoFileDTO `json:"files"`
}

// FormatStatDTO represents the count of active files for a single media format.
type FormatStatDTO struct {
	Format string `json:"format"`
	Count  int64  `json:"count"`
}

// FolderStatDTO represents the count of active logical photos in a folder.
type FolderStatDTO struct {
	FolderPath string `json:"folder_path"`
	Count      int64  `json:"count"`
}

// LibraryStatsResponse represents aggregate library counts and breakdowns.
type LibraryStatsResponse struct {
	TotalPhotos     int64           `json:"total_photos"`
	UnseenPhotos    int64           `json:"unseen_photos"`
	SeenPhotos      int64           `json:"seen_photos"`
	TotalViews      int64           `json:"total_views"`
	TotalFiles      int64           `json:"total_files"`
	DuplicateGroups int64           `json:"duplicate_groups"`
	DuplicateFiles  int64           `json:"duplicate_files"`
	MissingPhotos   int64           `json:"missing_photos"`
	MissingFiles    int64           `json:"missing_files"`
	ByFormat        []FormatStatDTO `json:"by_format"`
	ByFolder        []FolderStatDTO `json:"by_folder"`
}

// PurgeMissingResponse summarizes how many missing records were deleted.
type PurgeMissingResponse struct {
	DeletedCount int64 `json:"deleted_count"`
}

// HealthResponse reports service readiness across database, library, and cache storage.
type HealthResponse struct {
	Status   string `json:"status"`
	Database string `json:"database"`
	Library  string `json:"library"`
	Cache    string `json:"cache"`
}

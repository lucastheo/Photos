package httpapi

// ListPhotosRequest represents HTTP query parameters for searching and paginating photos.
type ListPhotosRequest struct {
	MinViews   *int     `json:"min_views"`
	MaxViews   *int     `json:"max_views"`
	ExactViews *int     `json:"exact_views"`
	EmojiTags  []string `json:"emoji_tags"`
	Untagged   bool     `json:"untagged"`
	Formats    []string `json:"formats"`
	MediaType  string   `json:"media_type"`
	Folder     string   `json:"folder"`
	HasGPS     *bool    `json:"has_gps"`
	MinLat     *float64 `json:"min_lat"`
	MaxLat     *float64 `json:"max_lat"`
	MinLng     *float64 `json:"min_lng"`
	MaxLng     *float64 `json:"max_lng"`
	FromDate   *string  `json:"from_date"`
	ToDate     *string  `json:"to_date"`
	Query      string   `json:"q"`
	SortBy     string   `json:"sort_by"`
	SortOrder  string   `json:"sort_order"`
	Seed       int64    `json:"seed"`
	Page       int      `json:"page"`
	PageSize   int      `json:"page_size"`
}

// PhotoFileDTO represents a physical media file record in HTTP responses.
type PhotoFileDTO struct {
	ID           string `json:"id"`
	RelativePath string `json:"relative_path"`
	FileName     string `json:"file_name"`
	FolderPath   string `json:"folder_path"`
	ContentHash  string `json:"content_hash"`
	Format       string `json:"format"`
	FileSize     int64  `json:"file_size"`
	Status       string `json:"status"`
}

// EXIFMetadataDTO represents camera, exposure, and GPS metadata in HTTP responses.
type EXIFMetadataDTO struct {
	Orientation   int      `json:"orientation"`
	CameraMake    string   `json:"camera_make,omitempty"`
	CameraModel   string   `json:"camera_model,omitempty"`
	LensModel     string   `json:"lens_model,omitempty"`
	ISO           *int     `json:"iso,omitempty"`
	FNumber       *float64 `json:"f_number,omitempty"`
	ExposureTime  string   `json:"exposure_time,omitempty"`
	FocalLengthMM *float64 `json:"focal_length_mm,omitempty"`
	GPSLatitude   *float64 `json:"gps_latitude,omitempty"`
	GPSLongitude  *float64 `json:"gps_longitude,omitempty"`
}

// ViewEventDTO represents a single timestamped view event in HTTP responses.
type ViewEventDTO struct {
	ID       int64  `json:"id"`
	ViewID   string `json:"view_id,omitempty"`
	PhotoID  string `json:"photo_id"`
	ViewedAt string `json:"viewed_at"`
	Source   string `json:"source"`
}

// PhotoResponse represents a logical photo or video item in HTTP JSON responses.
type PhotoResponse struct {
	ID           string           `json:"id"`
	BaseName     string           `json:"base_name"`
	FolderPath   string           `json:"folder_path"`
	Formats      []string         `json:"formats"`
	MediaType    string           `json:"media_type"`
	Width        int              `json:"width"`
	Height       int              `json:"height"`
	DurationSec  float64          `json:"duration_sec,omitempty"`
	TakenAt      *string          `json:"taken_at,omitempty"`
	GPSLatitude  *float64         `json:"gps_latitude,omitempty"`
	GPSLongitude *float64         `json:"gps_longitude,omitempty"`
	EXIF         *EXIFMetadataDTO `json:"exif,omitempty"`
	ViewCount    int              `json:"view_count"`
	EmojiTags    []string         `json:"emoji_tags"`
	Files        []PhotoFileDTO   `json:"files"`
	ViewHistory  []ViewEventDTO   `json:"view_history,omitempty"`
	LastViewedAt *string          `json:"last_viewed_at,omitempty"`
	Status       string           `json:"status"`
	ThumbnailURL string           `json:"thumbnail_url"`
	PreviewURL   string           `json:"preview_url"`
	OriginalURL  string           `json:"original_url"`
}

// PaginatedPhotosResponse represents a paginated list of photos.
type PaginatedPhotosResponse struct {
	Items    []PhotoResponse `json:"items"`
	Total    int64           `json:"total"`
	Page     int             `json:"page"`
	PageSize int             `json:"page_size"`
}

// RecordViewRequest represents the request body for POST /api/v1/photos/{id}/views.
type RecordViewRequest struct {
	ViewID string `json:"view_id"`
	Source string `json:"source"`
}

// AdjustViewRequest represents the request body for PATCH /api/v1/photos/{id}/views.
type AdjustViewRequest struct {
	Action string `json:"action"`
}

// AddTagRequest represents the request body for POST /api/v1/photos/{id}/tags.
type AddTagRequest struct {
	Emoji string `json:"emoji"`
}

// EmojiTagCountDTO represents an emoji tag and its active photo count.
type EmojiTagCountDTO struct {
	Emoji string `json:"emoji"`
	Count int64  `json:"count"`
}

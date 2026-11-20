package domain

import (
	"fmt"
	"strings"
	"time"

	"golang.org/x/text/unicode/norm"
)

// SortField identifies the field used to order search results.
type SortField string

const (
	// SortByViews orders photos by view_count.
	SortByViews SortField = "views"

	// SortByDateTaken orders photos by capture timestamp.
	SortByDateTaken SortField = "date_taken"

	// SortByLastViewed orders photos by most recent view timestamp.
	SortByLastViewed SortField = "last_viewed"

	// SortByRandom orders photos deterministically using a session seed.
	SortByRandom SortField = "random"
)

// IsValid reports whether s is a supported SortField value.
func (s SortField) IsValid() bool {
	return s == SortByViews || s == SortByDateTaken || s == SortByLastViewed || s == SortByRandom
}

// SortOrder specifies ascending or descending sort direction.
type SortOrder string

const (
	// SortOrderAsc sorts results in ascending order.
	SortOrderAsc SortOrder = "asc"

	// SortOrderDesc sorts results in descending order.
	SortOrderDesc SortOrder = "desc"
)

// IsValid reports whether o is a supported SortOrder value.
func (o SortOrder) IsValid() bool {
	return o == SortOrderAsc || o == SortOrderDesc
}

const (
	// DefaultPage is the default 1-based page number.
	DefaultPage = 1

	// DefaultPageSize is the default number of items per page.
	DefaultPageSize = 50

	// MaxPageSize is the upper bound applied to requested page sizes.
	MaxPageSize = 500
)

// PhotoFilter defines search, filter, sort, and pagination criteria for listing photos.
type PhotoFilter struct {
	// Status filters by file availability status; defaults to FileStatusActive.
	Status FileStatus
	// MinViews filters photos with view_count >= *MinViews.
	MinViews *int
	// MaxViews filters photos with view_count <= *MaxViews.
	MaxViews *int
	// ExactViews filters photos with view_count == *ExactViews.
	ExactViews *int
	// EmojiTags applies AND intersection across normalized emoji tags.
	EmojiTags []string
	// Untagged filters photos that have zero emoji tags when true.
	Untagged bool
	// Formats filters photos that have at least one file matching these formats.
	Formats []MediaFormat
	// MediaType filters by still photo or video when non-empty.
	MediaType MediaType
	// Folder filters by NFC-normalized relative folder prefix.
	Folder string
	// HasGPS filters photos by presence or absence of GPS coordinates.
	HasGPS *bool
	// MinLat specifies the southern latitude bound in [-90, 90].
	MinLat *float64
	// MaxLat specifies the northern latitude bound in [-90, 90].
	MaxLat *float64
	// MinLng specifies the western longitude bound in [-180, 180].
	MinLng *float64
	// MaxLng specifies the eastern longitude bound in [-180, 180].
	MaxLng *float64
	// FromDate filters photos taken on or after this timestamp.
	FromDate *time.Time
	// ToDate filters photos taken on or before this timestamp.
	ToDate *time.Time
	// Query performs substring search on filename or path.
	Query string
	// SortBy specifies the sort field; defaults to SortByViews.
	SortBy SortField
	// SortOrder specifies the sort direction; defaults to SortOrderAsc.
	SortOrder SortOrder
	// Seed provides deterministic ordering when SortBy is SortByRandom.
	Seed int64
	// Page is the 1-based page index; defaults to DefaultPage.
	Page int
	// PageSize is the number of items per page; defaults to DefaultPageSize and is clamped to MaxPageSize.
	PageSize int
}

// PaginatedPhotos holds a single page of matching Photo items and total count.
type PaginatedPhotos struct {
	// Items contains the photos on the current page.
	Items []Photo
	// Total is the total number of matching photos across all pages.
	Total int64
	// Page is the 1-based current page number.
	Page int
	// PageSize is the maximum number of items per page.
	PageSize int
}

// NormalizeAndValidate applies defaults and normalizations to f and validates
// all filter constraints, returning a sanitized copy or an error wrapping ErrInvalidFilter.
func (f PhotoFilter) NormalizeAndValidate() (PhotoFilter, error) {
	out := f
	if out.Status == "" {
		out.Status = FileStatusActive
	} else if !out.Status.IsValid() {
		return PhotoFilter{}, fmt.Errorf("%w: invalid status %q", ErrInvalidFilter, out.Status)
	}
	if err := out.validateViewBounds(); err != nil {
		return PhotoFilter{}, err
	}
	if err := out.normalizeTagsAndFormats(); err != nil {
		return PhotoFilter{}, err
	}
	if err := out.validateGPSAndDates(); err != nil {
		return PhotoFilter{}, err
	}
	if err := out.normalizeSortAndPaging(); err != nil {
		return PhotoFilter{}, err
	}
	out.Folder = NormalizeRelativePath(out.Folder)
	out.Query = norm.NFC.String(strings.TrimSpace(out.Query))
	return out, nil
}

func (f *PhotoFilter) validateViewBounds() error {
	if err := validateNonNeg(f.ExactViews, "exact_views"); err != nil {
		return err
	}
	if err := validateNonNeg(f.MinViews, "min_views"); err != nil {
		return err
	}
	if err := validateNonNeg(f.MaxViews, "max_views"); err != nil {
		return err
	}
	if f.MinViews != nil && f.MaxViews != nil && *f.MinViews > *f.MaxViews {
		return fmt.Errorf("%w: min_views cannot exceed max_views", ErrInvalidFilter)
	}
	return nil
}

func (f *PhotoFilter) normalizeTagsAndFormats() error {
	if f.Untagged && len(f.EmojiTags) > 0 {
		return fmt.Errorf("%w: untagged cannot be combined with emoji_tags", ErrInvalidFilter)
	}
	tags, err := normalizeEmojiTags(f.EmojiTags)
	if err != nil {
		return err
	}
	f.EmojiTags = tags
	if f.MediaType != "" && !f.MediaType.IsValid() {
		return fmt.Errorf("%w: invalid media_type %q", ErrInvalidFilter, f.MediaType)
	}
	formats, err := normalizeFormats(f.Formats)
	if err != nil {
		return err
	}
	f.Formats = formats
	return nil
}

func (f *PhotoFilter) validateGPSAndDates() error {
	if err := validateCoordRange(f.MinLat, -90, 90, "min_lat"); err != nil {
		return err
	}
	if err := validateCoordRange(f.MaxLat, -90, 90, "max_lat"); err != nil {
		return err
	}
	if f.MinLat != nil && f.MaxLat != nil && *f.MinLat > *f.MaxLat {
		return fmt.Errorf("%w: min_lat cannot exceed max_lat", ErrInvalidFilter)
	}
	if err := validateCoordRange(f.MinLng, -180, 180, "min_lng"); err != nil {
		return err
	}
	if err := validateCoordRange(f.MaxLng, -180, 180, "max_lng"); err != nil {
		return err
	}
	if f.MinLng != nil && f.MaxLng != nil && *f.MinLng > *f.MaxLng {
		return fmt.Errorf("%w: min_lng cannot exceed max_lng", ErrInvalidFilter)
	}
	if f.FromDate != nil && f.ToDate != nil && f.FromDate.After(*f.ToDate) {
		return fmt.Errorf("%w: from_date cannot be after to_date", ErrInvalidFilter)
	}
	return nil
}

func (f *PhotoFilter) normalizeSortAndPaging() error {
	if f.SortBy == "" {
		f.SortBy = SortByViews
	} else if !f.SortBy.IsValid() {
		return fmt.Errorf("%w: invalid sort_by %q", ErrInvalidFilter, f.SortBy)
	}
	if f.SortOrder == "" {
		f.SortOrder = SortOrderAsc
	} else if !f.SortOrder.IsValid() {
		return fmt.Errorf("%w: invalid sort_order %q", ErrInvalidFilter, f.SortOrder)
	}
	if f.Page <= 0 {
		f.Page = DefaultPage
	}
	if f.PageSize <= 0 {
		f.PageSize = DefaultPageSize
	} else if f.PageSize > MaxPageSize {
		f.PageSize = MaxPageSize
	}
	return nil
}

func normalizeEmojiTags(tags []string) ([]string, error) {
	if len(tags) == 0 {
		return nil, nil
	}
	seen := make(map[string]struct{}, len(tags))
	out := make([]string, 0, len(tags))
	for _, raw := range tags {
		normTag, err := NormalizeEmojiTag(raw)
		if err != nil {
			return nil, fmt.Errorf("%w: %w", ErrInvalidFilter, err)
		}
		if _, exists := seen[normTag]; exists {
			continue
		}
		seen[normTag] = struct{}{}
		out = append(out, normTag)
	}
	return out, nil
}

func normalizeFormats(formats []MediaFormat) ([]MediaFormat, error) {
	if len(formats) == 0 {
		return nil, nil
	}
	seen := make(map[MediaFormat]struct{}, len(formats))
	out := make([]MediaFormat, 0, len(formats))
	for _, raw := range formats {
		normFmt := MediaFormat(strings.ToLower(strings.TrimSpace(string(raw))))
		if _, ok := MediaTypeForFormat(normFmt); !ok {
			return nil, fmt.Errorf("%w: %w: %q", ErrInvalidFilter, ErrUnsupportedFormat, raw)
		}
		if _, exists := seen[normFmt]; exists {
			continue
		}
		seen[normFmt] = struct{}{}
		out = append(out, normFmt)
	}
	return out, nil
}

func validateNonNeg(v *int, field string) error {
	if v != nil && *v < 0 {
		return fmt.Errorf("%w: %s must be >= 0", ErrInvalidFilter, field)
	}
	return nil
}

func validateCoordRange(v *float64, minVal, maxVal float64, field string) error {
	if v != nil && !(*v >= minVal && *v <= maxVal) {
		return fmt.Errorf("%w: %s must be between %g and %g", ErrInvalidFilter, field, minVal, maxVal)
	}
	return nil
}

package domain_test

import (
	"errors"
	"math"
	"slices"
	"testing"
	"time"

	"github.com/lucastheo/photos/internal/domain"
)

func intPtr(v int) *int              { return &v }
func floatPtr(v float64) *float64    { return &v }
func boolPtr(v bool) *bool           { return &v }
func timePtr(v time.Time) *time.Time { return &v }

func TestSortFieldAndOrderIsValid(t *testing.T) {
	t.Parallel()

	fields := []struct {
		v    domain.SortField
		want bool
	}{
		{domain.SortByViews, true},
		{domain.SortByDateTaken, true},
		{domain.SortByLastViewed, true},
		{domain.SortByRandom, true},
		{"", false},
		{"filename", false},
	}
	for _, tc := range fields {
		if got := tc.v.IsValid(); got != tc.want {
			t.Fatalf("SortField(%q).IsValid() = %v, want %v", tc.v, got, tc.want)
		}
	}

	orders := []struct {
		v    domain.SortOrder
		want bool
	}{
		{domain.SortOrderAsc, true},
		{domain.SortOrderDesc, true},
		{"", false},
		{"random", false},
	}
	for _, tc := range orders {
		if got := tc.v.IsValid(); got != tc.want {
			t.Fatalf("SortOrder(%q).IsValid() = %v, want %v", tc.v, got, tc.want)
		}
	}
}

func TestPhotoFilterNormalizeAndValidateSuccess(t *testing.T) {
	t.Parallel()

	t0 := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	t1 := time.Date(2026, 12, 31, 0, 0, 0, 0, time.UTC)

	t.Run("defaults on zero filter", func(t *testing.T) {
		t.Parallel()
		got, err := (domain.PhotoFilter{}).NormalizeAndValidate()
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got.Status != domain.FileStatusActive ||
			got.SortBy != domain.SortByViews ||
			got.SortOrder != domain.SortOrderAsc ||
			got.Page != domain.DefaultPage ||
			got.PageSize != domain.DefaultPageSize {
			t.Fatalf("unexpected defaults: %+v", got)
		}
	})

	t.Run("normalizes tags formats folder query and clamps page size", func(t *testing.T) {
		t.Parallel()
		origTags := []string{"❤️", "\u2764", "🏖️"}
		origFmts := []domain.MediaFormat{domain.FormatCR3, "JPG", " cr3 "}
		in := domain.PhotoFilter{
			Status:     domain.FileStatusMissing,
			MinViews:   intPtr(0),
			MaxViews:   intPtr(5),
			ExactViews: intPtr(2),
			EmojiTags:  origTags,
			Formats:    origFmts,
			MediaType:  domain.MediaTypePhoto,
			Folder:     `/Cafe` + "\u0301" + `\2025/`,
			HasGPS:     boolPtr(true),
			MinLat:     floatPtr(-90),
			MaxLat:     floatPtr(90),
			MinLng:     floatPtr(-180),
			MaxLng:     floatPtr(180),
			FromDate:   timePtr(t0),
			ToDate:     timePtr(t1),
			Query:      "  Cafe\u0301  ",
			SortBy:     domain.SortByRandom,
			SortOrder:  domain.SortOrderDesc,
			Seed:       42,
			Page:       -3,
			PageSize:   9999,
		}

		got, err := in.NormalizeAndValidate()
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !slices.Equal(got.EmojiTags, []string{"\u2764", "🏖"}) {
			t.Fatalf("EmojiTags = %v, want [❤ 🏖]", got.EmojiTags)
		}
		if !slices.Equal(got.Formats, []domain.MediaFormat{domain.FormatCR3, domain.FormatJPG}) {
			t.Fatalf("Formats = %v, want [cr3 jpg]", got.Formats)
		}
		if got.Folder != "Café/2025" {
			t.Fatalf("Folder = %q, want Café/2025", got.Folder)
		}
		if got.Query != "Café" {
			t.Fatalf("Query = %q, want Café", got.Query)
		}
		if got.Page != domain.DefaultPage || got.PageSize != domain.MaxPageSize {
			t.Fatalf("Page=%d PageSize=%d, want 1 and %d", got.Page, got.PageSize, domain.MaxPageSize)
		}
		if origTags[0] != "❤️" || origFmts[1] != "JPG" {
			t.Fatal("input slices were mutated")
		}
	})

	t.Run("untagged without emoji tags and equal date/view bounds", func(t *testing.T) {
		t.Parallel()
		in := domain.PhotoFilter{
			Untagged: true,
			MinViews: intPtr(2),
			MaxViews: intPtr(2),
			FromDate: timePtr(t0),
			ToDate:   timePtr(t0),
			Page:     3,
			PageSize: 25,
		}
		got, err := in.NormalizeAndValidate()
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !got.Untagged || got.Page != 3 || got.PageSize != 25 {
			t.Fatalf("unexpected filter: %+v", got)
		}
	})
}

func TestPhotoFilterNormalizeAndValidateErrors(t *testing.T) {
	t.Parallel()

	t0 := time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC)
	t1 := time.Date(2026, 4, 1, 0, 0, 0, 0, time.UTC)

	cases := []struct {
		name      string
		filter    domain.PhotoFilter
		wantErrIs error
	}{
		{
			name:      "invalid status",
			filter:    domain.PhotoFilter{Status: "unknown"},
			wantErrIs: domain.ErrInvalidFilter,
		},
		{
			name:      "negative exact views",
			filter:    domain.PhotoFilter{ExactViews: intPtr(-1)},
			wantErrIs: domain.ErrInvalidFilter,
		},
		{
			name:      "negative min views",
			filter:    domain.PhotoFilter{MinViews: intPtr(-1)},
			wantErrIs: domain.ErrInvalidFilter,
		},
		{
			name:      "negative max views",
			filter:    domain.PhotoFilter{MaxViews: intPtr(-1)},
			wantErrIs: domain.ErrInvalidFilter,
		},
		{
			name:      "min views exceeds max views",
			filter:    domain.PhotoFilter{MinViews: intPtr(5), MaxViews: intPtr(2)},
			wantErrIs: domain.ErrInvalidFilter,
		},
		{
			name:      "untagged combined with emoji tags",
			filter:    domain.PhotoFilter{Untagged: true, EmojiTags: []string{"❤️"}},
			wantErrIs: domain.ErrInvalidFilter,
		},
		{
			name:      "invalid emoji tag propagates ErrInvalidEmojiTag",
			filter:    domain.PhotoFilter{EmojiTags: []string{"hello"}},
			wantErrIs: domain.ErrInvalidEmojiTag,
		},
		{
			name:      "invalid emoji tag also wraps ErrInvalidFilter",
			filter:    domain.PhotoFilter{EmojiTags: []string{"hello"}},
			wantErrIs: domain.ErrInvalidFilter,
		},
		{
			name:      "invalid media type",
			filter:    domain.PhotoFilter{MediaType: "audio"},
			wantErrIs: domain.ErrInvalidFilter,
		},
		{
			name:      "unsupported format propagates ErrUnsupportedFormat",
			filter:    domain.PhotoFilter{Formats: []domain.MediaFormat{"bmp"}},
			wantErrIs: domain.ErrUnsupportedFormat,
		},
		{
			name:      "unsupported format also wraps ErrInvalidFilter",
			filter:    domain.PhotoFilter{Formats: []domain.MediaFormat{"bmp"}},
			wantErrIs: domain.ErrInvalidFilter,
		},
		{
			name:      "min lat below -90",
			filter:    domain.PhotoFilter{MinLat: floatPtr(-90.1)},
			wantErrIs: domain.ErrInvalidFilter,
		},
		{
			name:      "max lat above 90",
			filter:    domain.PhotoFilter{MaxLat: floatPtr(90.1)},
			wantErrIs: domain.ErrInvalidFilter,
		},
		{
			name:      "lat NaN",
			filter:    domain.PhotoFilter{MinLat: floatPtr(math.NaN())},
			wantErrIs: domain.ErrInvalidFilter,
		},
		{
			name:      "min lat exceeds max lat",
			filter:    domain.PhotoFilter{MinLat: floatPtr(10), MaxLat: floatPtr(-10)},
			wantErrIs: domain.ErrInvalidFilter,
		},
		{
			name:      "min lng below -180",
			filter:    domain.PhotoFilter{MinLng: floatPtr(-180.1)},
			wantErrIs: domain.ErrInvalidFilter,
		},
		{
			name:      "max lng above 180",
			filter:    domain.PhotoFilter{MaxLng: floatPtr(180.1)},
			wantErrIs: domain.ErrInvalidFilter,
		},
		{
			name:      "lng +Inf",
			filter:    domain.PhotoFilter{MaxLng: floatPtr(math.Inf(1))},
			wantErrIs: domain.ErrInvalidFilter,
		},
		{
			name:      "min lng exceeds max lng",
			filter:    domain.PhotoFilter{MinLng: floatPtr(20), MaxLng: floatPtr(10)},
			wantErrIs: domain.ErrInvalidFilter,
		},
		{
			name:      "from date after to date",
			filter:    domain.PhotoFilter{FromDate: timePtr(t0), ToDate: timePtr(t1)},
			wantErrIs: domain.ErrInvalidFilter,
		},
		{
			name:      "invalid sort by",
			filter:    domain.PhotoFilter{SortBy: "size"},
			wantErrIs: domain.ErrInvalidFilter,
		},
		{
			name:      "invalid sort order",
			filter:    domain.PhotoFilter{SortOrder: "sideways"},
			wantErrIs: domain.ErrInvalidFilter,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			_, err := tc.filter.NormalizeAndValidate()
			if err == nil {
				t.Fatalf("expected error matching %v, got nil", tc.wantErrIs)
			}
			if !errors.Is(err, tc.wantErrIs) {
				t.Fatalf("error = %v, want errors.Is(_, %v)", err, tc.wantErrIs)
			}
		})
	}
}

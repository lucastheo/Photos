package domain_test

import (
	"slices"
	"testing"
	"time"

	"github.com/lucastheo/photos/internal/domain"
)

func TestMediaTypeIsValid(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name  string
		input domain.MediaType
		want  bool
	}{
		{name: "photo", input: domain.MediaTypePhoto, want: true},
		{name: "video", input: domain.MediaTypeVideo, want: true},
		{name: "empty", input: "", want: false},
		{name: "unknown", input: "audio", want: false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := tc.input.IsValid(); got != tc.want {
				t.Fatalf("MediaType(%q).IsValid() = %v, want %v", tc.input, got, tc.want)
			}
		})
	}
}

func TestMediaFormatAndExtensionHelpers(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name     string
		ext      string
		wantFmt  domain.MediaFormat
		wantType domain.MediaType
		wantOK   bool
		wantRAW  bool
	}{
		{name: "jpg with dot", ext: ".jpg", wantFmt: domain.FormatJPG, wantType: domain.MediaTypePhoto, wantOK: true},
		{name: "jpeg uppercase", ext: ".JPEG", wantFmt: domain.FormatJPG, wantType: domain.MediaTypePhoto, wantOK: true},
		{name: "jpeg without dot", ext: "jpeg", wantFmt: domain.FormatJPG, wantType: domain.MediaTypePhoto, wantOK: true},
		{name: "png", ext: "PNG", wantFmt: domain.FormatPNG, wantType: domain.MediaTypePhoto, wantOK: true},
		{name: "webp", ext: ".webp", wantFmt: domain.FormatWebP, wantType: domain.MediaTypePhoto, wantOK: true},
		{name: "avif", ext: ".avif", wantFmt: domain.FormatAVIF, wantType: domain.MediaTypePhoto, wantOK: true},
		{name: "gif", ext: ".gif", wantFmt: domain.FormatGIF, wantType: domain.MediaTypePhoto, wantOK: true},
		{name: "cr3", ext: ".CR3", wantFmt: domain.FormatCR3, wantType: domain.MediaTypePhoto, wantOK: true, wantRAW: true},
		{name: "cr2", ext: ".cr2", wantFmt: domain.FormatCR2, wantType: domain.MediaTypePhoto, wantOK: true, wantRAW: true},
		{name: "dng", ext: ".dng", wantFmt: domain.FormatDNG, wantType: domain.MediaTypePhoto, wantOK: true, wantRAW: true},
		{name: "nef", ext: ".nef", wantFmt: domain.FormatNEF, wantType: domain.MediaTypePhoto, wantOK: true, wantRAW: true},
		{name: "arw", ext: ".arw", wantFmt: domain.FormatARW, wantType: domain.MediaTypePhoto, wantOK: true, wantRAW: true},
		{name: "orf", ext: ".orf", wantFmt: domain.FormatORF, wantType: domain.MediaTypePhoto, wantOK: true, wantRAW: true},
		{name: "rw2", ext: ".rw2", wantFmt: domain.FormatRW2, wantType: domain.MediaTypePhoto, wantOK: true, wantRAW: true},
		{name: "heic", ext: ".HEIC", wantFmt: domain.FormatHEIC, wantType: domain.MediaTypePhoto, wantOK: true},
		{name: "heif", ext: ".heif", wantFmt: domain.FormatHEIF, wantType: domain.MediaTypePhoto, wantOK: true},
		{name: "mp4", ext: ".mp4", wantFmt: domain.FormatMP4, wantType: domain.MediaTypeVideo, wantOK: true},
		{name: "mov", ext: ".MOV", wantFmt: domain.FormatMOV, wantType: domain.MediaTypeVideo, wantOK: true},
		{name: "webm", ext: ".webm", wantFmt: domain.FormatWebM, wantType: domain.MediaTypeVideo, wantOK: true},
		{name: "mkv", ext: ".mkv", wantFmt: domain.FormatMKV, wantType: domain.MediaTypeVideo, wantOK: true},
		{name: "m4v", ext: ".m4v", wantFmt: domain.FormatM4V, wantType: domain.MediaTypeVideo, wantOK: true},
		{name: "empty", ext: "", wantOK: false},
		{name: "dot only", ext: ".", wantOK: false},
		{name: "double dot", ext: "..jpg", wantOK: false},
		{name: "unsupported txt", ext: ".txt", wantOK: false},
		{name: "unsupported xmp", ext: ".xmp", wantOK: false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			gotFmt, gotType, gotOK := domain.FormatFromExtension(tc.ext)
			if gotOK != tc.wantOK || gotFmt != tc.wantFmt || gotType != tc.wantType {
				t.Fatalf("FormatFromExtension(%q) = (%q, %q, %v), want (%q, %q, %v)",
					tc.ext, gotFmt, gotType, gotOK, tc.wantFmt, tc.wantType, tc.wantOK)
			}
			if tc.wantOK {
				if !gotFmt.IsValid() {
					t.Fatalf("MediaFormat(%q).IsValid() = false, want true", gotFmt)
				}
				if gotRAW := domain.IsRAWFormat(gotFmt); gotRAW != tc.wantRAW {
					t.Fatalf("IsRAWFormat(%q) = %v, want %v", gotFmt, gotRAW, tc.wantRAW)
				}
			}
		})
	}

	if domain.MediaFormat("bmp").IsValid() {
		t.Fatal("MediaFormat(\"bmp\").IsValid() = true, want false")
	}
	if domain.IsRAWFormat(domain.FormatJPG) {
		t.Fatal("IsRAWFormat(FormatJPG) = true, want false")
	}
}

func TestFileStatusIsValid(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name  string
		input domain.FileStatus
		want  bool
	}{
		{name: "active", input: domain.FileStatusActive, want: true},
		{name: "missing", input: domain.FileStatusMissing, want: true},
		{name: "empty", input: "", want: false},
		{name: "deleted", input: "deleted", want: false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := tc.input.IsValid(); got != tc.want {
				t.Fatalf("FileStatus(%q).IsValid() = %v, want %v", tc.input, got, tc.want)
			}
		})
	}
}

func TestFormatPriorityOrdering(t *testing.T) {
	t.Parallel()

	ordered := []domain.MediaFormat{
		domain.FormatCR3,
		domain.FormatCR2,
		domain.FormatHEIC,
		domain.FormatJPG,
		domain.FormatPNG,
		domain.FormatMP4,
		domain.MediaFormat("unknown"),
	}

	for i := range len(ordered) - 1 {
		hi := domain.FormatPriority(ordered[i])
		lo := domain.FormatPriority(ordered[i+1])
		if hi <= lo {
			t.Fatalf("FormatPriority(%q)=%d must be > FormatPriority(%q)=%d",
				ordered[i], hi, ordered[i+1], lo)
		}
	}

	rawGroup := []domain.MediaFormat{
		domain.FormatCR2, domain.FormatDNG, domain.FormatNEF,
		domain.FormatARW, domain.FormatORF, domain.FormatRW2,
	}
	for _, f := range rawGroup {
		if domain.FormatPriority(f) >= domain.FormatPriority(domain.FormatCR3) {
			t.Fatalf("FormatPriority(%q) must be < CR3", f)
		}
		if domain.FormatPriority(f) <= domain.FormatPriority(domain.FormatHEIC) {
			t.Fatalf("FormatPriority(%q) must be > HEIC", f)
		}
	}
}

func TestSortFilesUniqueFormatsAndPrimaryFile(t *testing.T) {
	t.Parallel()

	t.Run("empty slice", func(t *testing.T) {
		t.Parallel()
		if got := domain.SortPhotoFilesByPriority(nil); len(got) != 0 {
			t.Fatalf("SortPhotoFilesByPriority(nil) len = %d, want 0", len(got))
		}
		if got := domain.UniqueSortedFormats(nil); len(got) != 0 {
			t.Fatalf("UniqueSortedFormats(nil) len = %d, want 0", len(got))
		}
		if _, ok := domain.PrimaryFile(nil); ok {
			t.Fatal("PrimaryFile(nil) ok = true, want false")
		}
	})

	t.Run("active before missing and CR3 over HEIC over JPG", func(t *testing.T) {
		t.Parallel()
		input := []domain.PhotoFile{
			{ID: "f1", Format: domain.FormatJPG, RelativePath: "b/IMG.jpg", Status: domain.FileStatusActive},
			{ID: "f2", Format: domain.FormatCR3, RelativePath: "a/IMG.cr3", Status: domain.FileStatusMissing},
			{ID: "f3", Format: domain.FormatHEIC, RelativePath: "a/IMG.heic", Status: domain.FileStatusActive},
			{ID: "f4", Format: domain.FormatCR3, RelativePath: "c/IMG.cr3", Status: domain.FileStatusActive},
			{ID: "f5", Format: domain.FormatJPG, RelativePath: "a/IMG.jpg", Status: domain.FileStatusActive},
			{ID: "f6", Format: domain.FormatPNG, RelativePath: "x/IMG.png", Status: domain.FileStatus("other")},
		}
		orig := slices.Clone(input)

		sorted := domain.SortPhotoFilesByPriority(input)
		if !slices.Equal(input, orig) {
			t.Fatal("SortPhotoFilesByPriority mutated caller slice")
		}

		wantIDs := []string{"f4", "f3", "f5", "f1", "f2", "f6"}
		for i, wantID := range wantIDs {
			if sorted[i].ID != wantID {
				t.Fatalf("sorted[%d].ID = %q, want %q", i, sorted[i].ID, wantID)
			}
		}

		primary, ok := domain.PrimaryFile(input)
		if !ok || primary.ID != "f4" {
			t.Fatalf("PrimaryFile() = (%+v, %v), want f4", primary, ok)
		}

		formats := domain.UniqueSortedFormats(input)
		wantFormats := []domain.MediaFormat{domain.FormatCR3, domain.FormatHEIC, domain.FormatJPG}
		if !slices.Equal(formats, wantFormats) {
			t.Fatalf("UniqueSortedFormats() = %v, want %v", formats, wantFormats)
		}
	})

	t.Run("tie-break by path then ID and fallback to missing formats", func(t *testing.T) {
		t.Parallel()
		files := []domain.PhotoFile{
			{ID: "b", Format: domain.FormatPNG, RelativePath: "same.png", Status: domain.FileStatusMissing},
			{ID: "a", Format: domain.FormatPNG, RelativePath: "same.png", Status: domain.FileStatusMissing},
			{ID: "c", Format: domain.FormatAVIF, RelativePath: "z.avif", Status: domain.FileStatusMissing},
			{ID: "d", Format: "", RelativePath: "emptyfmt", Status: domain.FileStatusMissing},
		}

		sorted := domain.SortPhotoFilesByPriority(files)
		if sorted[0].ID != "a" || sorted[1].ID != "b" || sorted[2].ID != "c" {
			t.Fatalf("unexpected tie-break order: %+v", sorted)
		}

		formats := domain.UniqueSortedFormats(files)
		wantFormats := []domain.MediaFormat{domain.FormatAVIF, domain.FormatPNG}
		if !slices.Equal(formats, wantFormats) {
			t.Fatalf("UniqueSortedFormats(all missing) = %v, want %v", formats, wantFormats)
		}
	})
}

func TestNormalizeRelativePath(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name  string
		input string
		want  string
	}{
		{name: "empty", input: "", want: ""},
		{name: "dot", input: ".", want: ""},
		{name: "dot slash", input: "./", want: ""},
		{name: "slash", input: "/", want: ""},
		{name: "multiple slashes", input: "///", want: ""},
		{name: "slash dot", input: "/.", want: ""},
		{name: "backslash root", input: "\\", want: ""},
		{name: "leading and trailing slashes", input: "/Trips/2025/", want: "Trips/2025"},
		{name: "windows separators", input: `Trips\2025\IMG_0042.JPG`, want: "Trips/2025/IMG_0042.JPG"},
		{name: "clean segments", input: "a//b/./c/../d/", want: "a/b/d"},
		{name: "NFD to NFC", input: "Cafe\u0301/IMG.jpg", want: "Café/IMG.jpg"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := domain.NormalizeRelativePath(tc.input); got != tc.want {
				t.Fatalf("NormalizeRelativePath(%q) = %q, want %q", tc.input, got, tc.want)
			}
		})
	}
}

func TestComputeStackKey(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name      string
		folder    string
		mediaType domain.MediaType
		baseName  string
		format    domain.MediaFormat
		want      string
	}{
		{
			name:      "still photo CR3 in folder",
			folder:    "2025/Trip",
			mediaType: domain.MediaTypePhoto,
			baseName:  "IMG_0042",
			format:    domain.FormatCR3,
			want:      "2025/Trip/photo/img_0042",
		},
		{
			name:      "still photo JPG lowercase matches CR3",
			folder:    "/2025\\Trip/",
			mediaType: domain.MediaTypePhoto,
			baseName:  "img_0042",
			format:    domain.FormatJPG,
			want:      "2025/Trip/photo/img_0042",
		},
		{
			name:      "still photo at root with NFD name",
			folder:    "",
			mediaType: domain.MediaTypePhoto,
			baseName:  "E\u0301te\u0301",
			format:    domain.FormatHEIC,
			want:      "photo/été",
		},
		{
			name:      "video MOV does not stack with still photo",
			folder:    "2025/Trip",
			mediaType: domain.MediaTypeVideo,
			baseName:  "IMG_0042",
			format:    domain.FormatMOV,
			want:      "2025/Trip/video/img_0042.mov",
		},
		{
			name:      "video MP4 does not stack with MOV",
			folder:    "2025/Trip",
			mediaType: domain.MediaTypeVideo,
			baseName:  "IMG_0042",
			format:    domain.FormatMP4,
			want:      "2025/Trip/video/img_0042.mp4",
		},
		{
			name:      "video at root without format",
			folder:    ".",
			mediaType: domain.MediaTypeVideo,
			baseName:  "Clip",
			format:    "",
			want:      "video/clip",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := domain.ComputeStackKey(tc.folder, tc.mediaType, tc.baseName, tc.format)
			if got != tc.want {
				t.Fatalf("ComputeStackKey(%q, %q, %q, %q) = %q, want %q",
					tc.folder, tc.mediaType, tc.baseName, tc.format, got, tc.want)
			}
		})
	}

	// Verify CR3 and JPG stack together while MOV does not.
	cr3 := domain.ComputeStackKey("2025/Trip", domain.MediaTypePhoto, "IMG_0042", domain.FormatCR3)
	jpg := domain.ComputeStackKey("2025/Trip", domain.MediaTypePhoto, "IMG_0042", domain.FormatJPG)
	mov := domain.ComputeStackKey("2025/Trip", domain.MediaTypeVideo, "IMG_0042", domain.FormatMOV)
	if cr3 != jpg {
		t.Fatalf("expected CR3 (%q) and JPG (%q) to share stack key", cr3, jpg)
	}
	if cr3 == mov {
		t.Fatalf("expected MOV (%q) not to stack with still photo (%q)", mov, cr3)
	}
}

func TestPhotoStructsInstantiation(t *testing.T) {
	t.Parallel()

	now := time.Now().UTC()
	iso := 400
	fnum := 2.8
	focal := 35.0
	lat := -23.5505
	lng := -46.6333

	p := domain.Photo{
		ID:         "p1",
		StackKey:   "Trips/photo/img_0001",
		BaseName:   "IMG_0001",
		FolderPath: "Trips",
		MediaType:  domain.MediaTypePhoto,
		Formats:    []domain.MediaFormat{domain.FormatCR3},
		Files: []domain.PhotoFile{
			{
				ID:           "f1",
				PhotoID:      "p1",
				ContentHash:  "abc",
				RelativePath: "Trips/IMG_0001.CR3",
				FileName:     "IMG_0001.CR3",
				FolderPath:   "Trips",
				Format:       domain.FormatCR3,
				FileSize:     1024,
				ModTime:      now,
				Status:       domain.FileStatusActive,
				CreatedAt:    now,
				UpdatedAt:    now,
			},
		},
		Width:       6000,
		Height:      4000,
		DurationSec: 0,
		TakenAt:     &now,
		EXIF: domain.EXIFMetadata{
			Orientation:   1,
			CameraMake:    "Canon",
			CameraModel:   "EOS R6",
			LensModel:     "RF35mm F1.8",
			ISO:           &iso,
			FNumber:       &fnum,
			ExposureTime:  "1/500",
			FocalLengthMM: &focal,
			GPSLatitude:   &lat,
			GPSLongitude:  &lng,
		},
		ViewCount:    3,
		EmojiTags:    []string{"❤"},
		LastViewedAt: &now,
		Status:       domain.FileStatusActive,
		CreatedAt:    now,
		UpdatedAt:    now,
	}

	if p.EXIF.CameraMake != "Canon" || len(p.Files) != 1 {
		t.Fatalf("unexpected photo struct: %+v", p)
	}
}

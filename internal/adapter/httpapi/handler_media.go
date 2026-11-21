package httpapi

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/lucastheo/photos/internal/domain"
)

const immutableMediaCacheHeader = "private, max-age=604800, immutable"

// Thumbnail handles GET /api/v1/photos/{id}/thumbnail, serving the cached 400px
// JPEG thumbnail (generated on demand if missing).
func (h *PhotoHandler) Thumbnail(w http.ResponseWriter, r *http.Request) {
	photo, target, ok := h.resolveTargetFile(w, r)
	if !ok {
		return
	}
	cacheName := target.ContentHash + "_thumb.jpg"
	etag := `"thumb-` + target.ContentHash + `"`
	h.serveCachedJPEG(w, r, photo, target, cacheName, etag)
}

// Preview handles GET /api/v1/photos/{id}/preview, serving cached 2560px previews
// for RAW/HEIC formats or direct HTTP-Range-capable streams for standard images and videos.
func (h *PhotoHandler) Preview(w http.ResponseWriter, r *http.Request) {
	photo, target, ok := h.resolveTargetFile(w, r)
	if !ok {
		return
	}
	if needsTranscodedPreview(target.Format) {
		cacheName := target.ContentHash + "_preview.jpg"
		etag := `"preview-` + target.ContentHash + `"`
		h.serveCachedJPEG(w, r, photo, target, cacheName, etag)
		return
	}
	h.serveLibraryFile(w, r, target, false)
}

// Original handles GET /api/v1/photos/{id}/original, streaming the original media
// file from the read-only library root with HTTP Range support.
func (h *PhotoHandler) Original(w http.ResponseWriter, r *http.Request) {
	_, target, ok := h.resolveTargetFile(w, r)
	if !ok {
		return
	}
	download := strings.EqualFold(r.URL.Query().Get("download"), "true") || r.URL.Query().Get("download") == "1"
	h.serveLibraryFile(w, r, target, download)
}

func (h *PhotoHandler) resolveTargetFile(w http.ResponseWriter, r *http.Request) (domain.Photo, domain.PhotoFile, bool) {
	if h.searcher == nil {
		WriteProblem(w, r, h.logger, errors.New("photo searcher unavailable"))
		return domain.Photo{}, domain.PhotoFile{}, false
	}
	photo, err := h.searcher.GetByID(r.Context(), r.PathValue("id"))
	if err != nil {
		WriteProblem(w, r, h.logger, err)
		return domain.Photo{}, domain.PhotoFile{}, false
	}
	target, err := selectTargetFile(photo, r.URL.Query().Get("format"), r.URL.Query().Get("file_id"))
	if err != nil {
		WriteProblem(w, r, h.logger, err)
		return domain.Photo{}, domain.PhotoFile{}, false
	}
	return photo, target, true
}

func selectTargetFile(photo domain.Photo, requestedFormat, requestedFileID string) (domain.PhotoFile, error) {
	var normFmt domain.MediaFormat
	if rawFmt := strings.TrimSpace(requestedFormat); rawFmt != "" {
		parsed, _, ok := domain.FormatFromExtension(rawFmt)
		if !ok {
			return domain.PhotoFile{}, fmt.Errorf("%w: %q", domain.ErrUnsupportedFormat, rawFmt)
		}
		normFmt = parsed
	}
	var active []domain.PhotoFile
	for _, f := range domain.SortPhotoFilesByPriority(photo.Files) {
		if f.Status == domain.FileStatusActive {
			active = append(active, f)
		}
	}
	if len(active) == 0 {
		return domain.PhotoFile{}, domain.ErrFileNotFound
	}
	if fileID := strings.TrimSpace(requestedFileID); fileID != "" {
		for _, f := range active {
			if f.ID == fileID {
				return f, nil
			}
		}
		return domain.PhotoFile{}, domain.ErrFileNotFound
	}
	if normFmt != "" {
		for _, f := range active {
			if f.Format == normFmt {
				return f, nil
			}
		}
		return domain.PhotoFile{}, domain.ErrFileNotFound
	}
	return active[0], nil
}

func (h *PhotoHandler) serveCachedJPEG(
	w http.ResponseWriter,
	r *http.Request,
	photo domain.Photo,
	target domain.PhotoFile,
	cacheName, etag string,
) {
	if _, err := filepath.Localize(cacheName); err != nil {
		WriteProblem(w, r, h.logger, domain.ErrFileNotFound)
		return
	}
	if err := h.ensureCacheExists(r.Context(), photo, target, cacheName); err != nil {
		WriteProblem(w, r, h.logger, err)
		return
	}
	root, f, modTime, err := openRootRegularFile(h.cachePath, cacheName)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) || errors.Is(err, fs.ErrNotExist) {
			WriteProblem(w, r, h.logger, domain.ErrFileNotFound)
			return
		}
		WriteProblem(w, r, h.logger, err)
		return
	}
	defer func() { _ = f.Close(); _ = root.Close() }()

	w.Header().Set("Content-Type", "image/jpeg")
	w.Header().Set("ETag", etag)
	w.Header().Set("Cache-Control", immutableMediaCacheHeader)
	http.ServeContent(w, r, cacheName, modTime, f)
}

func (h *PhotoHandler) ensureCacheExists(
	ctx context.Context,
	photo domain.Photo,
	target domain.PhotoFile,
	cacheName string,
) error {
	if h.previews == nil || strings.TrimSpace(h.cachePath) == "" {
		return nil
	}
	root, err := os.OpenRoot(h.cachePath)
	if err == nil {
		info, statErr := root.Stat(cacheName)
		_ = root.Close()
		if statErr == nil && info.Mode().IsRegular() && info.Size() > 0 {
			return nil
		}
	}
	return h.previews.EnsureCache(ctx, target.RelativePath, target.ContentHash, target.Format, photo.EXIF.Orientation)
}

func (h *PhotoHandler) serveLibraryFile(w http.ResponseWriter, r *http.Request, target domain.PhotoFile, download bool) {
	root, f, modTime, err := openRootRegularFile(h.libraryPath, target.RelativePath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) || errors.Is(err, fs.ErrNotExist) {
			WriteProblem(w, r, h.logger, domain.ErrFileNotFound)
			return
		}
		WriteProblem(w, r, h.logger, err)
		return
	}
	defer func() { _ = f.Close(); _ = root.Close() }()

	disposition := "inline"
	if download {
		disposition = "attachment"
	}
	safeName := strings.NewReplacer(`"`, "", `\`, "", "\r", "", "\n", "").Replace(target.FileName)
	w.Header().Set("Content-Disposition", fmt.Sprintf(`%s; filename="%s"`, disposition, safeName))
	w.Header().Set("Content-Type", mimeTypeForFormat(target.Format))
	w.Header().Set("ETag", `"orig-`+target.ContentHash+`"`)
	w.Header().Set("Cache-Control", immutableMediaCacheHeader)
	http.ServeContent(w, r, target.FileName, modTime, f)
}

func openRootRegularFile(baseDir, relativePath string) (*os.Root, *os.File, time.Time, error) {
	if strings.TrimSpace(baseDir) == "" {
		return nil, nil, time.Time{}, domain.ErrFileNotFound
	}
	norm := domain.NormalizeRelativePath(relativePath)
	local, err := filepath.Localize(norm)
	if err != nil {
		return nil, nil, time.Time{}, fmt.Errorf("%w: invalid path %q", domain.ErrFileNotFound, relativePath)
	}
	root, err := os.OpenRoot(baseDir)
	if err != nil {
		return nil, nil, time.Time{}, fmt.Errorf("open root %q: %w", baseDir, err)
	}
	f, err := root.Open(local)
	if err != nil {
		_ = root.Close()
		return nil, nil, time.Time{}, err
	}
	info, err := f.Stat()
	if err != nil || !info.Mode().IsRegular() {
		_ = f.Close()
		_ = root.Close()
		if err != nil {
			return nil, nil, time.Time{}, err
		}
		return nil, nil, time.Time{}, domain.ErrFileNotFound
	}
	return root, f, info.ModTime(), nil
}

func needsTranscodedPreview(format domain.MediaFormat) bool {
	return domain.IsRAWFormat(format) || format == domain.FormatHEIC || format == domain.FormatHEIF
}

func mimeTypeForFormat(format domain.MediaFormat) string {
	switch format {
	case domain.FormatJPG:
		return "image/jpeg"
	case domain.FormatPNG:
		return "image/png"
	case domain.FormatWebP:
		return "image/webp"
	case domain.FormatAVIF:
		return "image/avif"
	case domain.FormatGIF:
		return "image/gif"
	case domain.FormatHEIC:
		return "image/heic"
	case domain.FormatHEIF:
		return "image/heif"
	case domain.FormatMP4, domain.FormatM4V:
		return "video/mp4"
	case domain.FormatMOV:
		return "video/quicktime"
	case domain.FormatWebM:
		return "video/webm"
	case domain.FormatMKV:
		return "video/x-matroska"
	default:
		return "application/octet-stream"
	}
}

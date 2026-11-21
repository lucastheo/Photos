package httpapi_test

import (
	"bytes"
	"context"
	"encoding/json"
	"image"
	"image/color"
	"image/jpeg"
	"io"
	"log/slog"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/lucastheo/photos/internal/adapter/httpapi"
	"github.com/lucastheo/photos/internal/domain"
)

type httpIntegrationHarness struct {
	mu           sync.Mutex
	libDir       string
	cacheDir     string
	photos       map[string]domain.Photo
	history      map[string][]domain.ViewEvent
	viewIDs      map[string]bool
	latestSync   *domain.SyncReport
	password     string
	authDisabled bool
	validToken   string
}

func newHTTPIntegrationHarness(t *testing.T, authDisabled bool) *httpIntegrationHarness {
	t.Helper()
	libDir := t.TempDir()
	cacheDir := t.TempDir()

	writeFileBytes(t, filepath.Join(libDir, "2026/Trip/IMG_0042.CR3"), []byte("RAW-CR3-BINARY-PAYLOAD-FOR-DOWNLOAD"))
	writeFileBytes(t, filepath.Join(libDir, "2026/Trip/IMG_0042.jpg"), encodeJPEGBytes(t, 120, 80))
	writeFileBytes(t, filepath.Join(libDir, "2026/Trip/clip.mp4"), buildRangeTestVideoBytes())
	writeFileBytes(t, filepath.Join(libDir, "Backup/IMG_0042.jpg"), encodeJPEGBytes(t, 120, 80))

	return &httpIntegrationHarness{
		libDir:       libDir,
		cacheDir:     cacheDir,
		photos:       make(map[string]domain.Photo),
		history:      make(map[string][]domain.ViewEvent),
		viewIDs:      make(map[string]bool),
		password:     "integration-secret",
		authDisabled: authDisabled,
		validToken:   "signed-session-token",
	}
}

func (h *httpIntegrationHarness) Login(_ context.Context, password string) (string, time.Time, error) {
	if !h.authDisabled && password != h.password {
		return "", time.Time{}, domain.ErrInvalidCredentials
	}
	return h.validToken, time.Now().UTC().Add(24 * time.Hour), nil
}

func (h *httpIntegrationHarness) SessionStatus(token string) domain.AuthSession {
	if h.authDisabled {
		return domain.AuthSession{Authenticated: true, AuthDisabled: true}
	}
	return domain.AuthSession{Authenticated: token == h.validToken, AuthDisabled: false}
}

func (h *httpIntegrationHarness) Sync(_ context.Context) (domain.SyncReport, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	now := time.Now().UTC()
	lat, lng := -34.6037, -58.3816

	h.photos["photo-stack"] = domain.Photo{
		ID:         "photo-stack",
		StackKey:   "2026/Trip/photo/img_0042",
		BaseName:   "IMG_0042",
		FolderPath: "2026/Trip",
		MediaType:  domain.MediaTypePhoto,
		Formats:    []domain.MediaFormat{domain.FormatCR3, domain.FormatJPG},
		Width:      6000,
		Height:     4000,
		TakenAt:    &now,
		EXIF: domain.EXIFMetadata{
			Orientation:  1,
			CameraMake:   "Canon",
			CameraModel:  "EOS R5",
			GPSLatitude:  &lat,
			GPSLongitude: &lng,
		},
		Status: domain.FileStatusActive,
		Files: []domain.PhotoFile{
			{
				ID: "f-cr3", PhotoID: "photo-stack", ContentHash: "hashcr3",
				RelativePath: "2026/Trip/IMG_0042.CR3", FileName: "IMG_0042.CR3",
				FolderPath: "2026/Trip", Format: domain.FormatCR3, FileSize: 35, Status: domain.FileStatusActive,
			},
			{
				ID: "f-jpg", PhotoID: "photo-stack", ContentHash: "hashjpg",
				RelativePath: "2026/Trip/IMG_0042.jpg", FileName: "IMG_0042.jpg",
				FolderPath: "2026/Trip", Format: domain.FormatJPG, FileSize: 512, Status: domain.FileStatusActive,
			},
			{
				ID: "f-dup", PhotoID: "photo-stack", ContentHash: "hashjpg",
				RelativePath: "Backup/IMG_0042.jpg", FileName: "IMG_0042.jpg",
				FolderPath: "Backup", Format: domain.FormatJPG, FileSize: 512, Status: domain.FileStatusActive,
			},
		},
	}
	h.photos["video-clip"] = domain.Photo{
		ID:          "video-clip",
		StackKey:    "2026/Trip/video/clip.mp4",
		BaseName:    "clip",
		FolderPath:  "2026/Trip",
		MediaType:   domain.MediaTypeVideo,
		Formats:     []domain.MediaFormat{domain.FormatMP4},
		Width:       1920,
		Height:      1080,
		DurationSec: 4.5,
		TakenAt:     &now,
		Status:      domain.FileStatusActive,
		Files: []domain.PhotoFile{
			{
				ID: "f-mp4", PhotoID: "video-clip", ContentHash: "hashmp4",
				RelativePath: "2026/Trip/clip.mp4", FileName: "clip.mp4",
				FolderPath: "2026/Trip", Format: domain.FormatMP4, FileSize: 256, Status: domain.FileStatusActive,
			},
		},
	}
	h.photos["missing-old"] = domain.Photo{
		ID: "missing-old", BaseName: "gone", FolderPath: "2025", MediaType: domain.MediaTypePhoto,
		Formats: []domain.MediaFormat{domain.FormatJPG}, Status: domain.FileStatusMissing,
		Files: []domain.PhotoFile{
			{ID: "f-gone", PhotoID: "missing-old", ContentHash: "hashgone", RelativePath: "2025/gone.jpg", FileName: "gone.jpg", FolderPath: "2025", Format: domain.FormatJPG, Status: domain.FileStatusMissing},
		},
	}

	finished := now.Add(time.Second)
	rep := domain.SyncReport{
		ID:             "sync-1",
		StartedAt:      now,
		FinishedAt:     &finished,
		Status:         domain.SyncStatusCompleted,
		ScannedCount:   4,
		AddedCount:     2,
		StackedCount:   1,
		DuplicateCount: 1,
		MissingCount:   1,
	}
	h.latestSync = &rep
	return rep, nil
}

func (h *httpIntegrationHarness) Status(_ context.Context) (domain.SyncReport, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.latestSync == nil {
		return domain.SyncReport{}, domain.ErrSyncNotFound
	}
	return *h.latestSync, nil
}

func (h *httpIntegrationHarness) PurgeMissing(_ context.Context) (int64, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	var purged int64
	for id, p := range h.photos {
		if p.Status == domain.FileStatusMissing {
			delete(h.photos, id)
			purged += 2
		}
	}
	return purged, nil
}

func (h *httpIntegrationHarness) Search(_ context.Context, filter domain.PhotoFilter) (domain.PaginatedPhotos, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	var items []domain.Photo
	for _, p := range h.photos {
		if p.Status != filter.Status {
			continue
		}
		if filter.MaxViews != nil && p.ViewCount > *filter.MaxViews {
			continue
		}
		if filter.MinViews != nil && p.ViewCount < *filter.MinViews {
			continue
		}
		if filter.Untagged && len(p.EmojiTags) > 0 {
			continue
		}
		if len(filter.EmojiTags) > 0 && !hasAllTags(p.EmojiTags, filter.EmojiTags) {
			continue
		}
		items = append(items, p)
	}
	slices.SortFunc(items, func(a, b domain.Photo) int {
		return strings.Compare(a.ID, b.ID)
	})
	return domain.PaginatedPhotos{Items: items, Total: int64(len(items)), Page: filter.Page, PageSize: filter.PageSize}, nil
}

func (h *httpIntegrationHarness) GetByID(_ context.Context, id string) (domain.Photo, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	p, ok := h.photos[id]
	if !ok {
		return domain.Photo{}, domain.ErrPhotoNotFound
	}
	return p, nil
}

func (h *httpIntegrationHarness) RecordView(_ context.Context, photoID, viewID string, source domain.ViewSource) (domain.Photo, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	p, ok := h.photos[photoID]
	if !ok {
		return domain.Photo{}, domain.ErrPhotoNotFound
	}
	if viewID != "" && h.viewIDs[viewID] {
		return p, nil
	}
	if viewID != "" {
		h.viewIDs[viewID] = true
	}
	if source == "" {
		source = domain.ViewSourceDwell
	}
	now := time.Now().UTC()
	h.history[photoID] = append([]domain.ViewEvent{{ID: int64(len(h.history[photoID]) + 1), ViewID: viewID, PhotoID: photoID, ViewedAt: now, Source: source}}, h.history[photoID]...)
	p.ViewCount = len(h.history[photoID])
	p.LastViewedAt = &now
	h.photos[photoID] = p
	return p, nil
}

func (h *httpIntegrationHarness) AdjustViewCount(_ context.Context, photoID string, action domain.ViewAction) (domain.Photo, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	p, ok := h.photos[photoID]
	if !ok {
		return domain.Photo{}, domain.ErrPhotoNotFound
	}
	now := time.Now().UTC()
	switch action {
	case domain.ViewActionIncrement:
		h.history[photoID] = append([]domain.ViewEvent{{ID: int64(len(h.history[photoID]) + 1), PhotoID: photoID, ViewedAt: now, Source: domain.ViewSourceManual}}, h.history[photoID]...)
	case domain.ViewActionDecrement:
		if len(h.history[photoID]) > 0 {
			h.history[photoID] = h.history[photoID][1:]
		}
	case domain.ViewActionReset:
		h.history[photoID] = nil
	default:
		return domain.Photo{}, domain.ErrInvalidViewAction
	}
	p.ViewCount = len(h.history[photoID])
	if p.ViewCount == 0 {
		p.LastViewedAt = nil
	}
	h.photos[photoID] = p
	return p, nil
}

func (h *httpIntegrationHarness) ListViewHistory(_ context.Context, photoID string, _ int) ([]domain.ViewEvent, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	return slices.Clone(h.history[photoID]), nil
}

func (h *httpIntegrationHarness) AddTag(_ context.Context, photoID, rawEmoji string) (domain.Photo, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	p, ok := h.photos[photoID]
	if !ok {
		return domain.Photo{}, domain.ErrPhotoNotFound
	}
	norm, err := domain.NormalizeEmojiTag(rawEmoji)
	if err != nil {
		return domain.Photo{}, err
	}
	if !slices.Contains(p.EmojiTags, norm) {
		p.EmojiTags = append(p.EmojiTags, norm)
	}
	h.photos[photoID] = p
	return p, nil
}

func (h *httpIntegrationHarness) RemoveTag(_ context.Context, photoID, rawEmoji string) (domain.Photo, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	p, ok := h.photos[photoID]
	if !ok {
		return domain.Photo{}, domain.ErrPhotoNotFound
	}
	norm, err := domain.NormalizeEmojiTag(rawEmoji)
	if err != nil {
		return domain.Photo{}, err
	}
	p.EmojiTags = slices.DeleteFunc(p.EmojiTags, func(s string) bool { return s == norm })
	h.photos[photoID] = p
	return p, nil
}

func (h *httpIntegrationHarness) ListTags(_ context.Context) ([]domain.EmojiTagCount, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	counts := make(map[string]int64)
	for _, p := range h.photos {
		if p.Status == domain.FileStatusActive {
			for _, e := range p.EmojiTags {
				counts[e]++
			}
		}
	}
	out := make([]domain.EmojiTagCount, 0, len(counts))
	for e, c := range counts {
		out = append(out, domain.EmojiTagCount{Emoji: e, Count: c})
	}
	return out, nil
}

func (h *httpIntegrationHarness) EnsureCache(_ context.Context, _, contentHash string, _ domain.MediaFormat, _ int) error {
	writeFileBytes(nil, filepath.Join(h.cacheDir, contentHash+"_thumb.jpg"), encodeJPEGBytes(nil, 40, 30))
	writeFileBytes(nil, filepath.Join(h.cacheDir, contentHash+"_preview.jpg"), encodeJPEGBytes(nil, 120, 80))
	return nil
}

func (h *httpIntegrationHarness) GetLibraryStats(_ context.Context) (domain.LibraryStats, error) {
	return domain.LibraryStats{TotalPhotos: 2, DuplicateGroups: 1, DuplicateFiles: 2, MissingPhotos: 1, MissingFiles: 1}, nil
}

func (h *httpIntegrationHarness) ListDuplicates(_ context.Context) ([]domain.DuplicateGroup, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	p := h.photos["photo-stack"]
	return []domain.DuplicateGroup{{ContentHash: "hashjpg", PhotoID: p.ID, FileCount: 2, Files: p.Files[1:]}}, nil
}

func (h *httpIntegrationHarness) ListMissing(_ context.Context) ([]domain.Photo, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	var out []domain.Photo
	for _, p := range h.photos {
		if p.Status == domain.FileStatusMissing {
			out = append(out, p)
		}
	}
	return out, nil
}

func (h *httpIntegrationHarness) PingContext(_ context.Context) error {
	return nil
}

func TestHTTPAPI_Integration_EndToEndLifecycleAndRangeStreaming(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	h := newHTTPIntegrationHarness(t, false)
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	router := httpapi.NewRouter(httpapi.RouterConfig{
		Auth:          h,
		AuthHandler:   httpapi.NewAuthHandler(h, httpapi.NewLoginRateLimiter(5, time.Minute), false).WithLogger(logger),
		PhotoHandler:  httpapi.NewPhotoHandler(h, h, h, h, h.libDir, h.cacheDir).WithLogger(logger),
		SyncHandler:   httpapi.NewSyncHandler(h, h).WithLogger(logger),
		HealthHandler: httpapi.NewHealthHandler(h, h.libDir, h.cacheDir),
		Logger:        logger,
	})

	srv := httptest.NewServer(router)
	defer srv.Close()

	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatalf("cookiejar.New: %v", err)
	}
	client := &http.Client{Jar: jar}

	// 1. Unauthenticated request rejected with 401; login sets HttpOnly cookie.
	resp, err := client.Get(srv.URL + "/api/v1/photos")
	if err != nil {
		t.Fatalf("GET /photos: %v", err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("unauthenticated status = %d, want 401", resp.StatusCode)
	}

	resp, err = client.Post(srv.URL+"/api/v1/auth/login", "application/json", strings.NewReader(`{"password":"integration-secret"}`))
	if err != nil {
		t.Fatalf("POST /login: %v", err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("login status = %d, want 200", resp.StatusCode)
	}

	// 2. Sync -> search -> views (idempotent POST + PATCH) -> tags.
	resp, err = client.Post(srv.URL+"/api/v1/library/sync", "application/json", nil)
	if err != nil {
		t.Fatalf("POST /sync: %v", err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("sync status = %d, want 200", resp.StatusCode)
	}

	resp, err = client.Get(srv.URL + "/api/v1/photos?min_views=0&max_views=0")
	if err != nil {
		t.Fatalf("GET /photos: %v", err)
	}
	var listResp httpapi.PaginatedPhotosResponse
	decodeRespJSON(t, resp, &listResp)
	if listResp.Total != 2 {
		t.Fatalf("Total = %d, want 2", listResp.Total)
	}

	for range 2 {
		resp, err = client.Post(srv.URL+"/api/v1/photos/photo-stack/views", "application/json", strings.NewReader(`{"view_id":"idem-1","source":"dwell"}`))
		if err != nil {
			t.Fatalf("POST /views: %v", err)
		}
		var detail httpapi.PhotoResponse
		decodeRespJSON(t, resp, &detail)
		if detail.ViewCount != 1 {
			t.Fatalf("idempotent ViewCount = %d, want 1", detail.ViewCount)
		}
	}

	patchReq, _ := http.NewRequest(http.MethodPatch, srv.URL+"/api/v1/photos/photo-stack/views", strings.NewReader(`{"action":"increment"}`))
	patchReq.Header.Set("Content-Type", "application/json")
	resp, err = client.Do(patchReq)
	if err != nil {
		t.Fatalf("PATCH /views: %v", err)
	}
	var detail httpapi.PhotoResponse
	decodeRespJSON(t, resp, &detail)
	if detail.ViewCount != 2 || len(detail.ViewHistory) != 2 {
		t.Fatalf("adjusted detail = %+v", detail)
	}

	// Add & filter & remove emoji tag
	resp, err = client.Post(srv.URL+"/api/v1/photos/photo-stack/tags", "application/json", strings.NewReader(`{"emoji":"❤️"}`))
	if err != nil {
		t.Fatalf("POST /tags: %v", err)
	}
	decodeRespJSON(t, resp, &detail)
	if !slices.Equal(detail.EmojiTags, []string{"❤"}) {
		t.Fatalf("EmojiTags = %v, want [❤]", detail.EmojiTags)
	}

	resp, err = client.Get(srv.URL + "/api/v1/photos?emoji_tags=%E2%9D%A4%EF%B8%8F")
	if err != nil {
		t.Fatalf("GET /photos?emoji_tags: %v", err)
	}
	decodeRespJSON(t, resp, &listResp)
	if listResp.Total != 1 || listResp.Items[0].ID != "photo-stack" {
		t.Fatalf("filtered emoji list = %+v", listResp)
	}

	delReq, _ := http.NewRequest(http.MethodDelete, srv.URL+"/api/v1/photos/photo-stack/tags/%E2%9D%A4%EF%B8%8F", nil)
	resp, err = client.Do(delReq)
	if err != nil {
		t.Fatalf("DELETE /tags: %v", err)
	}
	_ = resp.Body.Close()

	// 3. Thumbnail, preview (CR3 default vs ?format=jpg), original download, and 206 Partial Content Range streaming.
	for _, path := range []string{
		"/api/v1/photos/photo-stack/thumbnail",
		"/api/v1/photos/photo-stack/preview",
		"/api/v1/photos/photo-stack/preview?format=jpg",
		"/api/v1/photos/photo-stack/original?format=cr3&download=1",
	} {
		resp, err = client.Get(srv.URL + path)
		if err != nil {
			t.Fatalf("GET %s: %v", path, err)
		}
		_, _ = io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("GET %s status = %d, want 200", path, resp.StatusCode)
		}
	}

	rangeReq, _ := http.NewRequest(http.MethodGet, srv.URL+"/api/v1/photos/video-clip/preview", nil)
	rangeReq.Header.Set("Range", "bytes=0-63")
	resp, err = client.Do(rangeReq)
	if err != nil {
		t.Fatalf("Range GET /preview: %v", err)
	}
	chunk, _ := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusPartialContent || len(chunk) != 64 {
		t.Fatalf("Range response = (%d, len=%d), want (206, 64)", resp.StatusCode, len(chunk))
	}

	// 4. Stats, duplicates, missing, purge missing, and sync status.
	for _, ep := range []string{"/api/v1/library/stats", "/api/v1/library/duplicates", "/api/v1/library/missing", "/api/v1/library/sync/status", "/api/v1/tags"} {
		resp, err = client.Get(srv.URL + ep)
		if err != nil {
			t.Fatalf("GET %s: %v", ep, err)
		}
		_ = resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("GET %s status = %d, want 200", ep, resp.StatusCode)
		}
	}
	purgeReq, _ := http.NewRequest(http.MethodDelete, srv.URL+"/api/v1/library/missing", nil)
	resp, err = client.Do(purgeReq)
	if err != nil {
		t.Fatalf("DELETE /missing: %v", err)
	}
	var purgeResp httpapi.PurgeMissingResponse
	decodeRespJSON(t, resp, &purgeResp)
	if purgeResp.DeletedCount != 2 {
		t.Fatalf("DeletedCount = %d, want 2", purgeResp.DeletedCount)
	}
}

func hasAllTags(have, want []string) bool {
	for _, w := range want {
		if !slices.Contains(have, w) {
			return false
		}
	}
	return true
}

func decodeRespJSON(t *testing.T, resp *http.Response, dst any) {
	t.Helper()
	defer func() { _ = resp.Body.Close() }()
	if err := json.NewDecoder(resp.Body).Decode(dst); err != nil {
		t.Fatalf("decode JSON response: %v", err)
	}
}

func writeFileBytes(t *testing.T, fullPath string, data []byte) {
	if err := os.MkdirAll(filepath.Dir(fullPath), 0o755); err != nil {
		if t != nil {
			t.Fatalf("mkdir %s: %v", fullPath, err)
		}
		return
	}
	if err := os.WriteFile(fullPath, data, 0o600); err != nil && t != nil {
		t.Fatalf("write %s: %v", fullPath, err)
	}
}

func encodeJPEGBytes(t *testing.T, w, h int) []byte {
	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	for y := range h {
		for x := range w {
			img.SetNRGBA(x, y, color.NRGBA{R: 120, G: 160, B: 200, A: 255})
		}
	}
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: 85}); err != nil && t != nil {
		t.Fatalf("encode jpeg: %v", err)
	}
	return buf.Bytes()
}

func buildRangeTestVideoBytes() []byte {
	return bytes.Repeat([]byte("0123456789abcdef"), 16)
}

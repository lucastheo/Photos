package main

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
	"testing"

	"github.com/lucastheo/photos/internal/adapter/httpapi"
	"github.com/lucastheo/photos/internal/adapter/sqlite"
	"github.com/lucastheo/photos/internal/config"
	"github.com/lucastheo/photos/internal/domain"
	"github.com/lucastheo/photos/web"
)

func TestHelpersAndLoggerConfig(t *testing.T) {
	t.Parallel()

	cfg := config.DefaultConfig()
	cfg.LogFormat = "json"
	cfg.LogLevel = "debug"
	if l := newLogger(cfg); l == nil {
		t.Fatal("expected json logger")
	}
	for _, lvl := range []string{"debug", "info", "warn", "error", "unknown"} {
		_ = parseSlogLevel(lvl)
	}
	if got := decodeSecret(""); got != nil {
		t.Fatalf("expected nil for empty secret, got %v", got)
	}
	hex64 := strings.Repeat("ab", 32)
	if got := decodeSecret(hex64); len(got) != 32 {
		t.Fatalf("expected 32 decoded bytes, got %d", len(got))
	}
	if got := decodeSecret("raw-passphrase"); string(got) != "raw-passphrase" {
		t.Fatalf("unexpected raw secret: %q", string(got))
	}
}

func TestApp_Integration_FullConcreteWiringAndGracefulShutdown(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	tmp := t.TempDir()
	libDir := filepath.Join(tmp, "library")
	cacheDir := filepath.Join(tmp, "cache")
	dbPath := filepath.Join(tmp, "photos.db")

	writeJPEGFixture(t, filepath.Join(libDir, "2026/Trip/IMG_001.jpg"), 320, 240)
	writeJPEGFixture(t, filepath.Join(libDir, "2026/Trip/IMG_002.jpg"), 400, 300)

	// Seed a stale running sync row before app startup to verify RecoverStaleRuns.
	seedDB, err := sqlite.Open(context.Background(), dbPath)
	if err != nil {
		t.Fatalf("sqlite.Open seed: %v", err)
	}
	if err := seedDB.Migrate(context.Background()); err != nil {
		t.Fatalf("Migrate seed: %v", err)
	}
	syncRepo := sqlite.NewSyncRepository(seedDB.SQL())
	if err := syncRepo.CreateSyncRun(context.Background(), domain.SyncReport{
		ID:     "stale-1",
		Status: domain.SyncStatusRunning,
	}); err != nil {
		t.Fatalf("CreateSyncRun stale: %v", err)
	}
	_ = seedDB.Close()

	cfg := config.DefaultConfig()
	cfg.LibraryPath = libDir
	cfg.DBPath = dbPath
	cfg.CachePath = cacheDir
	cfg.AuthPassword = "app-password"

	staticFS, err := web.DistFS()
	if err != nil {
		t.Fatalf("web.DistFS failed: %v", err)
	}
	app, err := buildApplication(context.Background(), cfg, slog.New(slog.NewTextHandler(io.Discard, nil)), staticFS)
	if err != nil {
		t.Fatalf("buildApplication failed: %v", err)
	}

	srv := httptest.NewServer(app.router)
	defer func() {
		srv.Close()
		if err := app.shutdown(); err != nil {
			t.Fatalf("app.shutdown failed: %v", err)
		}
	}()

	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatalf("cookiejar.New: %v", err)
	}
	client := &http.Client{Jar: jar}

	// Verify embedded SPA routes serve index.html with no-cache and health returns JSON 200.
	for _, spaRoute := range []string{"/", "/library"} {
		spaResp, spaErr := client.Get(srv.URL + spaRoute)
		if spaErr != nil || spaResp.StatusCode != http.StatusOK {
			t.Fatalf("GET %s = (%v, %v)", spaRoute, spaResp, spaErr)
		}
		spaBody, _ := io.ReadAll(spaResp.Body)
		_ = spaResp.Body.Close()
		if !strings.Contains(string(spaBody), "<photo-tracker-app>") || spaResp.Header.Get("Cache-Control") != "no-cache" {
			t.Fatalf("GET %s unexpected SPA response (cc=%q)", spaRoute, spaResp.Header.Get("Cache-Control"))
		}
	}

	resp, err := client.Get(srv.URL + "/api/v1/health")
	if err != nil || resp.StatusCode != http.StatusOK {
		t.Fatalf("GET /health = (%v, %v)", resp, err)
	}
	_ = resp.Body.Close()

	resp, err = client.Post(srv.URL+"/api/v1/auth/login", "application/json", strings.NewReader(`{"password":"app-password"}`))
	if err != nil || resp.StatusCode != http.StatusOK {
		t.Fatalf("POST /login = (%v, %v)", resp, err)
	}
	_ = resp.Body.Close()

	resp, err = client.Get(srv.URL + "/api/v1/library/sync/status")
	if err != nil {
		t.Fatalf("GET /sync/status: %v", err)
	}
	var staleRep httpapi.SyncReportResponse
	decodeJSONResp(t, resp, &staleRep)
	if staleRep.Status != "failed" || !strings.Contains(staleRep.ErrorMessage, "interrupted") {
		t.Fatalf("expected stale run recovered as failed, got %+v", staleRep)
	}

	// Full sync -> search -> record view -> tag -> thumbnail.
	resp, err = client.Post(srv.URL+"/api/v1/library/sync", "application/json", nil)
	if err != nil || resp.StatusCode != http.StatusOK {
		t.Fatalf("POST /sync = (%v, %v)", resp, err)
	}
	var syncRep httpapi.SyncReportResponse
	decodeJSONResp(t, resp, &syncRep)
	if syncRep.Status != "completed" || syncRep.AddedCount != 2 {
		t.Fatalf("unexpected sync report: %+v", syncRep)
	}

	resp, err = client.Get(srv.URL + "/api/v1/photos?exact_views=0")
	if err != nil {
		t.Fatalf("GET /photos: %v", err)
	}
	var page httpapi.PaginatedPhotosResponse
	decodeJSONResp(t, resp, &page)
	if page.Total != 2 {
		t.Fatalf("Total = %d, want 2", page.Total)
	}
	photoID := page.Items[0].ID

	resp, err = client.Post(srv.URL+"/api/v1/photos/"+photoID+"/views", "application/json", strings.NewReader(`{"view_id":"app-v1","source":"dwell"}`))
	if err != nil || resp.StatusCode != http.StatusOK {
		t.Fatalf("POST /views = (%v, %v)", resp, err)
	}
	var detail httpapi.PhotoResponse
	decodeJSONResp(t, resp, &detail)
	if detail.ViewCount != 1 {
		t.Fatalf("ViewCount = %d, want 1", detail.ViewCount)
	}

	resp, err = client.Post(srv.URL+"/api/v1/photos/"+photoID+"/tags", "application/json", strings.NewReader(`{"emoji":"⭐"}`))
	if err != nil || resp.StatusCode != http.StatusOK {
		t.Fatalf("POST /tags = (%v, %v)", resp, err)
	}
	decodeJSONResp(t, resp, &detail)
	if !slices.Contains(detail.EmojiTags, "⭐") {
		t.Fatalf("EmojiTags = %v, want ⭐", detail.EmojiTags)
	}

	resp, err = client.Get(srv.URL + detail.ThumbnailURL)
	if err != nil || resp.StatusCode != http.StatusOK {
		t.Fatalf("GET thumbnail = (%v, %v)", resp, err)
	}
	thumbBytes, _ := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if len(thumbBytes) == 0 {
		t.Fatal("expected non-empty thumbnail JPEG")
	}
}

func decodeJSONResp(t *testing.T, resp *http.Response, dst any) {
	t.Helper()
	defer func() { _ = resp.Body.Close() }()
	if err := json.NewDecoder(resp.Body).Decode(dst); err != nil {
		t.Fatalf("decode JSON: %v", err)
	}
}

func writeJPEGFixture(t *testing.T, path string, w, h int) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", path, err)
	}
	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	for y := range h {
		for x := range w {
			img.SetNRGBA(x, y, color.NRGBA{R: uint8(x % 255), G: uint8(y % 255), B: 180, A: 255})
		}
	}
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: 85}); err != nil {
		t.Fatalf("jpeg.Encode: %v", err)
	}
	if err := os.WriteFile(path, buf.Bytes(), 0o600); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

package httpapi_test

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/lucastheo/photos/internal/adapter/httpapi"
	"github.com/lucastheo/photos/internal/domain"
)

type fakeAuthSvc struct {
	authDisabled bool
	validToken   string
	password     string
}

func (f *fakeAuthSvc) Login(_ context.Context, password string) (string, time.Time, error) {
	if !f.authDisabled && password != f.password {
		return "", time.Time{}, domain.ErrInvalidCredentials
	}
	return f.validToken, time.Now().UTC().Add(24 * time.Hour), nil
}

func (f *fakeAuthSvc) SessionStatus(token string) domain.AuthSession {
	if f.authDisabled {
		return domain.AuthSession{Authenticated: true, AuthDisabled: true}
	}
	return domain.AuthSession{Authenticated: token == f.validToken, AuthDisabled: false}
}

type fakePhotoServices struct {
	photo   domain.Photo
	history []domain.ViewEvent
	tags    []domain.EmojiTagCount
	err     error
}

func (f *fakePhotoServices) Search(_ context.Context, filter domain.PhotoFilter) (domain.PaginatedPhotos, error) {
	if f.err != nil {
		return domain.PaginatedPhotos{}, f.err
	}
	return domain.PaginatedPhotos{Items: []domain.Photo{f.photo}, Total: 1, Page: filter.Page, PageSize: filter.PageSize}, nil
}

func (f *fakePhotoServices) GetByID(_ context.Context, id string) (domain.Photo, error) {
	if f.err != nil {
		return domain.Photo{}, f.err
	}
	if id != f.photo.ID {
		return domain.Photo{}, domain.ErrPhotoNotFound
	}
	return f.photo, nil
}

func (f *fakePhotoServices) RecordView(_ context.Context, photoID, viewID string, source domain.ViewSource) (domain.Photo, error) {
	if f.err != nil {
		return domain.Photo{}, f.err
	}
	if photoID != f.photo.ID {
		return domain.Photo{}, domain.ErrPhotoNotFound
	}
	if source == "bad" {
		return domain.Photo{}, domain.ErrInvalidViewSource
	}
	f.photo.ViewCount++
	f.history = append(f.history, domain.ViewEvent{ID: int64(f.photo.ViewCount), ViewID: viewID, PhotoID: photoID, ViewedAt: time.Now().UTC(), Source: source})
	return f.photo, nil
}

func (f *fakePhotoServices) AdjustViewCount(_ context.Context, photoID string, action domain.ViewAction) (domain.Photo, error) {
	if f.err != nil {
		return domain.Photo{}, f.err
	}
	if photoID != f.photo.ID {
		return domain.Photo{}, domain.ErrPhotoNotFound
	}
	switch action {
	case domain.ViewActionIncrement:
		f.photo.ViewCount++
	case domain.ViewActionDecrement:
		if f.photo.ViewCount > 0 {
			f.photo.ViewCount--
		}
	case domain.ViewActionReset:
		f.photo.ViewCount = 0
		f.history = nil
	default:
		return domain.Photo{}, domain.ErrInvalidViewAction
	}
	return f.photo, nil
}

func (f *fakePhotoServices) ListViewHistory(_ context.Context, _ string, _ int) ([]domain.ViewEvent, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.history, nil
}

func (f *fakePhotoServices) AddTag(_ context.Context, photoID, rawEmoji string) (domain.Photo, error) {
	if f.err != nil {
		return domain.Photo{}, f.err
	}
	if photoID != f.photo.ID {
		return domain.Photo{}, domain.ErrPhotoNotFound
	}
	norm, err := domain.NormalizeEmojiTag(rawEmoji)
	if err != nil {
		return domain.Photo{}, err
	}
	f.photo.EmojiTags = append(f.photo.EmojiTags, norm)
	return f.photo, nil
}

func (f *fakePhotoServices) RemoveTag(_ context.Context, photoID, rawEmoji string) (domain.Photo, error) {
	if f.err != nil {
		return domain.Photo{}, f.err
	}
	if photoID != f.photo.ID {
		return domain.Photo{}, domain.ErrPhotoNotFound
	}
	if _, err := domain.NormalizeEmojiTag(rawEmoji); err != nil {
		return domain.Photo{}, err
	}
	f.photo.EmojiTags = nil
	return f.photo, nil
}

func (f *fakePhotoServices) ListTags(_ context.Context) ([]domain.EmojiTagCount, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.tags, nil
}

type fakeSyncServices struct {
	syncReport domain.SyncReport
	syncErr    error
	statusErr  error
	purgeCount int64
	stats      domain.LibraryStats
	dups       []domain.DuplicateGroup
	missing    []domain.Photo
	pingErr    error
	gate       chan struct{}
}

func (f *fakeSyncServices) Sync(ctx context.Context) (domain.SyncReport, error) {
	if f.gate != nil {
		select {
		case <-f.gate:
		case <-ctx.Done():
			return domain.SyncReport{}, ctx.Err()
		}
	}
	if f.syncErr != nil {
		return domain.SyncReport{}, f.syncErr
	}
	return f.syncReport, nil
}

func (f *fakeSyncServices) Status(_ context.Context) (domain.SyncReport, error) {
	if f.statusErr != nil {
		return domain.SyncReport{}, f.statusErr
	}
	return f.syncReport, nil
}

func (f *fakeSyncServices) PurgeMissing(_ context.Context) (int64, error) {
	if f.syncErr != nil {
		return 0, f.syncErr
	}
	return f.purgeCount, nil
}

func (f *fakeSyncServices) GetLibraryStats(_ context.Context) (domain.LibraryStats, error) {
	if f.syncErr != nil {
		return domain.LibraryStats{}, f.syncErr
	}
	return f.stats, nil
}

func (f *fakeSyncServices) ListDuplicates(_ context.Context) ([]domain.DuplicateGroup, error) {
	if f.syncErr != nil {
		return nil, f.syncErr
	}
	return f.dups, nil
}

func (f *fakeSyncServices) ListMissing(_ context.Context) ([]domain.Photo, error) {
	if f.syncErr != nil {
		return nil, f.syncErr
	}
	return f.missing, nil
}

func (f *fakeSyncServices) PingContext(_ context.Context) error {
	return f.pingErr
}

func TestRouterAuthAndSPAFallbackUnit(t *testing.T) {
	t.Parallel()

	authSvc := &fakeAuthSvc{validToken: "tok-ok", password: "pw"}
	limiter := httpapi.NewLoginRateLimiter(2, time.Minute)
	authH := httpapi.NewAuthHandler(authSvc, limiter, false)
	syncSvc := &fakeSyncServices{statusErr: domain.ErrSyncNotFound}
	syncH := httpapi.NewSyncHandler(syncSvc, syncSvc)
	healthH := httpapi.NewHealthHandler(syncSvc, t.TempDir(), t.TempDir())
	spaFS := fstest.MapFS{
		"index.html":    &fstest.MapFile{Data: []byte("<html>SPA</html>")},
		"assets/app.js": &fstest.MapFile{Data: []byte("console.log('ok')")},
		"favicon.ico":   &fstest.MapFile{Data: []byte("ico")},
		"nested.html":   &fstest.MapFile{Data: []byte("<html>nested</html>")},
	}

	rt := httpapi.NewRouter(httpapi.RouterConfig{
		Auth:          authSvc,
		AuthHandler:   authH,
		SyncHandler:   syncH,
		HealthHandler: healthH,
		StaticFS:      spaFS,
	})

	// Health OK
	rec := httptest.NewRecorder()
	rt.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/health", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("health status = %d", rec.Code)
	}

	// Health degraded when DB ping fails
	syncSvc.pingErr = errors.New("db down")
	rec = httptest.NewRecorder()
	rt.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/health", nil))
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("degraded health status = %d, want 503", rec.Code)
	}
	syncSvc.pingErr = nil

	// SPA asset and fallback routes with cache headers
	rec = httptest.NewRecorder()
	rt.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/assets/app.js", nil))
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "console.log") ||
		rec.Header().Get("Cache-Control") != "public, max-age=31536000, immutable" {
		t.Fatalf("static asset = (%d, cc=%q, %q)", rec.Code, rec.Header().Get("Cache-Control"), rec.Body.String())
	}
	rec = httptest.NewRecorder()
	rt.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/favicon.ico", nil))
	if rec.Code != http.StatusOK || rec.Header().Get("Cache-Control") != "public, max-age=3600" {
		t.Fatalf("favicon = (%d, cc=%q)", rec.Code, rec.Header().Get("Cache-Control"))
	}
	rec = httptest.NewRecorder()
	rt.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/nested.html", nil))
	if rec.Code != http.StatusOK || rec.Header().Get("Cache-Control") != "no-cache" {
		t.Fatalf("nested.html = (%d, cc=%q)", rec.Code, rec.Header().Get("Cache-Control"))
	}
	for _, spaPath := range []string{"/", "/index.html", "/library", "/photo/abc"} {
		rec = httptest.NewRecorder()
		rt.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, spaPath, nil))
		if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "SPA") ||
			rec.Header().Get("Cache-Control") != "no-cache" {
			t.Fatalf("SPA fallback %s = (%d, cc=%q, %q)", spaPath, rec.Code, rec.Header().Get("Cache-Control"), rec.Body.String())
		}
	}
	rec = httptest.NewRecorder()
	rt.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/missing.css", nil))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("missing static asset = %d, want 404", rec.Code)
	}
	rec = httptest.NewRecorder()
	rt.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/unknown", nil))
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("unauthenticated unknown API route = %d, want 401", rec.Code)
	}

	// Rate limit & login cookie flow
	for range 2 {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", strings.NewReader(`{"password":"wrong"}`))
		req.RemoteAddr = "1.2.3.4:1234"
		rec = httptest.NewRecorder()
		rt.ServeHTTP(rec, req)
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("bad login = %d, want 401", rec.Code)
		}
	}
	reqBlocked := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", strings.NewReader(`{"password":"pw"}`))
	reqBlocked.RemoteAddr = "1.2.3.4:1234"
	rec = httptest.NewRecorder()
	rt.ServeHTTP(rec, reqBlocked)
	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("rate limited login = %d, want 429", rec.Code)
	}

	// Malformed JSON and trailing JSON rejected
	for _, body := range []string{"", "{bad", `{"password":"pw"} {}`} {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", strings.NewReader(body))
		req.RemoteAddr = "5.6.7.8:1234"
		rec = httptest.NewRecorder()
		rt.ServeHTTP(rec, req)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("body %q status = %d, want 400", body, rec.Code)
		}
	}

	// Successful login -> authenticated API unknown route 404 -> logout
	reqLogin := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", strings.NewReader(`{"password":"pw"}`))
	reqLogin.RemoteAddr = "5.6.7.8:1234"
	rec = httptest.NewRecorder()
	rt.ServeHTTP(rec, reqLogin)
	if rec.Code != http.StatusOK || len(rec.Result().Cookies()) == 0 {
		t.Fatalf("login failed: %d", rec.Code)
	}
	sessCookie := rec.Result().Cookies()[0]

	reqIdle := httptest.NewRequest(http.MethodGet, "/api/v1/library/sync/status", nil)
	reqIdle.AddCookie(sessCookie)
	rec = httptest.NewRecorder()
	rt.ServeHTTP(rec, reqIdle)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"idle"`) {
		t.Fatalf("idle sync status = (%d, %s)", rec.Code, rec.Body.String())
	}

	reqUnknownAPI := httptest.NewRequest(http.MethodGet, "/api/v1/nonexistent", nil)
	reqUnknownAPI.AddCookie(sessCookie)
	rec = httptest.NewRecorder()
	rt.ServeHTTP(rec, reqUnknownAPI)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("unknown API route = %d, want 404", rec.Code)
	}

	reqLogout := httptest.NewRequest(http.MethodPost, "/api/v1/auth/logout", nil)
	reqLogout.AddCookie(sessCookie)
	rec = httptest.NewRecorder()
	rt.ServeHTTP(rec, reqLogout)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"authenticated":false`) {
		t.Fatalf("logout = (%d, %s)", rec.Code, rec.Body.String())
	}
}

func TestSyncHandlerAsyncAndPhotoHandlerErrors(t *testing.T) {
	t.Parallel()

	gate := make(chan struct{})
	syncSvc := &fakeSyncServices{
		gate:       gate,
		syncReport: domain.SyncReport{ID: "s1", Status: domain.SyncStatusCompleted, AddedCount: 2},
	}
	authSvc := &fakeAuthSvc{authDisabled: true}
	photoSvc := &fakePhotoServices{
		photo: domain.Photo{ID: "p1", BaseName: "IMG_1", MediaType: domain.MediaTypePhoto, Status: domain.FileStatusActive},
		tags:  []domain.EmojiTagCount{{Emoji: "❤", Count: 1}},
	}
	rt := httpapi.NewRouter(httpapi.RouterConfig{
		Auth:         authSvc,
		AuthHandler:  httpapi.NewAuthHandler(authSvc, nil, false),
		PhotoHandler: httpapi.NewPhotoHandler(photoSvc, photoSvc, photoSvc, nil, t.TempDir(), t.TempDir()),
		SyncHandler:  httpapi.NewSyncHandler(syncSvc, syncSvc).WithBaseContext(context.Background()),
	})

	// Start async sync -> 202 Accepted
	rec := httptest.NewRecorder()
	rt.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/v1/library/sync?async=true", nil))
	if rec.Code != http.StatusAccepted {
		t.Fatalf("async sync = %d, want 202", rec.Code)
	}
	// Second concurrent trigger -> 409 Conflict
	rec = httptest.NewRecorder()
	rt.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/v1/library/sync?async=true", nil))
	if rec.Code != http.StatusConflict {
		t.Fatalf("concurrent async sync = %d, want 409", rec.Code)
	}
	rec = httptest.NewRecorder()
	rt.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/v1/library/sync", nil))
	if rec.Code != http.StatusConflict {
		t.Fatalf("concurrent sync trigger = %d, want 409", rec.Code)
	}
	close(gate)

	// Wait briefly by triggering a fresh sync handler Wait via a synchronous call after gate closes
	time.Sleep(20 * time.Millisecond)

	// Invalid request payloads & filters
	badRequests := []struct {
		method string
		target string
		body   string
		want   int
	}{
		{method: http.MethodGet, target: "/api/v1/photos?min_views=bad", want: http.StatusBadRequest},
		{method: http.MethodGet, target: "/api/v1/photos/missing", want: http.StatusNotFound},
		{method: http.MethodPost, target: "/api/v1/photos/p1/views", body: "{invalid", want: http.StatusBadRequest},
		{method: http.MethodPost, target: "/api/v1/photos/p1/views", body: `{"source":"bad"}`, want: http.StatusBadRequest},
		{method: http.MethodPatch, target: "/api/v1/photos/p1/views", body: "", want: http.StatusBadRequest},
		{method: http.MethodPatch, target: "/api/v1/photos/p1/views", body: `{"action":"bogus"}`, want: http.StatusBadRequest},
		{method: http.MethodPost, target: "/api/v1/photos/p1/tags", body: `{"emoji":"not-emoji"}`, want: http.StatusBadRequest},
		{method: http.MethodDelete, target: "/api/v1/photos/p1/tags/ascii", want: http.StatusBadRequest},
		{method: http.MethodGet, target: "/api/v1/photos/p1/thumbnail?format=txt", want: http.StatusBadRequest},
		{method: http.MethodGet, target: "/api/v1/photos/p1/preview?file_id=none", want: http.StatusNotFound},
		{method: http.MethodGet, target: "/api/v1/photos/p1/original?format=cr3", want: http.StatusNotFound},
	}
	for _, tc := range badRequests {
		var bodyReader io.Reader
		if tc.body != "" {
			bodyReader = strings.NewReader(tc.body)
		}
		rec = httptest.NewRecorder()
		rt.ServeHTTP(rec, httptest.NewRequest(tc.method, tc.target, bodyReader))
		if rec.Code != tc.want {
			t.Fatalf("%s %s status = %d, want %d (body=%s)", tc.method, tc.target, rec.Code, tc.want, rec.Body.String())
		}
	}

	// Nil staticFS fallback 404 JSON
	rec = httptest.NewRecorder()
	rt.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/missing-page", nil))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("missing page without StaticFS = %d, want 404", rec.Code)
	}
	var prob httpapi.ProblemResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &prob); err != nil || prob.Code != "NOT_FOUND" {
		t.Fatalf("unexpected fallback problem: (%v, %+v)", err, prob)
	}
}

func TestHandlersNilDependenciesErrorsAndMediaEdgeCases(t *testing.T) {
	t.Parallel()

	// 1. Nil auth service & nil syncer return 500
	nilAuthH := httpapi.NewAuthHandler(nil, nil, false)
	for _, fn := range []func(http.ResponseWriter, *http.Request){nilAuthH.Session, nilAuthH.Login, nilAuthH.Logout} {
		rec := httptest.NewRecorder()
		fn(rec, httptest.NewRequest(http.MethodGet, "/", nil))
		if rec.Code != http.StatusInternalServerError {
			t.Fatalf("nil auth handler status = %d, want 500", rec.Code)
		}
	}

	nilSyncH := httpapi.NewSyncHandler(nil, nil)
	for _, tc := range []struct {
		method string
		fn     func(http.ResponseWriter, *http.Request)
	}{
		{http.MethodPost, nilSyncH.TriggerSync},
		{http.MethodGet, nilSyncH.SyncStatus},
		{http.MethodDelete, nilSyncH.PurgeMissing},
		{http.MethodGet, nilSyncH.Stats},
		{http.MethodGet, nilSyncH.Duplicates},
		{http.MethodGet, nilSyncH.Missing},
	} {
		rec := httptest.NewRecorder()
		tc.fn(rec, httptest.NewRequest(tc.method, "/", nil))
		if rec.Code != http.StatusInternalServerError {
			t.Fatalf("nil sync handler %s status = %d, want 500", tc.method, rec.Code)
		}
	}

	nilPhotoH := httpapi.NewPhotoHandler(nil, nil, nil, nil, "", "")
	for _, tc := range []struct {
		method string
		body   string
		fn     func(http.ResponseWriter, *http.Request)
	}{
		{http.MethodGet, "", nilPhotoH.List},
		{http.MethodGet, "", nilPhotoH.Get},
		{http.MethodPost, `{}`, nilPhotoH.RecordView},
		{http.MethodPatch, `{"action":"increment"}`, nilPhotoH.AdjustViewCount},
		{http.MethodPost, `{"emoji":"❤️"}`, nilPhotoH.AddTag},
		{http.MethodDelete, "", nilPhotoH.RemoveTag},
		{http.MethodGet, "", nilPhotoH.ListTags},
		{http.MethodGet, "", nilPhotoH.Thumbnail},
	} {
		rec := httptest.NewRecorder()
		tc.fn(rec, httptest.NewRequest(tc.method, "/", strings.NewReader(tc.body)))
		if rec.Code != http.StatusInternalServerError {
			t.Fatalf("nil photo handler %s status = %d, want 500", tc.method, rec.Code)
		}
	}

	// Concurrent synchronous + async trigger returns 409; Wait blocks new async starts.
	gate := make(chan struct{})
	syncStarted := make(chan struct{})
	concurrentSync := &signalingSyncService{
		gate:    gate,
		started: syncStarted,
	}
	concH := httpapi.NewSyncHandler(concurrentSync, nil)
	doneSync := make(chan int, 1)
	go func() {
		recSync := httptest.NewRecorder()
		concH.TriggerSync(recSync, httptest.NewRequest(http.MethodPost, "/api/v1/library/sync", nil))
		doneSync <- recSync.Code
	}()
	<-syncStarted
	recAsync := httptest.NewRecorder()
	concH.TriggerSync(recAsync, httptest.NewRequest(http.MethodPost, "/api/v1/library/sync?async=true", nil))
	if recAsync.Code != http.StatusConflict {
		t.Fatalf("async during running sync = %d, want 409", recAsync.Code)
	}
	close(gate)
	if code := <-doneSync; code != http.StatusOK {
		t.Fatalf("sync completion status = %d, want 200", code)
	}
	concH.Wait()
	recAfterWait := httptest.NewRecorder()
	concH.TriggerSync(recAfterWait, httptest.NewRequest(http.MethodPost, "/api/v1/library/sync?async=true", nil))
	if recAfterWait.Code != http.StatusInternalServerError {
		t.Fatalf("async after Wait() status = %d, want 500", recAfterWait.Code)
	}

	// 2. SyncHandler.Wait() + error propagation + HealthHandler invalid dirs
	boom := errors.New("backend failure")
	errSyncSvc := &fakeSyncServices{syncErr: boom, statusErr: boom}
	syncH := httpapi.NewSyncHandler(errSyncSvc, errSyncSvc)
	rec := httptest.NewRecorder()
	syncH.TriggerSync(rec, httptest.NewRequest(http.MethodPost, "/api/v1/library/sync?async=1", nil))
	if rec.Code != http.StatusAccepted {
		t.Fatalf("async=1 status = %d, want 202", rec.Code)
	}
	syncH.Wait()

	authSvc := &fakeAuthSvc{authDisabled: true}
	errPhotoSvc := &fakePhotoServices{err: boom}
	rtErr := httpapi.NewRouter(httpapi.RouterConfig{
		Auth:          authSvc,
		AuthHandler:   httpapi.NewAuthHandler(authSvc, nil, false),
		PhotoHandler:  httpapi.NewPhotoHandler(errPhotoSvc, errPhotoSvc, errPhotoSvc, nil, "", ""),
		SyncHandler:   syncH,
		HealthHandler: httpapi.NewHealthHandler(nil, "", "/nonexistent-dir-xyz"),
	})

	rec = httptest.NewRecorder()
	rtErr.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/auth/session", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /api/v1/auth/session status = %d, want 200", rec.Code)
	}

	for _, ep := range []struct{ method, path string }{
		{http.MethodGet, "/api/v1/photos"},
		{http.MethodPost, "/api/v1/photos/p1/views"},
		{http.MethodPatch, "/api/v1/photos/p1/views"},
		{http.MethodPost, "/api/v1/photos/p1/tags"},
		{http.MethodDelete, "/api/v1/photos/p1/tags/%E2%9D%A4"},
		{http.MethodGet, "/api/v1/tags"},
		{http.MethodPost, "/api/v1/library/sync"},
		{http.MethodGet, "/api/v1/library/sync/status"},
		{http.MethodGet, "/api/v1/library/stats"},
		{http.MethodGet, "/api/v1/library/duplicates"},
		{http.MethodGet, "/api/v1/library/missing"},
		{http.MethodDelete, "/api/v1/library/missing"},
	} {
		var body io.Reader
		if ep.method == http.MethodPatch {
			body = strings.NewReader(`{"action":"increment"}`)
		} else if ep.method == http.MethodPost && strings.Contains(ep.path, "/tags") {
			body = strings.NewReader(`{"emoji":"❤️"}`)
		}
		rec = httptest.NewRecorder()
		rtErr.ServeHTTP(rec, httptest.NewRequest(ep.method, ep.path, body))
		if rec.Code != http.StatusInternalServerError {
			t.Fatalf("%s %s status = %d, want 500", ep.method, ep.path, rec.Code)
		}
	}

	rec = httptest.NewRecorder()
	rtErr.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/health", nil))
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("health with nil pinger/bad dirs = %d, want 503", rec.Code)
	}

	// 3. Media format selection (?file_id match), missing files on disk, and MIME types
	libDir := t.TempDir()
	cacheDir := t.TempDir()
	formats := []domain.MediaFormat{
		domain.FormatPNG, domain.FormatWebP, domain.FormatAVIF, domain.FormatGIF,
		domain.FormatHEIC, domain.FormatHEIF, domain.FormatMOV, domain.FormatWebM,
		domain.FormatMKV, domain.MediaFormat("unknown"),
	}
	var files []domain.PhotoFile
	for i, fmtVal := range formats {
		name := "file_" + string(fmtVal) + "." + string(fmtVal)
		writeFileBytes(t, libDir+"/"+name, []byte("media-bytes"))
		files = append(files, domain.PhotoFile{
			ID: "fid-" + string(fmtVal), PhotoID: "p-media", ContentHash: "h-" + string(fmtVal),
			RelativePath: name, FileName: name, Format: fmtVal, Status: domain.FileStatusActive,
		})
		_ = i
	}
	mediaPhotoSvc := &fakePhotoServices{
		photo: domain.Photo{ID: "p-media", MediaType: domain.MediaTypePhoto, Status: domain.FileStatusActive, Files: files},
	}
	rtMedia := httpapi.NewRouter(httpapi.RouterConfig{
		Auth:         authSvc,
		PhotoHandler: httpapi.NewPhotoHandler(mediaPhotoSvc, nil, mediaPhotoSvc, nil, libDir, cacheDir),
	})

	// Detail with nil tracker still succeeds
	rec = httptest.NewRecorder()
	rtMedia.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/photos/p-media", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("GET detail with nil tracker = %d, want 200", rec.Code)
	}
	// Missing cached thumbnail with nil preview generator -> 404
	rec = httptest.NewRecorder()
	rtMedia.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/photos/p-media/thumbnail?file_id=fid-png", nil))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("missing cached thumb = %d, want 404", rec.Code)
	}
	// Originals across all MIME formats via ?file_id
	for _, fmtVal := range formats {
		rec = httptest.NewRecorder()
		rtMedia.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/photos/p-media/original?file_id=fid-"+string(fmtVal), nil))
		if rec.Code != http.StatusOK || rec.Header().Get("Content-Type") == "" {
			t.Fatalf("original for %s = (%d, %q)", fmtVal, rec.Code, rec.Header().Get("Content-Type"))
		}
	}
	// Missing library file on disk -> 404
	mediaPhotoSvc.photo.Files = []domain.PhotoFile{{
		ID: "f-missing-disk", PhotoID: "p-media", ContentHash: "h-miss",
		RelativePath: "does-not-exist.jpg", FileName: "does-not-exist.jpg",
		Format: domain.FormatJPG, Status: domain.FileStatusActive,
	}}
	rec = httptest.NewRecorder()
	rtMedia.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/photos/p-media/original", nil))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("missing disk original = %d, want 404", rec.Code)
	}
}

type signalingSyncService struct {
	gate    chan struct{}
	started chan struct{}
}

func (s *signalingSyncService) Sync(ctx context.Context) (domain.SyncReport, error) {
	close(s.started)
	select {
	case <-s.gate:
		return domain.SyncReport{ID: "s-conc", Status: domain.SyncStatusCompleted}, nil
	case <-ctx.Done():
		return domain.SyncReport{}, ctx.Err()
	}
}

func (s *signalingSyncService) Status(_ context.Context) (domain.SyncReport, error) {
	return domain.SyncReport{}, domain.ErrSyncNotFound
}

func (s *signalingSyncService) PurgeMissing(_ context.Context) (int64, error) {
	return 0, nil
}

package httpapi

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"sync"

	"github.com/lucastheo/photos/internal/domain"
)

// LibrarySyncer defines library synchronization operations consumed by SyncHandler.
type LibrarySyncer interface {
	Sync(ctx context.Context) (domain.SyncReport, error)
	Status(ctx context.Context) (domain.SyncReport, error)
	PurgeMissing(ctx context.Context) (int64, error)
}

// LibraryStatsReader defines read-only library statistics and duplicate/missing reports.
type LibraryStatsReader interface {
	GetLibraryStats(ctx context.Context) (domain.LibraryStats, error)
	ListDuplicates(ctx context.Context) ([]domain.DuplicateGroup, error)
	ListMissing(ctx context.Context) ([]domain.Photo, error)
}

// HealthPinger checks database connectivity for readiness probes.
type HealthPinger interface {
	PingContext(ctx context.Context) error
}

// SyncHandler handles library synchronization, statistics, duplicate reports,
// and missing-file purge endpoints.
type SyncHandler struct {
	syncer   LibrarySyncer
	reader   LibraryStatsReader
	baseCtx  context.Context
	logger   *slog.Logger
	mu       sync.Mutex
	active   bool
	stopping bool
	wg       sync.WaitGroup
}

// NewSyncHandler constructs a concrete SyncHandler.
func NewSyncHandler(syncer LibrarySyncer, reader LibraryStatsReader) *SyncHandler {
	return &SyncHandler{
		syncer:  syncer,
		reader:  reader,
		baseCtx: context.Background(),
	}
}

// WithBaseContext configures the root lifecycle context used for asynchronous sync jobs.
func (h *SyncHandler) WithBaseContext(ctx context.Context) *SyncHandler {
	if ctx != nil {
		h.baseCtx = ctx
	}
	return h
}

// WithLogger attaches a structured logger to SyncHandler.
func (h *SyncHandler) WithLogger(logger *slog.Logger) *SyncHandler {
	h.logger = logger
	return h
}

// Wait prevents new asynchronous sync jobs from starting and waits until all
// active background sync jobs launched by SyncHandler complete.
func (h *SyncHandler) Wait() {
	h.mu.Lock()
	h.stopping = true
	h.mu.Unlock()
	h.wg.Wait()
}

// TriggerSync handles POST /api/v1/library/sync, running synchronously by default
// or asynchronously when ?async=true is supplied.
func (h *SyncHandler) TriggerSync(w http.ResponseWriter, r *http.Request) {
	if h.syncer == nil {
		WriteProblem(w, r, h.logger, errors.New("sync service unavailable"))
		return
	}
	asyncParam := strings.TrimSpace(r.URL.Query().Get("async"))
	if strings.EqualFold(asyncParam, "true") || asyncParam == "1" {
		h.triggerAsyncSync(w, r)
		return
	}
	if err := h.tryBeginSync(false); err != nil {
		WriteProblem(w, r, h.logger, err)
		return
	}
	defer h.endSync()
	report, err := h.syncer.Sync(r.Context())
	if err != nil {
		WriteProblem(w, r, h.logger, err)
		return
	}
	writeJSON(w, http.StatusOK, toSyncReportResponse(report))
}

func (h *SyncHandler) triggerAsyncSync(w http.ResponseWriter, r *http.Request) {
	if err := h.tryBeginSync(true); err != nil {
		WriteProblem(w, r, h.logger, err)
		return
	}
	go func() {
		defer h.wg.Done()
		defer h.endSync()
		if _, err := h.syncer.Sync(h.baseCtx); err != nil && h.logger != nil {
			h.logger.WarnContext(h.baseCtx, "async library sync finished with error", "error", err)
		}
	}()
	writeJSON(w, http.StatusAccepted, SyncReportResponse{Status: string(domain.SyncStatusRunning)})
}

func (h *SyncHandler) tryBeginSync(isAsync bool) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	if isAsync && (h.stopping || h.baseCtx.Err() != nil) {
		return errors.New("sync handler shutting down")
	}
	if h.active {
		return domain.ErrSyncInProgress
	}
	h.active = true
	if isAsync {
		h.wg.Add(1)
	}
	return nil
}

func (h *SyncHandler) endSync() {
	h.mu.Lock()
	h.active = false
	h.mu.Unlock()
}

// SyncStatus handles GET /api/v1/library/sync/status.
func (h *SyncHandler) SyncStatus(w http.ResponseWriter, r *http.Request) {
	if h.syncer == nil {
		WriteProblem(w, r, h.logger, errors.New("sync service unavailable"))
		return
	}
	report, err := h.syncer.Status(r.Context())
	if err != nil {
		if errors.Is(err, domain.ErrSyncNotFound) {
			writeJSON(w, http.StatusOK, SyncReportResponse{Status: "idle"})
			return
		}
		WriteProblem(w, r, h.logger, err)
		return
	}
	writeJSON(w, http.StatusOK, toSyncReportResponse(report))
}

// Stats handles GET /api/v1/library/stats.
func (h *SyncHandler) Stats(w http.ResponseWriter, r *http.Request) {
	if h.reader == nil {
		WriteProblem(w, r, h.logger, errors.New("stats reader unavailable"))
		return
	}
	stats, err := h.reader.GetLibraryStats(r.Context())
	if err != nil {
		WriteProblem(w, r, h.logger, err)
		return
	}
	writeJSON(w, http.StatusOK, toLibraryStatsResponse(stats))
}

// Duplicates handles GET /api/v1/library/duplicates.
func (h *SyncHandler) Duplicates(w http.ResponseWriter, r *http.Request) {
	if h.reader == nil {
		WriteProblem(w, r, h.logger, errors.New("stats reader unavailable"))
		return
	}
	groups, err := h.reader.ListDuplicates(r.Context())
	if err != nil {
		WriteProblem(w, r, h.logger, err)
		return
	}
	writeJSON(w, http.StatusOK, toDuplicateGroupDTOs(groups))
}

// Missing handles GET /api/v1/library/missing.
func (h *SyncHandler) Missing(w http.ResponseWriter, r *http.Request) {
	if h.reader == nil {
		WriteProblem(w, r, h.logger, errors.New("stats reader unavailable"))
		return
	}
	items, err := h.reader.ListMissing(r.Context())
	if err != nil {
		WriteProblem(w, r, h.logger, err)
		return
	}
	out := make([]PhotoResponse, 0, len(items))
	for _, item := range items {
		out = append(out, toPhotoResponse(item, false, nil))
	}
	writeJSON(w, http.StatusOK, out)
}

// PurgeMissing handles DELETE /api/v1/library/missing.
func (h *SyncHandler) PurgeMissing(w http.ResponseWriter, r *http.Request) {
	if h.syncer == nil {
		WriteProblem(w, r, h.logger, errors.New("sync service unavailable"))
		return
	}
	deleted, err := h.syncer.PurgeMissing(r.Context())
	if err != nil {
		WriteProblem(w, r, h.logger, err)
		return
	}
	writeJSON(w, http.StatusOK, PurgeMissingResponse{DeletedCount: deleted})
}

// HealthHandler checks SQLite connectivity and library/cache directory availability.
type HealthHandler struct {
	pinger      HealthPinger
	libraryPath string
	cachePath   string
}

// NewHealthHandler constructs a concrete HealthHandler.
func NewHealthHandler(pinger HealthPinger, libraryPath, cachePath string) *HealthHandler {
	return &HealthHandler{
		pinger:      pinger,
		libraryPath: libraryPath,
		cachePath:   cachePath,
	}
}

// Check handles GET /api/v1/health.
func (h *HealthHandler) Check(w http.ResponseWriter, r *http.Request) {
	dbStatus := "ok"
	if h.pinger == nil || h.pinger.PingContext(r.Context()) != nil {
		dbStatus = "unavailable"
	}
	libStatus := checkDirStatus(h.libraryPath)
	cacheStatus := checkDirStatus(h.cachePath)

	status := http.StatusOK
	overall := "ok"
	if dbStatus != "ok" || libStatus != "ok" || cacheStatus != "ok" {
		status = http.StatusServiceUnavailable
		overall = "degraded"
	}
	writeJSON(w, status, HealthResponse{
		Status:   overall,
		Database: dbStatus,
		Library:  libStatus,
		Cache:    cacheStatus,
	})
}

func checkDirStatus(dirPath string) string {
	if strings.TrimSpace(dirPath) == "" {
		return "unavailable"
	}
	root, err := os.OpenRoot(dirPath)
	if err != nil {
		return "unavailable"
	}
	defer func() { _ = root.Close() }()
	info, err := root.Stat(".")
	if err != nil || !info.IsDir() {
		return "unavailable"
	}
	return "ok"
}

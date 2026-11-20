package usecase

import (
	"context"
	"errors"
	"fmt"
	"sync/atomic"
	"time"

	"github.com/lucastheo/photos/internal/domain"
)

const staleRunReason = "interrupted by server restart"

// SyncPhotoStore defines the persistence operations required by
// SyncLibraryUseCase to reconcile library files with logical photos.
type SyncPhotoStore interface {
	ListAllFiles(ctx context.Context) ([]domain.PhotoFile, error)
	GetByID(ctx context.Context, photoID string) (domain.Photo, error)
	GetByStackKey(ctx context.Context, stackKey string) (domain.Photo, error)
	FindFilesByContentHash(ctx context.Context, contentHash string) ([]domain.PhotoFile, error)
	CreatePhoto(ctx context.Context, photo domain.Photo) error
	AttachFile(ctx context.Context, photoID string, file domain.PhotoFile) error
	UpdateFileInPlace(ctx context.Context, file domain.PhotoFile) error
	UpdatePhotoMetadata(ctx context.Context, photo domain.Photo) error
	MoveFilesBatch(ctx context.Context, moves []domain.FileMove, now time.Time) error
	SplitMovedFile(ctx context.Context, fileID string, newPhoto domain.Photo, move domain.FileMove, now time.Time) error
	MarkFilesMissing(ctx context.Context, fileIDs []string, now time.Time) error
	PurgeMissing(ctx context.Context) (int64, error)
}

// SyncRunStore tracks the lifecycle and counters of library synchronization runs.
type SyncRunStore interface {
	CreateSyncRun(ctx context.Context, report domain.SyncReport) error
	UpdateSyncRun(ctx context.Context, report domain.SyncReport) error
	GetLatestSyncRun(ctx context.Context) (domain.SyncReport, error)
	ListSyncRuns(ctx context.Context, limit int) ([]domain.SyncReport, error)
	RecoverStaleRuns(ctx context.Context, reason string, now time.Time) (int64, error)
}

// DatabaseMaintainer handles database backup and WAL checkpoint operations
// surrounding destructive purge and sync completion boundaries.
type DatabaseMaintainer interface {
	Backup(ctx context.Context, suffix string) (string, error)
	Checkpoint(ctx context.Context) error
}

// MediaScanner discovers media files on disk and hashes changed or new files.
type MediaScanner interface {
	Walk(ctx context.Context) ([]domain.ScannedFile, error)
	HashFiles(ctx context.Context, files []domain.ScannedFile) ([]domain.ScannedFile, error)
}

// MetadataExtractor parses dimensions, capture timestamps, duration, and EXIF
// metadata from a media file inside the library.
type MetadataExtractor interface {
	Extract(ctx context.Context, relativePath string, format domain.MediaFormat) (domain.MediaMetadata, error)
}

// PreviewExtractor ensures cached grid thumbnails and web previews exist for a
// scanned or updated media file.
type PreviewExtractor interface {
	EnsureCache(ctx context.Context, relativePath, contentHash string, format domain.MediaFormat, orientation int) error
}

// SyncLibraryUseCase orchestrates incremental 4-step library synchronization,
// missing-record purging, sync status retrieval, and stale run recovery.
type SyncLibraryUseCase struct {
	photos     SyncPhotoStore
	scanner    MediaScanner
	previews   PreviewExtractor
	extractor  MetadataExtractor
	runs       SyncRunStore
	maintainer DatabaseMaintainer
	now        func() time.Time
	newID      func() string
	running    atomic.Bool
}

// NewSyncLibraryUseCase constructs a concrete SyncLibraryUseCase configured with
// required photo persistence, media scanner, and preview cache extractor ports.
func NewSyncLibraryUseCase(
	photos SyncPhotoStore,
	scanner MediaScanner,
	previews PreviewExtractor,
) *SyncLibraryUseCase {
	return &SyncLibraryUseCase{
		photos:   photos,
		scanner:  scanner,
		previews: previews,
		now:      func() time.Time { return time.Now().UTC() },
		newID:    newUUIDv4,
	}
}

// WithMetadataExtractor configures an optional MetadataExtractor for EXIF and
// media dimension extraction during sync.
func (u *SyncLibraryUseCase) WithMetadataExtractor(extractor MetadataExtractor) *SyncLibraryUseCase {
	u.extractor = extractor
	return u
}

// WithSyncRunStore configures an optional SyncRunStore for persisting sync
// progress and history.
func (u *SyncLibraryUseCase) WithSyncRunStore(runs SyncRunStore) *SyncLibraryUseCase {
	u.runs = runs
	return u
}

// WithDatabaseMaintainer configures an optional DatabaseMaintainer for
// post-sync WAL checkpoints and pre-purge database backups.
func (u *SyncLibraryUseCase) WithDatabaseMaintainer(maintainer DatabaseMaintainer) *SyncLibraryUseCase {
	u.maintainer = maintainer
	return u
}

// WithClock overrides the clock function used for sync timestamps.
func (u *SyncLibraryUseCase) WithClock(nowFunc func() time.Time) *SyncLibraryUseCase {
	if nowFunc != nil {
		u.now = nowFunc
	}
	return u
}

// WithIDGenerator overrides the ID generator used for new photos, files, and
// sync runs.
func (u *SyncLibraryUseCase) WithIDGenerator(idFunc func() string) *SyncLibraryUseCase {
	if idFunc != nil {
		u.newID = idFunc
	}
	return u
}

// Sync executes an incremental 4-step library scan and reconciliation pass,
// guarding against concurrent sync executions.
func (u *SyncLibraryUseCase) Sync(ctx context.Context) (domain.SyncReport, error) {
	if u.photos == nil || u.scanner == nil {
		return domain.SyncReport{}, errors.New("sync use case requires photo store and media scanner")
	}
	if !u.running.CompareAndSwap(false, true) {
		return domain.SyncReport{}, domain.ErrSyncInProgress
	}
	defer u.running.Store(false)

	report := domain.SyncReport{
		ID:        u.newID(),
		StartedAt: u.now().UTC(),
		Status:    domain.SyncStatusRunning,
	}
	if u.runs != nil {
		if err := u.runs.CreateSyncRun(ctx, report); err != nil {
			return report, fmt.Errorf("create sync run: %w", err)
		}
	}

	if err := u.runSyncPipeline(ctx, &report); err != nil {
		return report, u.failSyncRun(ctx, &report, err)
	}
	return u.finishSyncRun(ctx, report)
}

func (u *SyncLibraryUseCase) runSyncPipeline(ctx context.Context, report *domain.SyncReport) error {
	scanned, existing, err := u.preflight(ctx)
	if err != nil {
		return err
	}
	report.ScannedCount = len(scanned)

	diffNow := u.now().UTC()
	diff, err := u.computeFastDiff(ctx, scanned, existing, diffNow, report)
	if err != nil {
		return err
	}

	var hashed []domain.ScannedFile
	if len(diff.toHash) > 0 {
		hashed, err = u.scanner.HashFiles(ctx, diff.toHash)
		if err != nil {
			return fmt.Errorf("hash media files: %w", err)
		}
	}

	reconcileNow := u.now().UTC()
	if err := u.reconcileHashed(ctx, hashed, diff, reconcileNow, report); err != nil {
		return err
	}
	return u.markRemainingMissing(ctx, diff.unseen, diff.consumedIDs, reconcileNow, report)
}

func (u *SyncLibraryUseCase) preflight(ctx context.Context) ([]domain.ScannedFile, []domain.PhotoFile, error) {
	rawScanned, err := u.scanner.Walk(ctx)
	if err != nil {
		return nil, nil, fmt.Errorf("%w: %w", domain.ErrLibraryUnavailable, err)
	}
	existing, err := u.photos.ListAllFiles(ctx)
	if err != nil {
		return nil, nil, fmt.Errorf("list existing files: %w", err)
	}
	scanned := normalizeScannedFiles(rawScanned)
	if len(scanned) == 0 && hasActiveFile(existing) {
		return nil, nil, fmt.Errorf(
			"%w: scanner discovered 0 files while active records exist",
			domain.ErrLibraryUnavailable,
		)
	}
	return scanned, existing, nil
}

func (u *SyncLibraryUseCase) failSyncRun(ctx context.Context, report *domain.SyncReport, cause error) error {
	finishedAt := u.now().UTC()
	report.FinishedAt = &finishedAt
	report.Status = domain.SyncStatusFailed
	report.ErrorMessage = cause.Error()
	if u.runs == nil {
		return cause
	}
	if err := u.runs.UpdateSyncRun(context.WithoutCancel(ctx), *report); err != nil {
		return errors.Join(cause, fmt.Errorf("update failed sync run: %w", err))
	}
	return cause
}

func (u *SyncLibraryUseCase) finishSyncRun(ctx context.Context, report domain.SyncReport) (domain.SyncReport, error) {
	finishedAt := u.now().UTC()
	report.FinishedAt = &finishedAt
	report.Status = domain.SyncStatusCompleted
	if u.runs != nil {
		if err := u.runs.UpdateSyncRun(ctx, report); err != nil {
			return report, fmt.Errorf("update sync run: %w", err)
		}
	}
	if u.maintainer != nil {
		if err := u.maintainer.Checkpoint(ctx); err != nil {
			return report, fmt.Errorf("checkpoint database: %w", err)
		}
	}
	return report, nil
}

// PurgeMissing creates a pre-purge database backup when a DatabaseMaintainer
// is configured and then deletes all missing files and orphaned missing photos.
func (u *SyncLibraryUseCase) PurgeMissing(ctx context.Context) (int64, error) {
	if u.photos == nil {
		return 0, errors.New("sync use case requires photo store")
	}
	if u.maintainer != nil {
		suffix := fmt.Sprintf("pre_purge_%d", u.now().UTC().Unix())
		if _, err := u.maintainer.Backup(ctx, suffix); err != nil {
			return 0, fmt.Errorf("backup before purge: %w", err)
		}
	}
	purged, err := u.photos.PurgeMissing(ctx)
	if err != nil {
		return 0, fmt.Errorf("purge missing records: %w", err)
	}
	return purged, nil
}

// Status returns the latest recorded synchronization report, or
// domain.ErrSyncNotFound when no SyncRunStore or sync history exists.
func (u *SyncLibraryUseCase) Status(ctx context.Context) (domain.SyncReport, error) {
	if u.runs == nil {
		return domain.SyncReport{}, domain.ErrSyncNotFound
	}
	report, err := u.runs.GetLatestSyncRun(ctx)
	if err != nil {
		return domain.SyncReport{}, fmt.Errorf("get latest sync run: %w", err)
	}
	return report, nil
}

// RecoverStaleRuns marks any leftover running sync records as failed after an
// unexpected shutdown or crash.
func (u *SyncLibraryUseCase) RecoverStaleRuns(ctx context.Context) (int64, error) {
	if u.runs == nil {
		return 0, nil
	}
	recovered, err := u.runs.RecoverStaleRuns(ctx, staleRunReason, u.now().UTC())
	if err != nil {
		return 0, fmt.Errorf("recover stale sync runs: %w", err)
	}
	return recovered, nil
}

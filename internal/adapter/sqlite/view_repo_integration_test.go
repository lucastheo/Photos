package sqlite_test

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/lucastheo/photos/internal/adapter/sqlite"
	"github.com/lucastheo/photos/internal/domain"
)

func seedSingleViewTestPhoto(t *testing.T, db *sqlite.DB, photoID string) {
	t.Helper()
	now := time.Date(2026, 6, 1, 10, 0, 0, 0, time.UTC)
	err := sqlite.NewPhotoRepository(db.SQL()).CreatePhoto(context.Background(), makeSamplePhoto(
		photoID, "Trips", "IMG_1", domain.MediaTypePhoto,
		[]domain.PhotoFile{
			{
				ID: "file-" + photoID, ContentHash: "hash-" + photoID,
				RelativePath: "Trips/IMG_1.jpg", FileName: "IMG_1.jpg",
				FolderPath: "Trips", Format: domain.FormatJPG, FileSize: 100, ModTime: now,
			},
		},
		now,
	))
	if err != nil {
		t.Fatalf("seed photo failed: %v", err)
	}
}

func TestViewRepository_Integration_RecordViewAndIdempotency(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	ctx := context.Background()
	db := openTestDB(t, filepath.Join(t.TempDir(), "photos.db"))
	seedSingleViewTestPhoto(t, db, "p1")
	repo := sqlite.NewViewRepository(db.SQL())

	t1 := time.Date(2026, 6, 2, 10, 0, 0, 123, time.UTC)
	t2 := t1.Add(5 * time.Second)

	// Invalid source & unknown photo errors.
	if _, err := repo.RecordView(ctx, domain.ViewEvent{PhotoID: "p1", Source: "bad"}); !errors.Is(err, domain.ErrInvalidViewSource) {
		t.Fatalf("invalid source err = %v, want ErrInvalidViewSource", err)
	}
	if _, err := repo.RecordView(ctx, domain.ViewEvent{PhotoID: "missing", Source: domain.ViewSourceDwell}); !errors.Is(err, domain.ErrPhotoNotFound) {
		t.Fatalf("unknown photo err = %v, want ErrPhotoNotFound", err)
	}

	// First record with ViewID increments to 1.
	p, err := repo.RecordView(ctx, domain.ViewEvent{
		ViewID:   "view-uuid-1",
		PhotoID:  "p1",
		ViewedAt: t1,
		Source:   domain.ViewSourceDwell,
	})
	if err != nil || p.ViewCount != 1 || p.LastViewedAt == nil || !p.LastViewedAt.Equal(t1) {
		t.Fatalf("first RecordView = (%+v, %v)", p, err)
	}

	// Idempotent replay with same ViewID keeps ViewCount == 1.
	pReplay, err := repo.RecordView(ctx, domain.ViewEvent{
		ViewID:   "view-uuid-1",
		PhotoID:  "p1",
		ViewedAt: t2,
		Source:   domain.ViewSourceDwell,
	})
	if err != nil || pReplay.ViewCount != 1 || !pReplay.LastViewedAt.Equal(t1) {
		t.Fatalf("idempotent replay = (%+v, %v)", pReplay, err)
	}

	// Multiple empty-ViewID events do not collide on UNIQUE(view_id).
	for i := range 2 {
		p, err = repo.RecordView(ctx, domain.ViewEvent{
			PhotoID:  "p1",
			ViewedAt: t2.Add(time.Duration(i) * time.Second),
			Source:   domain.ViewSourceSlideshow,
		})
		if err != nil {
			t.Fatalf("empty ViewID RecordView #%d failed: %v", i, err)
		}
	}
	if p.ViewCount != 3 {
		t.Fatalf("ViewCount = %d, want 3", p.ViewCount)
	}
}

func TestViewRepository_Integration_AdjustViewCountSync(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	ctx := context.Background()
	db := openTestDB(t, filepath.Join(t.TempDir(), "photos.db"))
	seedSingleViewTestPhoto(t, db, "p1")
	repo := sqlite.NewViewRepository(db.SQL())

	if _, err := repo.AdjustViewCount(ctx, "p1", "bad", time.Now()); !errors.Is(err, domain.ErrInvalidViewAction) {
		t.Fatalf("invalid action err = %v, want ErrInvalidViewAction", err)
	}
	if _, err := repo.AdjustViewCount(ctx, "missing", domain.ViewActionIncrement, time.Now()); !errors.Is(err, domain.ErrPhotoNotFound) {
		t.Fatalf("missing photo err = %v, want ErrPhotoNotFound", err)
	}
	if _, err := repo.ListViewHistory(ctx, "missing", 10); !errors.Is(err, domain.ErrPhotoNotFound) {
		t.Fatalf("ListViewHistory missing photo err = %v, want ErrPhotoNotFound", err)
	}

	t1 := time.Date(2026, 6, 3, 10, 0, 0, 0, time.UTC)
	t2 := t1.Add(time.Minute)

	// Decrement at 0 clamps to 0.
	p0, err := repo.AdjustViewCount(ctx, "p1", domain.ViewActionDecrement, t1)
	if err != nil || p0.ViewCount != 0 || p0.LastViewedAt != nil {
		t.Fatalf("decrement at 0 = (%+v, %v)", p0, err)
	}

	// Increment twice at t1, t2.
	if _, err := repo.AdjustViewCount(ctx, "p1", domain.ViewActionIncrement, t1); err != nil {
		t.Fatalf("increment 1 failed: %v", err)
	}
	p2, err := repo.AdjustViewCount(ctx, "p1", domain.ViewActionIncrement, t2)
	if err != nil || p2.ViewCount != 2 || p2.LastViewedAt == nil || !p2.LastViewedAt.Equal(t2) {
		t.Fatalf("increment 2 = (%+v, %v)", p2, err)
	}

	// Decrement removes t2 and rolls LastViewedAt back to t1.
	pDec, err := repo.AdjustViewCount(ctx, "p1", domain.ViewActionDecrement, t2.Add(time.Minute))
	if err != nil || pDec.ViewCount != 1 || pDec.LastViewedAt == nil || !pDec.LastViewedAt.Equal(t1) {
		t.Fatalf("decrement rollback = (%+v, %v)", pDec, err)
	}

	history, err := repo.ListViewHistory(ctx, "p1", 10)
	if err != nil || len(history) != 1 || history[0].Source != domain.ViewSourceManual {
		t.Fatalf("ListViewHistory = (%+v, %v)", history, err)
	}

	// Reset clears history and sets view_count=0, LastViewedAt=nil.
	pReset, err := repo.AdjustViewCount(ctx, "p1", domain.ViewActionReset, t2)
	if err != nil || pReset.ViewCount != 0 || pReset.LastViewedAt != nil {
		t.Fatalf("reset = (%+v, %v)", pReset, err)
	}
	historyAfterReset, err := repo.ListViewHistory(ctx, "p1", 0)
	if err != nil || len(historyAfterReset) != 0 {
		t.Fatalf("ListViewHistory after reset = (%+v, %v)", historyAfterReset, err)
	}
}

func TestViewRepository_Integration_ConcurrentRecordView(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	ctx := context.Background()
	db := openTestDB(t, filepath.Join(t.TempDir(), "photos.db"))
	seedSingleViewTestPhoto(t, db, "p1")
	repo := sqlite.NewViewRepository(db.SQL())

	const workers = 20
	var wg sync.WaitGroup
	errs := make(chan error, workers)
	base := time.Date(2026, 6, 4, 10, 0, 0, 0, time.UTC)

	for i := range workers {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			_, err := repo.RecordView(ctx, domain.ViewEvent{
				ViewID:   fmt.Sprintf("concurrent-%d", idx),
				PhotoID:  "p1",
				ViewedAt: base.Add(time.Duration(idx) * time.Millisecond),
				Source:   domain.ViewSourceDwell,
			})
			if err != nil {
				errs <- err
			}
		}(i)
	}
	wg.Wait()
	close(errs)

	for err := range errs {
		t.Fatalf("concurrent RecordView error: %v", err)
	}

	photo, err := sqlite.NewPhotoRepository(db.SQL()).GetByID(ctx, "p1")
	if err != nil {
		t.Fatalf("GetByID failed: %v", err)
	}
	history, err := repo.ListViewHistory(ctx, "p1", 100)
	if err != nil {
		t.Fatalf("ListViewHistory failed: %v", err)
	}
	if photo.ViewCount != workers || len(history) != workers {
		t.Fatalf("ViewCount=%d history=%d, want %d", photo.ViewCount, len(history), workers)
	}

	// Zero-timestamp defaults + closed DB errors.
	if _, err := repo.RecordView(ctx, domain.ViewEvent{PhotoID: "p1", Source: domain.ViewSourceDwell}); err != nil {
		t.Fatalf("RecordView zero timestamp failed: %v", err)
	}
	if _, err := repo.AdjustViewCount(ctx, "p1", domain.ViewActionIncrement, time.Time{}); err != nil {
		t.Fatalf("AdjustViewCount zero time failed: %v", err)
	}
	if err := db.Close(); err != nil {
		t.Fatalf("db.Close failed: %v", err)
	}
	if _, err := repo.RecordView(ctx, domain.ViewEvent{PhotoID: "p1", Source: domain.ViewSourceDwell}); err == nil {
		t.Fatal("expected RecordView error on closed DB")
	}
	if _, err := repo.AdjustViewCount(ctx, "p1", domain.ViewActionIncrement, base); err == nil {
		t.Fatal("expected AdjustViewCount error on closed DB")
	}
	if _, err := repo.ListViewHistory(ctx, "p1", 5); err == nil {
		t.Fatal("expected ListViewHistory error on closed DB")
	}
}

func TestViewRepository_Integration_RollbackOnSummaryFailure(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	ctx := context.Background()
	db := openTestDB(t, filepath.Join(t.TempDir(), "photos.db"))
	seedSingleViewTestPhoto(t, db, "p1")
	repo := sqlite.NewViewRepository(db.SQL())

	if _, err := db.SQL().ExecContext(ctx, `
		CREATE TRIGGER block_photo_view_summary BEFORE UPDATE ON photos
		BEGIN SELECT RAISE(ABORT, 'summary update blocked'); END`); err != nil {
		t.Fatalf("create trigger failed: %v", err)
	}

	if _, err := repo.RecordView(ctx, domain.ViewEvent{
		ViewID:  "rollback-view",
		PhotoID: "p1",
		Source:  domain.ViewSourceDwell,
	}); err == nil {
		t.Fatal("expected RecordView error when summary update aborts")
	}
	if _, err := repo.AdjustViewCount(ctx, "p1", domain.ViewActionIncrement, time.Now()); err == nil {
		t.Fatal("expected AdjustViewCount error when summary update aborts")
	}

	history, err := repo.ListViewHistory(ctx, "p1", 10)
	if err != nil || len(history) != 0 {
		t.Fatalf("expected 0 photo_views after rollback, got %d (err=%v)", len(history), err)
	}

	// Triggers on photo_views exercise insert/delete failure paths cleanly.
	if _, err := db.SQL().ExecContext(ctx, `DROP TRIGGER block_photo_view_summary`); err != nil {
		t.Fatalf("drop summary trigger failed: %v", err)
	}
	if _, err := repo.RecordView(ctx, domain.ViewEvent{PhotoID: "p1", Source: domain.ViewSourceDwell}); err != nil {
		t.Fatalf("seed view for delete trigger failed: %v", err)
	}
	if _, err := db.SQL().ExecContext(ctx, `
		CREATE TRIGGER block_view_insert BEFORE INSERT ON photo_views
		BEGIN SELECT RAISE(ABORT, 'view insert blocked'); END;
		CREATE TRIGGER block_view_delete BEFORE DELETE ON photo_views
		BEGIN SELECT RAISE(ABORT, 'view delete blocked'); END`); err != nil {
		t.Fatalf("create photo_views triggers failed: %v", err)
	}
	if _, err := repo.RecordView(ctx, domain.ViewEvent{PhotoID: "p1", Source: domain.ViewSourceDwell}); err == nil {
		t.Fatal("expected RecordView error when insert trigger aborts")
	}
	if _, err := repo.RecordView(ctx, domain.ViewEvent{ViewID: "idemp", PhotoID: "p1", Source: domain.ViewSourceDwell}); err == nil {
		t.Fatal("expected idempotent RecordView error when insert trigger aborts")
	}
	for _, act := range []domain.ViewAction{domain.ViewActionIncrement, domain.ViewActionDecrement, domain.ViewActionReset} {
		if _, err := repo.AdjustViewCount(ctx, "p1", act, time.Now()); err == nil {
			t.Fatalf("expected AdjustViewCount(%s) error when trigger aborts", act)
		}
	}
}

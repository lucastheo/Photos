package sqlite_test

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/lucastheo/photos/internal/adapter/sqlite"
	"github.com/lucastheo/photos/internal/domain"
)

func TestSyncRepository_Integration_LifecycleAndRecovery(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	ctx := context.Background()
	db := openTestDB(t, filepath.Join(t.TempDir(), "photos.db"))
	repo := sqlite.NewSyncRepository(db.SQL())

	if _, err := repo.GetLatestSyncRun(ctx); !errors.Is(err, domain.ErrSyncNotFound) {
		t.Fatalf("empty GetLatestSyncRun err = %v, want ErrSyncNotFound", err)
	}
	if err := repo.UpdateSyncRun(ctx, domain.SyncReport{ID: "missing"}); !errors.Is(err, domain.ErrSyncNotFound) {
		t.Fatalf("UpdateSyncRun missing err = %v, want ErrSyncNotFound", err)
	}

	t1 := time.Date(2026, 7, 1, 9, 0, 0, 0, time.UTC)
	run1 := domain.SyncReport{
		ID:        "run-1",
		StartedAt: t1,
		Status:    domain.SyncStatusRunning,
	}
	if err := repo.CreateSyncRun(ctx, run1); err != nil {
		t.Fatalf("CreateSyncRun run-1 failed: %v", err)
	}

	finished := t1.Add(10 * time.Second)
	run1.Status = domain.SyncStatusCompleted
	run1.FinishedAt = &finished
	run1.ScannedCount = 50
	run1.AddedCount = 45
	run1.StackedCount = 5
	if err := repo.UpdateSyncRun(ctx, run1); err != nil {
		t.Fatalf("UpdateSyncRun run-1 failed: %v", err)
	}

	// Create two stale running syncs (same second tie-breaker tested via rowid).
	for _, id := range []string{"run-2", "run-3"} {
		if err := repo.CreateSyncRun(ctx, domain.SyncReport{
			ID:        id,
			StartedAt: t1.Add(time.Minute),
			Status:    domain.SyncStatusRunning,
		}); err != nil {
			t.Fatalf("CreateSyncRun %s failed: %v", id, err)
		}
	}

	latest, err := repo.GetLatestSyncRun(ctx)
	if err != nil || latest.ID != "run-3" || latest.Status != domain.SyncStatusRunning {
		t.Fatalf("GetLatestSyncRun = (%+v, %v), want run-3 running", latest, err)
	}

	recovered, err := repo.RecoverStaleRuns(ctx, "", t1.Add(2*time.Minute))
	if err != nil || recovered != 2 {
		t.Fatalf("RecoverStaleRuns = (%d, %v), want 2", recovered, err)
	}

	runs, err := repo.ListSyncRuns(ctx, 0)
	if err != nil || len(runs) != 3 {
		t.Fatalf("ListSyncRuns = (%d, %v), want 3", len(runs), err)
	}
	if runs[0].Status != domain.SyncStatusFailed || runs[0].ErrorMessage != "interrupted by server restart" {
		t.Fatalf("expected recovered failed run, got %+v", runs[0])
	}
	if runs[2].Status != domain.SyncStatusCompleted {
		t.Fatalf("expected completed run-1 untouched, got %+v", runs[2])
	}
}

func TestSyncRepository_Integration_Settings(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	ctx := context.Background()
	db := openTestDB(t, filepath.Join(t.TempDir(), "photos.db"))
	repo := sqlite.NewSyncRepository(db.SQL())
	now := time.Date(2026, 7, 2, 10, 0, 0, 0, time.UTC)

	if _, err := repo.GetSetting(ctx, "session_secret"); !errors.Is(err, domain.ErrSettingNotFound) {
		t.Fatalf("GetSetting missing err = %v, want ErrSettingNotFound", err)
	}
	if err := repo.SetSetting(ctx, " ", "v", now); err == nil {
		t.Fatal("expected error for empty setting key")
	}
	if err := repo.SetSetting(ctx, "session_secret", "first-secret", now); err != nil {
		t.Fatalf("SetSetting insert failed: %v", err)
	}
	if err := repo.SetSetting(ctx, "session_secret", "rotated-secret", now.Add(time.Minute)); err != nil {
		t.Fatalf("SetSetting upsert failed: %v", err)
	}

	got, err := repo.GetSetting(ctx, "session_secret")
	if err != nil || got != "rotated-secret" {
		t.Fatalf("GetSetting = (%q, %v), want (rotated-secret, nil)", got, err)
	}

	// Default StartedAt/Status, custom RecoverStaleRuns reason, and closed-DB errors.
	if err := repo.CreateSyncRun(ctx, domain.SyncReport{ID: "run-defaults"}); err != nil {
		t.Fatalf("CreateSyncRun defaults failed: %v", err)
	}
	if err := repo.SetSetting(ctx, "zero_time", "ok", time.Time{}); err != nil {
		t.Fatalf("SetSetting zero time failed: %v", err)
	}
	recovered, err := repo.RecoverStaleRuns(ctx, "manual abort", time.Time{})
	if err != nil || recovered != 1 {
		t.Fatalf("RecoverStaleRuns = (%d, %v), want 1", recovered, err)
	}
	runs, err := repo.ListSyncRuns(ctx, 1)
	if err != nil || len(runs) != 1 || runs[0].ErrorMessage != "manual abort" {
		t.Fatalf("ListSyncRuns(1) = (%+v, %v)", runs, err)
	}

	if err := db.Close(); err != nil {
		t.Fatalf("db.Close failed: %v", err)
	}
	if err := repo.CreateSyncRun(ctx, domain.SyncReport{ID: "x"}); err == nil {
		t.Fatal("expected CreateSyncRun error on closed DB")
	}
	if err := repo.UpdateSyncRun(ctx, domain.SyncReport{ID: "x"}); err == nil {
		t.Fatal("expected UpdateSyncRun error on closed DB")
	}
	if _, err := repo.GetLatestSyncRun(ctx); err == nil {
		t.Fatal("expected GetLatestSyncRun error on closed DB")
	}
	if _, err := repo.ListSyncRuns(ctx, 5); err == nil {
		t.Fatal("expected ListSyncRuns error on closed DB")
	}
	if _, err := repo.RecoverStaleRuns(ctx, "x", now); err == nil {
		t.Fatal("expected RecoverStaleRuns error on closed DB")
	}
	if _, err := repo.GetSetting(ctx, "session_secret"); err == nil {
		t.Fatal("expected GetSetting error on closed DB")
	}
	if err := repo.SetSetting(ctx, "k", "v", now); err == nil {
		t.Fatal("expected SetSetting error on closed DB")
	}
}

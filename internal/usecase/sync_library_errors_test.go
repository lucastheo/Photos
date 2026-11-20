package usecase_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/lucastheo/photos/internal/domain"
	"github.com/lucastheo/photos/internal/usecase"
)

func TestSyncLibraryUseCase_ErrorPropagation(t *testing.T) {
	now := time.Date(2026, 7, 16, 12, 0, 0, 0, time.UTC)
	later := now.Add(time.Minute)
	sentinel := errors.New("boom")

	seedStandard := func(store *fakePhotoStore) {
		store.seedPhoto(domain.Photo{
			ID: "p1", StackKey: "Trip/photo/a", BaseName: "A", FolderPath: "Trip",
			MediaType: domain.MediaTypePhoto, Status: domain.FileStatusActive,
			Files: []domain.PhotoFile{
				{ID: "f1", PhotoID: "p1", ContentHash: "h1", RelativePath: "Trip/A.jpg", FileName: "A.jpg", FolderPath: "Trip", Format: domain.FormatJPG, FileSize: 100, ModTime: now, Status: domain.FileStatusActive},
			},
		})
	}

	tests := []struct {
		name string
		run  func() error
	}{
		{
			name: "Sync nil dependencies",
			run: func() error {
				_, err := usecase.NewSyncLibraryUseCase(nil, nil, nil).Sync(context.Background())
				return err
			},
		},
		{
			name: "PurgeMissing nil store",
			run: func() error {
				_, err := usecase.NewSyncLibraryUseCase(nil, nil, nil).PurgeMissing(context.Background())
				return err
			},
		},
		{
			name: "CreateSyncRun error",
			run: func() error {
				runs := newFakeSyncRunStore()
				runs.createErr = sentinel
				uc := usecase.NewSyncLibraryUseCase(newFakePhotoStore(), newFakeMediaScanner(nil, nil), nil).WithSyncRunStore(runs)
				_, err := uc.Sync(context.Background())
				return err
			},
		},
		{
			name: "UpdateSyncRun error on completion",
			run: func() error {
				runs := newFakeSyncRunStore()
				runs.updateErr = sentinel
				uc := usecase.NewSyncLibraryUseCase(newFakePhotoStore(), newFakeMediaScanner(nil, nil), nil).WithSyncRunStore(runs)
				_, err := uc.Sync(context.Background())
				return err
			},
		},
		{
			name: "UpdateSyncRun error on failure path",
			run: func() error {
				runs := newFakeSyncRunStore()
				runs.updateErr = sentinel
				scanner := newFakeMediaScanner(nil, nil)
				scanner.walkErr = errors.New("disk err")
				uc := usecase.NewSyncLibraryUseCase(newFakePhotoStore(), scanner, nil).WithSyncRunStore(runs)
				_, err := uc.Sync(context.Background())
				return err
			},
		},
		{
			name: "ListAllFiles error",
			run: func() error {
				store := newFakePhotoStore()
				store.listAllErr = sentinel
				uc := usecase.NewSyncLibraryUseCase(store, newFakeMediaScanner(nil, nil), nil)
				_, err := uc.Sync(context.Background())
				return err
			},
		},
		{
			name: "HashFiles error",
			run: func() error {
				scanner := newFakeMediaScanner([]domain.ScannedFile{{RelativePath: "a.jpg", Format: domain.FormatJPG, FileSize: 1, ModTime: now}}, nil)
				scanner.hashErr = sentinel
				uc := usecase.NewSyncLibraryUseCase(newFakePhotoStore(), scanner, nil)
				_, err := uc.Sync(context.Background())
				return err
			},
		},
		{
			name: "reactivate UpdateFileInPlace error",
			run: func() error {
				store := newFakePhotoStore()
				store.seedPhoto(domain.Photo{
					ID: "p1", Status: domain.FileStatusMissing,
					Files: []domain.PhotoFile{{ID: "f1", PhotoID: "p1", RelativePath: "A.jpg", Format: domain.FormatJPG, FileSize: 10, ModTime: now, Status: domain.FileStatusMissing}},
				})
				store.updateFileErr = sentinel
				scanner := newFakeMediaScanner([]domain.ScannedFile{{RelativePath: "A.jpg", Format: domain.FormatJPG, FileSize: 10, ModTime: now}}, nil)
				_, err := usecase.NewSyncLibraryUseCase(store, scanner, nil).Sync(context.Background())
				return err
			},
		},
		{
			name: "in-place edit UpdateFileInPlace error",
			run: func() error {
				store := newFakePhotoStore()
				seedStandard(store)
				store.updateFileErr = sentinel
				scanner := newFakeMediaScanner([]domain.ScannedFile{{RelativePath: "Trip/A.jpg", Format: domain.FormatJPG, FileSize: 200, ModTime: later}}, map[string]string{"Trip/A.jpg": "h2"})
				_, err := usecase.NewSyncLibraryUseCase(store, scanner, nil).Sync(context.Background())
				return err
			},
		},
		{
			name: "in-place edit GetByID error",
			run: func() error {
				store := newFakePhotoStore()
				seedStandard(store)
				store.getByIDErr = sentinel
				scanner := newFakeMediaScanner([]domain.ScannedFile{{RelativePath: "Trip/A.jpg", Format: domain.FormatJPG, FileSize: 200, ModTime: later}}, map[string]string{"Trip/A.jpg": "h2"})
				_, err := usecase.NewSyncLibraryUseCase(store, scanner, nil).Sync(context.Background())
				return err
			},
		},
		{
			name: "in-place edit Extract error",
			run: func() error {
				store := newFakePhotoStore()
				seedStandard(store)
				scanner := newFakeMediaScanner([]domain.ScannedFile{{RelativePath: "Trip/A.jpg", Format: domain.FormatJPG, FileSize: 200, ModTime: later}}, map[string]string{"Trip/A.jpg": "h2"})
				ext := &fakeMetadataExtractor{errBy: map[string]error{"Trip/A.jpg": sentinel}}
				_, err := usecase.NewSyncLibraryUseCase(store, scanner, nil).WithMetadataExtractor(ext).Sync(context.Background())
				return err
			},
		},
		{
			name: "in-place edit EnsureCache error",
			run: func() error {
				store := newFakePhotoStore()
				seedStandard(store)
				scanner := newFakeMediaScanner([]domain.ScannedFile{{RelativePath: "Trip/A.jpg", Format: domain.FormatJPG, FileSize: 200, ModTime: later}}, map[string]string{"Trip/A.jpg": "h2"})
				prev := newFakePreviewExtractor()
				prev.errBy["Trip/A.jpg"] = sentinel
				_, err := usecase.NewSyncLibraryUseCase(store, scanner, prev).Sync(context.Background())
				return err
			},
		},
		{
			name: "in-place edit UpdatePhotoMetadata error",
			run: func() error {
				store := newFakePhotoStore()
				seedStandard(store)
				store.updateMetaErr = sentinel
				scanner := newFakeMediaScanner([]domain.ScannedFile{{RelativePath: "Trip/A.jpg", Format: domain.FormatJPG, FileSize: 200, ModTime: later}}, map[string]string{"Trip/A.jpg": "h2"})
				ext := &fakeMetadataExtractor{byPath: map[string]domain.MediaMetadata{"Trip/A.jpg": {Width: 100}}}
				_, err := usecase.NewSyncLibraryUseCase(store, scanner, nil).WithMetadataExtractor(ext).Sync(context.Background())
				return err
			},
		},
		{
			name: "move GetByID error",
			run: func() error {
				store := newFakePhotoStore()
				seedStandard(store)
				store.getByIDErr = sentinel
				scanner := newFakeMediaScanner([]domain.ScannedFile{{RelativePath: "Archive/A.jpg", Format: domain.FormatJPG, FileSize: 100, ModTime: now}}, map[string]string{"Archive/A.jpg": "h1"})
				_, err := usecase.NewSyncLibraryUseCase(store, scanner, nil).Sync(context.Background())
				return err
			},
		},
		{
			name: "split SplitMovedFile error",
			run: func() error {
				store := newFakePhotoStore()
				store.seedPhoto(domain.Photo{
					ID: "p1", MediaType: domain.MediaTypePhoto, Status: domain.FileStatusActive,
					Files: []domain.PhotoFile{
						{ID: "f1", PhotoID: "p1", ContentHash: "h1", RelativePath: "Trip/A.CR3", FileName: "A.CR3", FolderPath: "Trip", Format: domain.FormatCR3, FileSize: 100, ModTime: now, Status: domain.FileStatusActive},
						{ID: "f2", PhotoID: "p1", ContentHash: "h2", RelativePath: "Trip/A.jpg", FileName: "A.jpg", FolderPath: "Trip", Format: domain.FormatJPG, FileSize: 50, ModTime: now, Status: domain.FileStatusActive},
					},
				})
				store.splitMovedErr = sentinel
				scanner := newFakeMediaScanner([]domain.ScannedFile{
					{RelativePath: "Trip/A.CR3", Format: domain.FormatCR3, FileSize: 100, ModTime: now},
					{RelativePath: "Fav/A.jpg", Format: domain.FormatJPG, FileSize: 50, ModTime: now},
				}, map[string]string{"Fav/A.jpg": "h2"})
				_, err := usecase.NewSyncLibraryUseCase(store, scanner, nil).Sync(context.Background())
				return err
			},
		},
		{
			name: "split UpdateFileInPlace stats error",
			run: func() error {
				store := newFakePhotoStore()
				store.seedPhoto(domain.Photo{
					ID: "p1", MediaType: domain.MediaTypePhoto, Status: domain.FileStatusActive,
					Files: []domain.PhotoFile{
						{ID: "f1", PhotoID: "p1", ContentHash: "h1", RelativePath: "Trip/A.CR3", FileName: "A.CR3", FolderPath: "Trip", Format: domain.FormatCR3, FileSize: 100, ModTime: now, Status: domain.FileStatusActive},
						{ID: "f2", PhotoID: "p1", ContentHash: "h2", RelativePath: "Trip/A.jpg", FileName: "A.jpg", FolderPath: "Trip", Format: domain.FormatJPG, FileSize: 50, ModTime: now, Status: domain.FileStatusActive},
					},
				})
				store.updateFileErr = sentinel
				scanner := newFakeMediaScanner([]domain.ScannedFile{
					{RelativePath: "Trip/A.CR3", Format: domain.FormatCR3, FileSize: 100, ModTime: now},
					{RelativePath: "Fav/A.jpg", Format: domain.FormatJPG, FileSize: 50, ModTime: later},
				}, map[string]string{"Fav/A.jpg": "h2"})
				_, err := usecase.NewSyncLibraryUseCase(store, scanner, nil).Sync(context.Background())
				return err
			},
		},
		{
			name: "MoveFilesBatch error",
			run: func() error {
				store := newFakePhotoStore()
				seedStandard(store)
				store.moveBatchErr = sentinel
				scanner := newFakeMediaScanner([]domain.ScannedFile{{RelativePath: "Archive/A.jpg", Format: domain.FormatJPG, FileSize: 100, ModTime: now}}, map[string]string{"Archive/A.jpg": "h1"})
				_, err := usecase.NewSyncLibraryUseCase(store, scanner, nil).Sync(context.Background())
				return err
			},
		},
		{
			name: "moved UpdateFileInPlace stats error",
			run: func() error {
				store := newFakePhotoStore()
				seedStandard(store)
				store.updateFileErr = sentinel
				scanner := newFakeMediaScanner([]domain.ScannedFile{{RelativePath: "Archive/A.jpg", Format: domain.FormatJPG, FileSize: 100, ModTime: later}}, map[string]string{"Archive/A.jpg": "h1"})
				_, err := usecase.NewSyncLibraryUseCase(store, scanner, nil).Sync(context.Background())
				return err
			},
		},
		{
			name: "FindFilesByContentHash error",
			run: func() error {
				store := newFakePhotoStore()
				store.findByHashErr = sentinel
				scanner := newFakeMediaScanner([]domain.ScannedFile{{RelativePath: "new.jpg", Format: domain.FormatJPG, FileSize: 10, ModTime: now}}, map[string]string{"new.jpg": "h"})
				_, err := usecase.NewSyncLibraryUseCase(store, scanner, nil).Sync(context.Background())
				return err
			},
		},
		{
			name: "GetByStackKey error",
			run: func() error {
				store := newFakePhotoStore()
				store.getByStackKeyErr = sentinel
				scanner := newFakeMediaScanner([]domain.ScannedFile{{RelativePath: "new.jpg", Format: domain.FormatJPG, FileSize: 10, ModTime: now}}, map[string]string{"new.jpg": "h"})
				_, err := usecase.NewSyncLibraryUseCase(store, scanner, nil).Sync(context.Background())
				return err
			},
		},
		{
			name: "duplicate AttachFile error",
			run: func() error {
				store := newFakePhotoStore()
				seedStandard(store)
				store.attachFileErr = sentinel
				scanner := newFakeMediaScanner([]domain.ScannedFile{
					{RelativePath: "Trip/A.jpg", Format: domain.FormatJPG, FileSize: 100, ModTime: now},
					{RelativePath: "Dup/A.jpg", Format: domain.FormatJPG, FileSize: 100, ModTime: now},
				}, map[string]string{"Dup/A.jpg": "h1"})
				_, err := usecase.NewSyncLibraryUseCase(store, scanner, nil).Sync(context.Background())
				return err
			},
		},
		{
			name: "duplicate EnsureCache error",
			run: func() error {
				store := newFakePhotoStore()
				seedStandard(store)
				scanner := newFakeMediaScanner([]domain.ScannedFile{
					{RelativePath: "Trip/A.jpg", Format: domain.FormatJPG, FileSize: 100, ModTime: now},
					{RelativePath: "Dup/A.jpg", Format: domain.FormatJPG, FileSize: 100, ModTime: now},
				}, map[string]string{"Dup/A.jpg": "h1"})
				prev := newFakePreviewExtractor()
				prev.errBy["Dup/A.jpg"] = sentinel
				_, err := usecase.NewSyncLibraryUseCase(store, scanner, prev).Sync(context.Background())
				return err
			},
		},
		{
			name: "stacked AttachFile error",
			run: func() error {
				store := newFakePhotoStore()
				seedStandard(store)
				store.attachFileErr = sentinel
				scanner := newFakeMediaScanner([]domain.ScannedFile{
					{RelativePath: "Trip/A.jpg", Format: domain.FormatJPG, FileSize: 100, ModTime: now},
					{RelativePath: "Trip/A.CR3", Format: domain.FormatCR3, FileSize: 500, ModTime: now},
				}, map[string]string{"Trip/A.CR3": "h-cr3"})
				_, err := usecase.NewSyncLibraryUseCase(store, scanner, nil).Sync(context.Background())
				return err
			},
		},
		{
			name: "stacked Extract error",
			run: func() error {
				store := newFakePhotoStore()
				seedStandard(store)
				scanner := newFakeMediaScanner([]domain.ScannedFile{
					{RelativePath: "Trip/A.jpg", Format: domain.FormatJPG, FileSize: 100, ModTime: now},
					{RelativePath: "Trip/A.CR3", Format: domain.FormatCR3, FileSize: 500, ModTime: now},
				}, map[string]string{"Trip/A.CR3": "h-cr3"})
				ext := &fakeMetadataExtractor{errBy: map[string]error{"Trip/A.CR3": sentinel}}
				_, err := usecase.NewSyncLibraryUseCase(store, scanner, nil).WithMetadataExtractor(ext).Sync(context.Background())
				return err
			},
		},
		{
			name: "stacked EnsureCache error",
			run: func() error {
				store := newFakePhotoStore()
				seedStandard(store)
				scanner := newFakeMediaScanner([]domain.ScannedFile{
					{RelativePath: "Trip/A.jpg", Format: domain.FormatJPG, FileSize: 100, ModTime: now},
					{RelativePath: "Trip/A.CR3", Format: domain.FormatCR3, FileSize: 500, ModTime: now},
				}, map[string]string{"Trip/A.CR3": "h-cr3"})
				prev := newFakePreviewExtractor()
				prev.errBy["Trip/A.CR3"] = sentinel
				_, err := usecase.NewSyncLibraryUseCase(store, scanner, prev).Sync(context.Background())
				return err
			},
		},
		{
			name: "stacked UpdatePhotoMetadata error",
			run: func() error {
				store := newFakePhotoStore()
				seedStandard(store)
				store.updateMetaErr = sentinel
				scanner := newFakeMediaScanner([]domain.ScannedFile{
					{RelativePath: "Trip/A.jpg", Format: domain.FormatJPG, FileSize: 100, ModTime: now},
					{RelativePath: "Trip/A.CR3", Format: domain.FormatCR3, FileSize: 500, ModTime: now},
				}, map[string]string{"Trip/A.CR3": "h-cr3"})
				ext := &fakeMetadataExtractor{byPath: map[string]domain.MediaMetadata{"Trip/A.CR3": {Width: 6000}}}
				_, err := usecase.NewSyncLibraryUseCase(store, scanner, nil).WithMetadataExtractor(ext).Sync(context.Background())
				return err
			},
		},
		{
			name: "createNewPhoto Extract error",
			run: func() error {
				scanner := newFakeMediaScanner([]domain.ScannedFile{{RelativePath: "a.jpg", Format: domain.FormatJPG, FileSize: 10, ModTime: now}}, map[string]string{"a.jpg": "h"})
				ext := &fakeMetadataExtractor{errBy: map[string]error{"a.jpg": sentinel}}
				_, err := usecase.NewSyncLibraryUseCase(newFakePhotoStore(), scanner, nil).WithMetadataExtractor(ext).Sync(context.Background())
				return err
			},
		},
		{
			name: "createNewPhoto EnsureCache error",
			run: func() error {
				scanner := newFakeMediaScanner([]domain.ScannedFile{{RelativePath: "a.jpg", Format: domain.FormatJPG, FileSize: 10, ModTime: now}}, map[string]string{"a.jpg": "h"})
				prev := newFakePreviewExtractor()
				prev.errBy["a.jpg"] = sentinel
				_, err := usecase.NewSyncLibraryUseCase(newFakePhotoStore(), scanner, prev).Sync(context.Background())
				return err
			},
		},
		{
			name: "createNewPhoto CreatePhoto error",
			run: func() error {
				store := newFakePhotoStore()
				store.createPhotoErr = sentinel
				scanner := newFakeMediaScanner([]domain.ScannedFile{{RelativePath: "a.jpg", Format: domain.FormatJPG, FileSize: 10, ModTime: now}}, map[string]string{"a.jpg": "h"})
				_, err := usecase.NewSyncLibraryUseCase(store, scanner, nil).Sync(context.Background())
				return err
			},
		},
		{
			name: "MarkFilesMissing error",
			run: func() error {
				store := newFakePhotoStore()
				seedStandard(store)
				store.markMissingErr = sentinel
				scanner := newFakeMediaScanner([]domain.ScannedFile{{RelativePath: "new.jpg", Format: domain.FormatJPG, FileSize: 10, ModTime: now}}, map[string]string{"new.jpg": "hn"})
				_, err := usecase.NewSyncLibraryUseCase(store, scanner, nil).Sync(context.Background())
				return err
			},
		},
		{
			name: "Checkpoint error",
			run: func() error {
				maintainer := &fakeMaintainer{checkpointErr: sentinel}
				uc := usecase.NewSyncLibraryUseCase(newFakePhotoStore(), newFakeMediaScanner(nil, nil), nil).WithDatabaseMaintainer(maintainer)
				_, err := uc.Sync(context.Background())
				return err
			},
		},
		{
			name: "PurgeMissing Backup error",
			run: func() error {
				maintainer := &fakeMaintainer{backupErr: sentinel}
				uc := usecase.NewSyncLibraryUseCase(newFakePhotoStore(), newFakeMediaScanner(nil, nil), nil).WithDatabaseMaintainer(maintainer)
				_, err := uc.PurgeMissing(context.Background())
				return err
			},
		},
		{
			name: "PurgeMissing store error",
			run: func() error {
				store := newFakePhotoStore()
				store.purgeMissingErr = sentinel
				uc := usecase.NewSyncLibraryUseCase(store, newFakeMediaScanner(nil, nil), nil)
				_, err := uc.PurgeMissing(context.Background())
				return err
			},
		},
		{
			name: "Status store error",
			run: func() error {
				runs := newFakeSyncRunStore()
				runs.getLatestErr = sentinel
				uc := usecase.NewSyncLibraryUseCase(newFakePhotoStore(), newFakeMediaScanner(nil, nil), nil).WithSyncRunStore(runs)
				_, err := uc.Status(context.Background())
				return err
			},
		},
		{
			name: "RecoverStaleRuns store error",
			run: func() error {
				runs := newFakeSyncRunStore()
				runs.recoverErr = sentinel
				uc := usecase.NewSyncLibraryUseCase(newFakePhotoStore(), newFakeMediaScanner(nil, nil), nil).WithSyncRunStore(runs)
				_, err := uc.RecoverStaleRuns(context.Background())
				return err
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if err := tc.run(); err == nil {
				t.Fatal("expected non-nil error")
			}
		})
	}
}

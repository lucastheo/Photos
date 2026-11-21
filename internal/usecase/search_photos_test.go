package usecase_test

import (
	"context"
	"errors"
	"testing"

	"github.com/lucastheo/photos/internal/domain"
	"github.com/lucastheo/photos/internal/usecase"
)

type fakePhotoSearchStore struct {
	searchFn func(ctx context.Context, filter domain.PhotoFilter) (domain.PaginatedPhotos, error)
	byID     map[string]domain.Photo
	stats    domain.LibraryStats
	dups     []domain.DuplicateGroup
	missing  []domain.Photo
	err      error
}

func (f *fakePhotoSearchStore) Search(ctx context.Context, filter domain.PhotoFilter) (domain.PaginatedPhotos, error) {
	if f.err != nil {
		return domain.PaginatedPhotos{}, f.err
	}
	if f.searchFn != nil {
		return f.searchFn(ctx, filter)
	}
	return domain.PaginatedPhotos{Page: filter.Page, PageSize: filter.PageSize}, nil
}

func (f *fakePhotoSearchStore) GetByID(_ context.Context, photoID string) (domain.Photo, error) {
	if f.err != nil {
		return domain.Photo{}, f.err
	}
	p, ok := f.byID[photoID]
	if !ok {
		return domain.Photo{}, domain.ErrPhotoNotFound
	}
	return p, nil
}

func (f *fakePhotoSearchStore) GetLibraryStats(_ context.Context) (domain.LibraryStats, error) {
	if f.err != nil {
		return domain.LibraryStats{}, f.err
	}
	return f.stats, nil
}

func (f *fakePhotoSearchStore) ListDuplicates(_ context.Context) ([]domain.DuplicateGroup, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.dups, nil
}

func (f *fakePhotoSearchStore) ListMissing(_ context.Context) ([]domain.Photo, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.missing, nil
}

func TestSearchPhotosUseCase(t *testing.T) {
	t.Parallel()

	t.Run("search normalizes filters and forwards to store", func(t *testing.T) {
		t.Parallel()
		var gotFilter domain.PhotoFilter
		store := &fakePhotoSearchStore{
			searchFn: func(_ context.Context, filter domain.PhotoFilter) (domain.PaginatedPhotos, error) {
				gotFilter = filter
				return domain.PaginatedPhotos{
					Items:    []domain.Photo{{ID: "p1"}},
					Total:    1,
					Page:     filter.Page,
					PageSize: filter.PageSize,
				}, nil
			},
		}
		uc := usecase.NewSearchPhotosUseCase(store)
		res, err := uc.Search(context.Background(), domain.PhotoFilter{
			EmojiTags: []string{"❤️"},
			Folder:    "/2026/Trip/",
			PageSize:  999,
		})
		if err != nil {
			t.Fatalf("Search failed: %v", err)
		}
		if res.Total != 1 || len(res.Items) != 1 || gotFilter.PageSize != domain.MaxPageSize ||
			gotFilter.Folder != "2026/Trip" || gotFilter.EmojiTags[0] != "❤" {
			t.Fatalf("unexpected filter/result: gotFilter=%+v res=%+v", gotFilter, res)
		}
	})

	t.Run("invalid filter returns ErrInvalidFilter", func(t *testing.T) {
		t.Parallel()
		uc := usecase.NewSearchPhotosUseCase(&fakePhotoSearchStore{})
		minV, maxV := 5, 1
		_, err := uc.Search(context.Background(), domain.PhotoFilter{MinViews: &minV, MaxViews: &maxV})
		if !errors.Is(err, domain.ErrInvalidFilter) {
			t.Fatalf("expected ErrInvalidFilter, got %v", err)
		}
	})

	t.Run("get by id and library overview queries", func(t *testing.T) {
		t.Parallel()
		store := &fakePhotoSearchStore{
			byID:    map[string]domain.Photo{"p1": {ID: "p1", ViewCount: 2}},
			stats:   domain.LibraryStats{TotalPhotos: 4, UnseenPhotos: 1, SeenPhotos: 3},
			dups:    []domain.DuplicateGroup{{ContentHash: "abc", PhotoID: "p1", FileCount: 2}},
			missing: []domain.Photo{{ID: "m1", Status: domain.FileStatusMissing}},
		}
		uc := usecase.NewSearchPhotosUseCase(store)

		p, err := uc.GetByID(context.Background(), " p1 ")
		if err != nil || p.ViewCount != 2 {
			t.Fatalf("GetByID = (%+v, %v)", p, err)
		}
		if _, err := uc.GetByID(context.Background(), "  "); !errors.Is(err, domain.ErrInvalidInput) {
			t.Fatalf("expected ErrInvalidInput for empty photo id, got %v", err)
		}
		if _, err := uc.GetByID(context.Background(), "unknown"); !errors.Is(err, domain.ErrPhotoNotFound) {
			t.Fatalf("expected ErrPhotoNotFound, got %v", err)
		}

		stats, err := uc.GetLibraryStats(context.Background())
		if err != nil || stats.TotalPhotos != 4 {
			t.Fatalf("GetLibraryStats = (%+v, %v)", stats, err)
		}
		dups, err := uc.ListDuplicates(context.Background())
		if err != nil || len(dups) != 1 {
			t.Fatalf("ListDuplicates = (%+v, %v)", dups, err)
		}
		missing, err := uc.ListMissing(context.Background())
		if err != nil || len(missing) != 1 {
			t.Fatalf("ListMissing = (%+v, %v)", missing, err)
		}
	})

	t.Run("nil store and store error propagation", func(t *testing.T) {
		t.Parallel()
		nilUC := usecase.NewSearchPhotosUseCase(nil)
		if _, err := nilUC.Search(context.Background(), domain.PhotoFilter{}); err == nil {
			t.Fatal("expected error with nil store")
		}
		if _, err := nilUC.GetByID(context.Background(), "p1"); err == nil {
			t.Fatal("expected error with nil store")
		}
		if _, err := nilUC.GetLibraryStats(context.Background()); err == nil {
			t.Fatal("expected error with nil store")
		}
		if _, err := nilUC.ListDuplicates(context.Background()); err == nil {
			t.Fatal("expected error with nil store")
		}
		if _, err := nilUC.ListMissing(context.Background()); err == nil {
			t.Fatal("expected error with nil store")
		}

		boom := errors.New("db down")
		errUC := usecase.NewSearchPhotosUseCase(&fakePhotoSearchStore{err: boom})
		if _, err := errUC.Search(context.Background(), domain.PhotoFilter{}); !errors.Is(err, boom) {
			t.Fatalf("expected boom, got %v", err)
		}
		if _, err := errUC.GetLibraryStats(context.Background()); !errors.Is(err, boom) {
			t.Fatalf("expected boom, got %v", err)
		}
		if _, err := errUC.ListDuplicates(context.Background()); !errors.Is(err, boom) {
			t.Fatalf("expected boom, got %v", err)
		}
		if _, err := errUC.ListMissing(context.Background()); !errors.Is(err, boom) {
			t.Fatalf("expected boom, got %v", err)
		}
	})
}

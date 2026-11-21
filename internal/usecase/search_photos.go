package usecase

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/lucastheo/photos/internal/domain"
)

// PhotoSearchStore defines the read-only persistence operations required by
// SearchPhotosUseCase to query photos and library overview reports.
type PhotoSearchStore interface {
	Search(ctx context.Context, filter domain.PhotoFilter) (domain.PaginatedPhotos, error)
	GetByID(ctx context.Context, photoID string) (domain.Photo, error)
	GetLibraryStats(ctx context.Context) (domain.LibraryStats, error)
	ListDuplicates(ctx context.Context) ([]domain.DuplicateGroup, error)
	ListMissing(ctx context.Context) ([]domain.Photo, error)
}

// SearchPhotosUseCase orchestrates photo search, single-photo retrieval, and
// library overview queries.
type SearchPhotosUseCase struct {
	store PhotoSearchStore
}

// NewSearchPhotosUseCase constructs a concrete SearchPhotosUseCase backed by store.
func NewSearchPhotosUseCase(store PhotoSearchStore) *SearchPhotosUseCase {
	return &SearchPhotosUseCase{store: store}
}

// Search validates and normalizes filter criteria and returns a paginated page
// of matching Photo records.
func (u *SearchPhotosUseCase) Search(ctx context.Context, filter domain.PhotoFilter) (domain.PaginatedPhotos, error) {
	if u.store == nil {
		return domain.PaginatedPhotos{}, errors.New("search photos use case requires store")
	}
	norm, err := filter.NormalizeAndValidate()
	if err != nil {
		return domain.PaginatedPhotos{}, err
	}
	res, err := u.store.Search(ctx, norm)
	if err != nil {
		return domain.PaginatedPhotos{}, fmt.Errorf("search photos: %w", err)
	}
	return res, nil
}

// GetByID retrieves a single logical Photo by ID.
func (u *SearchPhotosUseCase) GetByID(ctx context.Context, photoID string) (domain.Photo, error) {
	if u.store == nil {
		return domain.Photo{}, errors.New("search photos use case requires store")
	}
	id := strings.TrimSpace(photoID)
	if id == "" {
		return domain.Photo{}, fmt.Errorf("%w: photo id is required", domain.ErrInvalidInput)
	}
	photo, err := u.store.GetByID(ctx, id)
	if err != nil {
		return domain.Photo{}, fmt.Errorf("get photo %q: %w", id, err)
	}
	return photo, nil
}

// GetLibraryStats returns aggregate counts across the indexed media library.
func (u *SearchPhotosUseCase) GetLibraryStats(ctx context.Context) (domain.LibraryStats, error) {
	if u.store == nil {
		return domain.LibraryStats{}, errors.New("search photos use case requires store")
	}
	stats, err := u.store.GetLibraryStats(ctx)
	if err != nil {
		return domain.LibraryStats{}, fmt.Errorf("get library stats: %w", err)
	}
	return stats, nil
}

// ListDuplicates returns all SHA-256 content hashes with more than one active
// physical file path on disk.
func (u *SearchPhotosUseCase) ListDuplicates(ctx context.Context) ([]domain.DuplicateGroup, error) {
	if u.store == nil {
		return nil, errors.New("search photos use case requires store")
	}
	groups, err := u.store.ListDuplicates(ctx)
	if err != nil {
		return nil, fmt.Errorf("list duplicates: %w", err)
	}
	return groups, nil
}

// ListMissing returns all logical Photo records currently marked missing or
// owning missing physical files.
func (u *SearchPhotosUseCase) ListMissing(ctx context.Context) ([]domain.Photo, error) {
	if u.store == nil {
		return nil, errors.New("search photos use case requires store")
	}
	missing, err := u.store.ListMissing(ctx)
	if err != nil {
		return nil, fmt.Errorf("list missing photos: %w", err)
	}
	return missing, nil
}

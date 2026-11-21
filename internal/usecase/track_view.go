package usecase

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/lucastheo/photos/internal/domain"
)

const defaultViewHistoryLimit = 100

// ViewStore defines the persistence operations required by TrackViewUseCase to
// record view events, adjust view counts, and query view history.
type ViewStore interface {
	RecordView(ctx context.Context, event domain.ViewEvent) (domain.Photo, error)
	AdjustViewCount(ctx context.Context, photoID string, action domain.ViewAction, now time.Time) (domain.Photo, error)
	ListViewHistory(ctx context.Context, photoID string, limit int) ([]domain.ViewEvent, error)
}

// TrackViewUseCase enforces view-tracking business rules and delegates atomic
// persistence to ViewStore.
type TrackViewUseCase struct {
	store ViewStore
	now   func() time.Time
}

// NewTrackViewUseCase constructs a concrete TrackViewUseCase backed by store.
func NewTrackViewUseCase(store ViewStore) *TrackViewUseCase {
	return &TrackViewUseCase{
		store: store,
		now:   func() time.Time { return time.Now().UTC() },
	}
}

// WithClock overrides the clock function used for view event timestamps.
func (u *TrackViewUseCase) WithClock(nowFunc func() time.Time) *TrackViewUseCase {
	if nowFunc != nil {
		u.now = nowFunc
	}
	return u
}

// RecordView records a timestamped view event for photoID, using viewID for
// idempotency when provided and defaulting an empty source to ViewSourceDwell.
func (u *TrackViewUseCase) RecordView(
	ctx context.Context,
	photoID, viewID string,
	source domain.ViewSource,
) (domain.Photo, error) {
	if u.store == nil {
		return domain.Photo{}, errors.New("track view use case requires store")
	}
	cleanID := strings.TrimSpace(photoID)
	if cleanID == "" {
		return domain.Photo{}, fmt.Errorf("%w: photo id is required", domain.ErrInvalidInput)
	}
	if source == "" {
		source = domain.ViewSourceDwell
	}
	if !source.IsValid() {
		return domain.Photo{}, fmt.Errorf("%w: %q", domain.ErrInvalidViewSource, source)
	}

	event := domain.ViewEvent{
		ViewID:   strings.TrimSpace(viewID),
		PhotoID:  cleanID,
		ViewedAt: u.now().UTC(),
		Source:   source,
	}
	photo, err := u.store.RecordView(ctx, event)
	if err != nil {
		return domain.Photo{}, fmt.Errorf("record view for %q: %w", cleanID, err)
	}
	return photo, nil
}

// AdjustViewCount applies a manual increment, decrement, or reset operation to
// photoID while keeping view_count synchronized with photo_views.
func (u *TrackViewUseCase) AdjustViewCount(
	ctx context.Context,
	photoID string,
	action domain.ViewAction,
) (domain.Photo, error) {
	if u.store == nil {
		return domain.Photo{}, errors.New("track view use case requires store")
	}
	cleanID := strings.TrimSpace(photoID)
	if cleanID == "" {
		return domain.Photo{}, fmt.Errorf("%w: photo id is required", domain.ErrInvalidInput)
	}
	if !action.IsValid() {
		return domain.Photo{}, fmt.Errorf("%w: %q", domain.ErrInvalidViewAction, action)
	}

	photo, err := u.store.AdjustViewCount(ctx, cleanID, action, u.now().UTC())
	if err != nil {
		return domain.Photo{}, fmt.Errorf("adjust view count for %q: %w", cleanID, err)
	}
	return photo, nil
}

// ListViewHistory returns recent view events for photoID ordered newest first.
func (u *TrackViewUseCase) ListViewHistory(
	ctx context.Context,
	photoID string,
	limit int,
) ([]domain.ViewEvent, error) {
	if u.store == nil {
		return nil, errors.New("track view use case requires store")
	}
	cleanID := strings.TrimSpace(photoID)
	if cleanID == "" {
		return nil, fmt.Errorf("%w: photo id is required", domain.ErrInvalidInput)
	}
	if limit <= 0 {
		limit = defaultViewHistoryLimit
	}

	events, err := u.store.ListViewHistory(ctx, cleanID, limit)
	if err != nil {
		return nil, fmt.Errorf("list view history for %q: %w", cleanID, err)
	}
	return events, nil
}

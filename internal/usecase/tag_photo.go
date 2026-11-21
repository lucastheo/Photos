package usecase

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/lucastheo/photos/internal/domain"
)

// TagStore defines the persistence operations required by TagPhotoUseCase to
// manage canonical emoji tags on photos and list tag usage counts.
type TagStore interface {
	AddTag(ctx context.Context, photoID, rawEmoji string, now time.Time) error
	RemoveTag(ctx context.Context, photoID, rawEmoji string) error
	ListTags(ctx context.Context) ([]domain.EmojiTagCount, error)
	GetByID(ctx context.Context, photoID string) (domain.Photo, error)
}

// TagPhotoUseCase validates and normalizes emoji tags and persists tag updates.
type TagPhotoUseCase struct {
	store TagStore
	now   func() time.Time
}

// NewTagPhotoUseCase constructs a concrete TagPhotoUseCase backed by store.
func NewTagPhotoUseCase(store TagStore) *TagPhotoUseCase {
	return &TagPhotoUseCase{
		store: store,
		now:   func() time.Time { return time.Now().UTC() },
	}
}

// WithClock overrides the clock function used for tag creation timestamps.
func (u *TagPhotoUseCase) WithClock(nowFunc func() time.Time) *TagPhotoUseCase {
	if nowFunc != nil {
		u.now = nowFunc
	}
	return u
}

// AddTag normalizes rawEmoji, attaches it to photoID, and returns the updated Photo.
func (u *TagPhotoUseCase) AddTag(ctx context.Context, photoID, rawEmoji string) (domain.Photo, error) {
	if u.store == nil {
		return domain.Photo{}, errors.New("tag photo use case requires store")
	}
	cleanID := strings.TrimSpace(photoID)
	if cleanID == "" {
		return domain.Photo{}, fmt.Errorf("%w: photo id is required", domain.ErrInvalidInput)
	}
	emoji, err := domain.NormalizeEmojiTag(rawEmoji)
	if err != nil {
		return domain.Photo{}, err
	}
	if err := u.store.AddTag(ctx, cleanID, emoji, u.now().UTC()); err != nil {
		return domain.Photo{}, fmt.Errorf("add tag %q to %q: %w", emoji, cleanID, err)
	}
	photo, err := u.store.GetByID(ctx, cleanID)
	if err != nil {
		return domain.Photo{}, fmt.Errorf("load tagged photo %q: %w", cleanID, err)
	}
	return photo, nil
}

// RemoveTag normalizes rawEmoji, detaches it from photoID, and returns the updated Photo.
func (u *TagPhotoUseCase) RemoveTag(ctx context.Context, photoID, rawEmoji string) (domain.Photo, error) {
	if u.store == nil {
		return domain.Photo{}, errors.New("tag photo use case requires store")
	}
	cleanID := strings.TrimSpace(photoID)
	if cleanID == "" {
		return domain.Photo{}, fmt.Errorf("%w: photo id is required", domain.ErrInvalidInput)
	}
	emoji, err := domain.NormalizeEmojiTag(rawEmoji)
	if err != nil {
		return domain.Photo{}, err
	}
	if err := u.store.RemoveTag(ctx, cleanID, emoji); err != nil {
		return domain.Photo{}, fmt.Errorf("remove tag %q from %q: %w", emoji, cleanID, err)
	}
	photo, err := u.store.GetByID(ctx, cleanID)
	if err != nil {
		return domain.Photo{}, fmt.Errorf("load untagged photo %q: %w", cleanID, err)
	}
	return photo, nil
}

// ListTags returns all emoji tags currently attached to active photos with counts.
func (u *TagPhotoUseCase) ListTags(ctx context.Context) ([]domain.EmojiTagCount, error) {
	if u.store == nil {
		return nil, errors.New("tag photo use case requires store")
	}
	tags, err := u.store.ListTags(ctx)
	if err != nil {
		return nil, fmt.Errorf("list tags: %w", err)
	}
	return tags, nil
}

package usecase_test

import (
	"context"
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/lucastheo/photos/internal/domain"
	"github.com/lucastheo/photos/internal/usecase"
)

type fakeTagStore struct {
	photos map[string]domain.Photo
	err    error
}

func newFakeTagStore() *fakeTagStore {
	return &fakeTagStore{
		photos: map[string]domain.Photo{
			"p1": {ID: "p1", Status: domain.FileStatusActive},
		},
	}
}

func (f *fakeTagStore) AddTag(_ context.Context, photoID, rawEmoji string, _ time.Time) error {
	if f.err != nil {
		return f.err
	}
	p, ok := f.photos[photoID]
	if !ok {
		return domain.ErrPhotoNotFound
	}
	norm, err := domain.NormalizeEmojiTag(rawEmoji)
	if err != nil {
		return err
	}
	if !slices.Contains(p.EmojiTags, norm) {
		p.EmojiTags = append(p.EmojiTags, norm)
	}
	f.photos[photoID] = p
	return nil
}

func (f *fakeTagStore) RemoveTag(_ context.Context, photoID, rawEmoji string) error {
	if f.err != nil {
		return f.err
	}
	p, ok := f.photos[photoID]
	if !ok {
		return domain.ErrPhotoNotFound
	}
	norm, err := domain.NormalizeEmojiTag(rawEmoji)
	if err != nil {
		return err
	}
	p.EmojiTags = slices.DeleteFunc(p.EmojiTags, func(s string) bool { return s == norm })
	f.photos[photoID] = p
	return nil
}

func (f *fakeTagStore) ListTags(_ context.Context) ([]domain.EmojiTagCount, error) {
	if f.err != nil {
		return nil, f.err
	}
	counts := make(map[string]int64)
	for _, p := range f.photos {
		for _, tag := range p.EmojiTags {
			counts[tag]++
		}
	}
	var out []domain.EmojiTagCount
	for e, c := range counts {
		out = append(out, domain.EmojiTagCount{Emoji: e, Count: c})
	}
	return out, nil
}

func (f *fakeTagStore) GetByID(_ context.Context, photoID string) (domain.Photo, error) {
	if f.err != nil {
		return domain.Photo{}, f.err
	}
	p, ok := f.photos[photoID]
	if !ok {
		return domain.Photo{}, domain.ErrPhotoNotFound
	}
	return p, nil
}

func TestTagPhotoUseCase(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 7, 15, 19, 0, 0, 0, time.UTC)

	t.Run("add remove and list normalized emoji tags", func(t *testing.T) {
		t.Parallel()
		store := newFakeTagStore()
		uc := usecase.NewTagPhotoUseCase(store).WithClock(testClock(now))

		p, err := uc.AddTag(context.Background(), " p1 ", "❤️")
		if err != nil || len(p.EmojiTags) != 1 || p.EmojiTags[0] != "❤" {
			t.Fatalf("AddTag = (%+v, %v)", p, err)
		}
		tags, err := uc.ListTags(context.Background())
		if err != nil || len(tags) != 1 || tags[0].Emoji != "❤" || tags[0].Count != 1 {
			t.Fatalf("ListTags = (%+v, %v)", tags, err)
		}
		p, err = uc.RemoveTag(context.Background(), "p1", "❤")
		if err != nil || len(p.EmojiTags) != 0 {
			t.Fatalf("RemoveTag = (%+v, %v)", p, err)
		}
	})

	t.Run("validation and error propagation", func(t *testing.T) {
		t.Parallel()
		store := newFakeTagStore()
		uc := usecase.NewTagPhotoUseCase(store)

		cases := []struct {
			name string
			fn   func() error
			want error
		}{
			{name: "add empty id", fn: func() error { _, err := uc.AddTag(context.Background(), "", "❤️"); return err }, want: domain.ErrInvalidInput},
			{name: "add invalid emoji", fn: func() error { _, err := uc.AddTag(context.Background(), "p1", "hello"); return err }, want: domain.ErrInvalidEmojiTag},
			{name: "add missing photo", fn: func() error { _, err := uc.AddTag(context.Background(), "missing", "❤️"); return err }, want: domain.ErrPhotoNotFound},
			{name: "remove empty id", fn: func() error { _, err := uc.RemoveTag(context.Background(), " ", "❤️"); return err }, want: domain.ErrInvalidInput},
			{name: "remove invalid emoji", fn: func() error { _, err := uc.RemoveTag(context.Background(), "p1", "bad"); return err }, want: domain.ErrInvalidEmojiTag},
			{name: "remove missing photo", fn: func() error { _, err := uc.RemoveTag(context.Background(), "missing", "❤️"); return err }, want: domain.ErrPhotoNotFound},
		}
		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				t.Parallel()
				if err := tc.fn(); !errors.Is(err, tc.want) {
					t.Fatalf("got %v, want %v", err, tc.want)
				}
			})
		}

		nilUC := usecase.NewTagPhotoUseCase(nil)
		if _, err := nilUC.AddTag(context.Background(), "p1", "❤️"); err == nil {
			t.Fatal("expected nil store error")
		}
		if _, err := nilUC.RemoveTag(context.Background(), "p1", "❤️"); err == nil {
			t.Fatal("expected nil store error")
		}
		if _, err := nilUC.ListTags(context.Background()); err == nil {
			t.Fatal("expected nil store error")
		}

		boom := errors.New("storage error")
		errUC := usecase.NewTagPhotoUseCase(&fakeTagStore{err: boom})
		if _, err := errUC.ListTags(context.Background()); !errors.Is(err, boom) {
			t.Fatalf("expected boom, got %v", err)
		}
	})
}

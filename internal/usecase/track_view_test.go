package usecase_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/lucastheo/photos/internal/domain"
	"github.com/lucastheo/photos/internal/usecase"
)

type fakeViewStore struct {
	photos  map[string]domain.Photo
	events  map[string][]domain.ViewEvent
	viewIDs map[string]bool
	err     error
}

func newFakeViewStore() *fakeViewStore {
	return &fakeViewStore{
		photos: map[string]domain.Photo{
			"p1": {ID: "p1", ViewCount: 0, Status: domain.FileStatusActive},
		},
		events:  make(map[string][]domain.ViewEvent),
		viewIDs: make(map[string]bool),
	}
}

func (f *fakeViewStore) RecordView(_ context.Context, event domain.ViewEvent) (domain.Photo, error) {
	if f.err != nil {
		return domain.Photo{}, f.err
	}
	p, ok := f.photos[event.PhotoID]
	if !ok {
		return domain.Photo{}, domain.ErrPhotoNotFound
	}
	if event.ViewID != "" {
		if f.viewIDs[event.ViewID] {
			return p, nil
		}
		f.viewIDs[event.ViewID] = true
	}
	event.ID = int64(len(f.events[event.PhotoID]) + 1)
	f.events[event.PhotoID] = append([]domain.ViewEvent{event}, f.events[event.PhotoID]...)
	p.ViewCount = len(f.events[event.PhotoID])
	t := event.ViewedAt
	p.LastViewedAt = &t
	f.photos[event.PhotoID] = p
	return p, nil
}

func (f *fakeViewStore) AdjustViewCount(
	_ context.Context,
	photoID string,
	action domain.ViewAction,
	now time.Time,
) (domain.Photo, error) {
	if f.err != nil {
		return domain.Photo{}, f.err
	}
	p, ok := f.photos[photoID]
	if !ok {
		return domain.Photo{}, domain.ErrPhotoNotFound
	}
	switch action {
	case domain.ViewActionIncrement:
		ev := domain.ViewEvent{ID: int64(len(f.events[photoID]) + 1), PhotoID: photoID, ViewedAt: now, Source: domain.ViewSourceManual}
		f.events[photoID] = append([]domain.ViewEvent{ev}, f.events[photoID]...)
	case domain.ViewActionDecrement:
		if len(f.events[photoID]) > 0 {
			f.events[photoID] = f.events[photoID][1:]
		}
	case domain.ViewActionReset:
		f.events[photoID] = nil
	}
	p.ViewCount = len(f.events[photoID])
	if p.ViewCount == 0 {
		p.LastViewedAt = nil
	} else {
		t := f.events[photoID][0].ViewedAt
		p.LastViewedAt = &t
	}
	f.photos[photoID] = p
	return p, nil
}

func (f *fakeViewStore) ListViewHistory(_ context.Context, photoID string, limit int) ([]domain.ViewEvent, error) {
	if f.err != nil {
		return nil, f.err
	}
	if _, ok := f.photos[photoID]; !ok {
		return nil, domain.ErrPhotoNotFound
	}
	all := f.events[photoID]
	if limit < len(all) {
		return all[:limit], nil
	}
	return all, nil
}

func TestTrackViewUseCase(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 7, 15, 18, 0, 0, 0, time.UTC)

	t.Run("record view defaults source and enforces idempotency", func(t *testing.T) {
		t.Parallel()
		store := newFakeViewStore()
		uc := usecase.NewTrackViewUseCase(store).WithClock(testClock(now))

		p1, err := uc.RecordView(context.Background(), " p1 ", " view-1 ", "")
		if err != nil || p1.ViewCount != 1 {
			t.Fatalf("first RecordView = (%+v, %v)", p1, err)
		}
		p2, err := uc.RecordView(context.Background(), "p1", "view-1", domain.ViewSourceSlideshow)
		if err != nil || p2.ViewCount != 1 {
			t.Fatalf("duplicate ViewID should be idempotent, got (%+v, %v)", p2, err)
		}
		p3, err := uc.RecordView(context.Background(), "p1", "view-2", domain.ViewSourceSlideshow)
		if err != nil || p3.ViewCount != 2 {
			t.Fatalf("second distinct ViewID = (%+v, %v)", p3, err)
		}
	})

	t.Run("manual adjust synchronizes view history and clamps at zero", func(t *testing.T) {
		t.Parallel()
		store := newFakeViewStore()
		uc := usecase.NewTrackViewUseCase(store).WithClock(testClock(now))

		p, err := uc.AdjustViewCount(context.Background(), "p1", domain.ViewActionIncrement)
		if err != nil || p.ViewCount != 1 {
			t.Fatalf("increment = (%+v, %v)", p, err)
		}
		p, err = uc.AdjustViewCount(context.Background(), "p1", domain.ViewActionIncrement)
		if err != nil || p.ViewCount != 2 {
			t.Fatalf("second increment = (%+v, %v)", p, err)
		}
		p, err = uc.AdjustViewCount(context.Background(), "p1", domain.ViewActionDecrement)
		if err != nil || p.ViewCount != 1 {
			t.Fatalf("decrement = (%+v, %v)", p, err)
		}
		p, err = uc.AdjustViewCount(context.Background(), "p1", domain.ViewActionReset)
		if err != nil || p.ViewCount != 0 || p.LastViewedAt != nil {
			t.Fatalf("reset = (%+v, %v)", p, err)
		}
		p, err = uc.AdjustViewCount(context.Background(), "p1", domain.ViewActionDecrement)
		if err != nil || p.ViewCount != 0 {
			t.Fatalf("clamped decrement = (%+v, %v)", p, err)
		}
	})

	t.Run("list history uses default limit when non-positive", func(t *testing.T) {
		t.Parallel()
		store := newFakeViewStore()
		uc := usecase.NewTrackViewUseCase(store).WithClock(testClock(now))
		if _, err := uc.RecordView(context.Background(), "p1", "v1", domain.ViewSourceDwell); err != nil {
			t.Fatalf("RecordView: %v", err)
		}
		hist, err := uc.ListViewHistory(context.Background(), "p1", 0)
		if err != nil || len(hist) != 1 || hist[0].Source != domain.ViewSourceDwell {
			t.Fatalf("ListViewHistory = (%+v, %v)", hist, err)
		}
	})

	t.Run("validation and error cases", func(t *testing.T) {
		t.Parallel()
		store := newFakeViewStore()
		uc := usecase.NewTrackViewUseCase(store)

		cases := []struct {
			name string
			fn   func() error
			want error
		}{
			{name: "record empty photo id", fn: func() error { _, err := uc.RecordView(context.Background(), " ", "v1", ""); return err }, want: domain.ErrInvalidInput},
			{name: "record invalid source", fn: func() error { _, err := uc.RecordView(context.Background(), "p1", "v1", "bogus"); return err }, want: domain.ErrInvalidViewSource},
			{name: "record unknown photo", fn: func() error { _, err := uc.RecordView(context.Background(), "missing", "v1", ""); return err }, want: domain.ErrPhotoNotFound},
			{name: "adjust empty photo id", fn: func() error {
				_, err := uc.AdjustViewCount(context.Background(), "", domain.ViewActionIncrement)
				return err
			}, want: domain.ErrInvalidInput},
			{name: "adjust invalid action", fn: func() error { _, err := uc.AdjustViewCount(context.Background(), "p1", "bogus"); return err }, want: domain.ErrInvalidViewAction},
			{name: "adjust unknown photo", fn: func() error {
				_, err := uc.AdjustViewCount(context.Background(), "missing", domain.ViewActionReset)
				return err
			}, want: domain.ErrPhotoNotFound},
			{name: "history empty photo id", fn: func() error { _, err := uc.ListViewHistory(context.Background(), "", 10); return err }, want: domain.ErrInvalidInput},
			{name: "history unknown photo", fn: func() error { _, err := uc.ListViewHistory(context.Background(), "missing", 10); return err }, want: domain.ErrPhotoNotFound},
		}
		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				t.Parallel()
				if err := tc.fn(); !errors.Is(err, tc.want) {
					t.Fatalf("got %v, want %v", err, tc.want)
				}
			})
		}

		nilUC := usecase.NewTrackViewUseCase(nil)
		if _, err := nilUC.RecordView(context.Background(), "p1", "", ""); err == nil {
			t.Fatal("expected nil store error")
		}
		if _, err := nilUC.AdjustViewCount(context.Background(), "p1", domain.ViewActionIncrement); err == nil {
			t.Fatal("expected nil store error")
		}
		if _, err := nilUC.ListViewHistory(context.Background(), "p1", 10); err == nil {
			t.Fatal("expected nil store error")
		}
	})
}

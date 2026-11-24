import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { DEFAULT_DWELL_MS, DwellTimer, type DwellTriggerEvent } from './dwell-timer';

describe('DwellTimer state machine', () => {
  beforeEach(() => {
    vi.useFakeTimers();
  });

  afterEach(() => {
    vi.useRealTimers();
  });

  it('does not count when closed before 1.5s dwell threshold', () => {
    const triggered: DwellTriggerEvent[] = [];
    const timer = new DwellTimer((ev) => triggered.push(ev), {
      getVisibilityState: () => 'visible',
      generateViewId: () => 'uuid-1',
    });

    timer.openPhoto('photo-1');
    timer.notifyMediaLoaded('photo-1');
    expect(timer.isPending()).toBe(true);

    vi.advanceTimersByTime(1200);
    expect(triggered).toHaveLength(0);

    timer.cancel();
    vi.advanceTimersByTime(1000);
    expect(triggered).toHaveLength(0);
    expect(timer.isPending()).toBe(false);
    expect(timer.getPhotoId()).toBeNull();
  });

  it('waits for media load before starting 1.5s dwell countdown and triggers once', () => {
    const triggered: DwellTriggerEvent[] = [];
    const timer = new DwellTimer((ev) => triggered.push(ev), {
      getVisibilityState: () => 'visible',
      generateViewId: () => 'uuid-2',
    });

    timer.openPhoto('photo-2', 'slideshow');
    expect(timer.getPhotoId()).toBe('photo-2');
    expect(timer.getViewId()).toBe('uuid-2');

    vi.advanceTimersByTime(2500);
    expect(triggered).toHaveLength(0);

    timer.notifyMediaLoaded('wrong-photo');
    expect(timer.isPending()).toBe(false);

    timer.notifyMediaLoaded('photo-2');
    vi.advanceTimersByTime(DEFAULT_DWELL_MS - 1);
    expect(triggered).toHaveLength(0);

    vi.advanceTimersByTime(1);
    expect(triggered).toEqual([{ photoId: 'photo-2', viewId: 'uuid-2', source: 'slideshow' }]);
    expect(timer.isCounted()).toBe(true);

    timer.notifyMediaLoaded('photo-2');
    vi.advanceTimersByTime(3000);
    expect(triggered).toHaveLength(1);
  });

  it('pauses when tab becomes hidden and resumes full dwell once visible again', () => {
    let visibility: DocumentVisibilityState = 'visible';
    const triggered: DwellTriggerEvent[] = [];
    const timer = new DwellTimer((ev) => triggered.push(ev), {
      getVisibilityState: () => visibility,
      generateViewId: () => 'uuid-3',
    });

    timer.openPhoto('photo-3');
    timer.notifyMediaLoaded('photo-3');
    vi.advanceTimersByTime(800);
    expect(triggered).toHaveLength(0);

    visibility = 'hidden';
    timer.handleVisibilityChange();
    expect(timer.isPending()).toBe(false);

    vi.advanceTimersByTime(3000);
    expect(triggered).toHaveLength(0);

    visibility = 'visible';
    timer.handleVisibilityChange();
    vi.advanceTimersByTime(1499);
    expect(triggered).toHaveLength(0);

    vi.advanceTimersByTime(1);
    expect(triggered).toHaveLength(1);
  });

  it('does not double-count when switching CR3 <-> JPG variants on the same photo', () => {
    const triggered: DwellTriggerEvent[] = [];
    const timer = new DwellTimer((ev) => triggered.push(ev), {
      getVisibilityState: () => 'visible',
      generateViewId: () => 'uuid-4',
    });

    timer.openPhoto('photo-4');
    timer.notifyMediaLoaded('photo-4');
    vi.advanceTimersByTime(1500);
    expect(triggered).toHaveLength(1);

    timer.switchVariant('photo-4');
    timer.notifyMediaLoaded('photo-4');
    vi.advanceTimersByTime(3000);
    expect(triggered).toHaveLength(1);

    timer.switchVariant('photo-5');
    expect(timer.getPhotoId()).toBe('photo-5');
  });

  it('uses default crypto UUID and document visibility fallbacks cleanly', () => {
    const triggered: DwellTriggerEvent[] = [];
    const origCrypto = globalThis.crypto;
    Object.defineProperty(globalThis, 'crypto', { value: {}, configurable: true });
    const timer = new DwellTimer((ev) => triggered.push(ev));
    timer.openPhoto('photo-6');
    expect(timer.getViewId()).toMatch(/^view-/);
    timer.notifyMediaLoaded('photo-6');
    vi.advanceTimersByTime(1500);
    expect(triggered).toHaveLength(1);
    Object.defineProperty(globalThis, 'crypto', { value: origCrypto, configurable: true });
  });
});

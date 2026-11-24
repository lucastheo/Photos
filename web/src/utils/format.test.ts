import { afterEach, describe, expect, it, vi } from 'vitest';
import type { PhotoFilterParams } from '../types/api';
import {
  applySortOption,
  applyViewPreset,
  buildNearbyGPSFilter,
  buildOpenStreetMapUrl,
  detectSortOption,
  detectViewPreset,
  filterToSearchParams,
  formatDateTime,
  formatDuration,
  formatFileSize,
  formatStackBadge,
  runViewTransition,
  searchParamsToFilter,
  toggleEmojiTagFilter,
  toggleFormatFilter,
  toggleUntaggedFilter,
} from './format';

describe('format and filter utilities', () => {
  afterEach(() => {
    vi.restoreAllMocks();
  });

  it('serializes and parses full PhotoFilterParams round-trips', () => {
    const filter: PhotoFilterParams = {
      min_views: 1,
      max_views: 5,
      exact_views: 2,
      emoji_tags: ['❤️', '🏖️'],
      untagged: false,
      formats: ['cr3', 'jpg'],
      media_type: 'photo',
      folder: '2026/Trips',
      has_gps: true,
      min_lat: -23.6,
      max_lat: -23.4,
      min_lng: -46.7,
      max_lng: -46.5,
      from_date: '2026-01-01',
      to_date: '2026-12-31',
      q: 'sunset',
      sort_by: 'random',
      sort_order: 'desc',
      seed: 99,
      page: 3,
      page_size: 25,
    };

    const params = filterToSearchParams(filter);
    const parsed = searchParamsToFilter(params);
    expect(parsed).toEqual({
      min_views: 1,
      max_views: 5,
      exact_views: 2,
      emoji_tags: ['❤️', '🏖️'],
      formats: ['cr3', 'jpg'],
      media_type: 'photo',
      folder: '2026/Trips',
      has_gps: true,
      min_lat: -23.6,
      max_lat: -23.4,
      min_lng: -46.7,
      max_lng: -46.5,
      from_date: '2026-01-01',
      to_date: '2026-12-31',
      q: 'sunset',
      sort_by: 'random',
      sort_order: 'desc',
      seed: 99,
      page: 3,
      page_size: 25,
    });
  });

  it('handles untagged=true and has_gps=false and ignores invalid numeric params', () => {
    const params = new URLSearchParams(
      'untagged=true&has_gps=false&min_views=abc&formats=&sort_by=invalid&sort_order=bad',
    );
    expect(searchParamsToFilter(params)).toEqual({ untagged: true, has_gps: false });
    expect(filterToSearchParams({ untagged: true, has_gps: false }).toString()).toBe(
      'untagged=true&has_gps=false',
    );
  });

  it('applies and detects view count presets', () => {
    const base: PhotoFilterParams = { exact_views: 4, max_views: 9, page: 3 };
    const all = applyViewPreset(base, 'all');
    expect(detectViewPreset(all)).toBe('all');

    const unseen = applyViewPreset(base, 'unseen');
    expect(unseen).toMatchObject({ min_views: 0, max_views: 0, page: 1 });
    expect(detectViewPreset(unseen)).toBe('unseen');

    const seen = applyViewPreset(base, 'seen');
    expect(seen).toMatchObject({ min_views: 1, page: 1 });
    expect(seen.max_views).toBeUndefined();
    expect(detectViewPreset(seen)).toBe('seen');

    expect(detectViewPreset({ exact_views: 2 })).toBe('custom');
    expect(detectViewPreset({ min_views: 2, max_views: 5 })).toBe('custom');
  });

  it('applies and detects sort options including seeded random', () => {
    const opts = [
      'views_asc',
      'views_desc',
      'date_desc',
      'date_asc',
      'last_viewed_desc',
      'last_viewed_asc',
      'random',
    ] as const;
    for (const opt of opts) {
      const updated = applySortOption({}, opt, 123);
      expect(detectSortOption(updated)).toBe(opt);
      if (opt === 'random') {
        expect(updated.seed).toBe(123);
      } else {
        expect(updated.seed).toBeUndefined();
      }
    }
    expect(detectSortOption({})).toBe('views_asc');
  });

  it('toggles emoji tags, untagged, and format filters', () => {
    let f: PhotoFilterParams = { untagged: true };
    f = toggleEmojiTagFilter(f, '❤️');
    expect(f.emoji_tags).toEqual(['❤️']);
    expect(f.untagged).toBe(false);

    f = toggleEmojiTagFilter(f, '🏖️');
    expect(f.emoji_tags).toEqual(['❤️', '🏖️']);

    f = toggleEmojiTagFilter(f, '❤️');
    expect(f.emoji_tags).toEqual(['🏖️']);

    f = toggleUntaggedFilter(f);
    expect(f.untagged).toBe(true);
    expect(f.emoji_tags).toEqual([]);

    f = toggleFormatFilter(f, 'CR3');
    expect(f.formats).toEqual(['cr3']);
    f = toggleFormatFilter(f, 'cr3');
    expect(f.formats).toEqual([]);
  });

  it('formats durations, file sizes, stack badges, dates, and GPS helpers', () => {
    expect(formatDuration(undefined)).toBe('');
    expect(formatDuration(75)).toBe('01:15');
    expect(formatFileSize(512)).toBe('512 B');
    expect(formatFileSize(2048)).toBe('2.0 KB');
    expect(formatFileSize(5 * 1024 * 1024)).toBe('5.0 MB');
    expect(formatFileSize(2 * 1024 * 1024 * 1024)).toBe('2.00 GB');

    expect(
      formatStackBadge({ formats: ['cr3', 'jpg'], media_type: 'photo', duration_sec: 0 }),
    ).toBe('CR3 + JPG');
    expect(formatStackBadge({ formats: ['mp4'], media_type: 'video', duration_sec: 75 })).toBe(
      'MP4 01:15',
    );

    expect(formatDateTime(null)).toBe('Unknown');
    expect(formatDateTime('not-a-date')).toBe('not-a-date');
    expect(formatDateTime('2026-11-17T18:40:00Z')).toContain('2026');

    expect(buildNearbyGPSFilter(89.99, -179.99, 0.05)).toEqual({
      has_gps: true,
      min_lat: 89.94,
      max_lat: 90,
      min_lng: -180,
      max_lng: -179.94,
      page: 1,
    });
    expect(buildOpenStreetMapUrl(12.34, 56.78)).toBe(
      'https://www.openstreetmap.org/?mlat=12.34&mlon=56.78#map=14/12.34/56.78',
    );
  });

  it('runs View Transitions with feature detection, reduced motion, and fallback', () => {
    const update = vi.fn();
    runViewTransition(update);
    expect(update).toHaveBeenCalledTimes(1);

    const startMock = vi.fn((arg: unknown) => {
      if (typeof arg === 'function') {
        arg();
      } else {
        (arg as { update: () => void }).update();
      }
    });
    Object.defineProperty(document, 'startViewTransition', {
      value: startMock,
      configurable: true,
    });
    vi.spyOn(window, 'matchMedia').mockReturnValue({ matches: false } as MediaQueryList);

    runViewTransition(update);
    runViewTransition(update, 'forward');
    expect(startMock).toHaveBeenCalledTimes(2);
    expect(update).toHaveBeenCalledTimes(3);

    const fallbackStart = vi.fn((arg: unknown) => {
      if (typeof arg !== 'function') throw new TypeError('callback required');
      arg();
    });
    Object.defineProperty(document, 'startViewTransition', {
      value: fallbackStart,
      configurable: true,
    });
    runViewTransition(update, 'backward');
    expect(update).toHaveBeenCalledTimes(4);

    vi.spyOn(window, 'matchMedia').mockReturnValue({ matches: true } as MediaQueryList);
    runViewTransition(update, 'forward');
    expect(update).toHaveBeenCalledTimes(5);

    delete (document as unknown as { startViewTransition?: unknown }).startViewTransition;
  });
});

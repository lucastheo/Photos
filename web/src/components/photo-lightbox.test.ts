import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { ApiClient, ApiError } from '../api/client';
import type { PhotoResponse } from '../types/api';
import './photo-lightbox';
import type { PhotoLightbox } from './photo-lightbox';

function makePhoto(overrides: Partial<PhotoResponse> = {}): PhotoResponse {
  return {
    id: 'p1',
    base_name: 'IMG_0042',
    folder_path: '2026/Trips',
    formats: ['cr3', 'jpg'],
    media_type: 'photo',
    width: 6000,
    height: 4000,
    taken_at: '2026-06-15T14:20:00Z',
    gps_latitude: -23.5505,
    gps_longitude: -46.6333,
    exif: {
      orientation: 1,
      camera_make: 'Canon',
      camera_model: 'EOS R5',
      lens_model: 'RF 24-70mm',
      iso: 100,
      f_number: 2.8,
      exposure_time: '1/250',
      focal_length_mm: 35,
      gps_latitude: -23.5505,
      gps_longitude: -46.6333,
    },
    view_count: 0,
    emoji_tags: ['❤️'],
    files: [
      {
        id: 'f1',
        relative_path: '2026/Trips/IMG_0042.CR3',
        file_name: 'IMG_0042.CR3',
        folder_path: '2026/Trips',
        content_hash: 'abc123def456',
        format: 'cr3',
        file_size: 42000000,
        status: 'active',
      },
      {
        id: 'f2',
        relative_path: '2026/Trips/IMG_0042.JPG',
        file_name: 'IMG_0042.JPG',
        folder_path: '2026/Trips',
        content_hash: 'def456789012',
        format: 'jpg',
        file_size: 8500000,
        status: 'active',
      },
    ],
    view_history: [
      { id: 1, photo_id: 'p1', viewed_at: '2026-06-16T10:00:00Z', source: 'dwell' },
    ],
    status: 'active',
    thumbnail_url: '/api/v1/photos/p1/thumbnail',
    preview_url: '/api/v1/photos/p1/preview',
    original_url: '/api/v1/photos/p1/original',
    ...overrides,
  };
}

describe('PhotoLightbox', () => {
  beforeEach(() => {
    document.body.innerHTML = '';
    vi.useFakeTimers();
  });

  afterEach(() => {
    vi.useRealTimers();
    vi.restoreAllMocks();
  });

  it('opens native dialog, triggers 1.5s dwell view once, and handles variant switching without double counting', async () => {
    const api = new ApiClient();
    const initial = makePhoto({ view_count: 0 });
    const full = makePhoto({ view_count: 0 });
    const viewed = makePhoto({ view_count: 1 });

    vi.spyOn(api, 'getPhoto').mockResolvedValue(full);
    const recordSpy = vi.spyOn(api, 'recordView').mockResolvedValue(viewed);

    const el = document.createElement('photo-lightbox') as PhotoLightbox;
    el.api = api;
    el.hasPrev = true;
    el.hasNext = true;
    document.body.appendChild(el);
    el.photo = initial;
    await el.updateComplete;
    await Promise.resolve();
    await el.updateComplete;

    const dialog = el.shadowRoot?.querySelector('dialog');
    expect(dialog?.open).toBe(true);
    expect(dialog?.hasAttribute('role')).toBe(false);

    const img = el.shadowRoot?.querySelector('img');
    expect(img?.getAttribute('src')).toBe('/api/v1/photos/p1/preview?format=cr3');

    img?.dispatchEvent(new Event('load'));
    await vi.advanceTimersByTimeAsync(1500);
    await el.updateComplete;

    expect(recordSpy).toHaveBeenCalledTimes(1);
    expect(el.shadowRoot?.querySelector('.view-count-big')?.textContent).toContain('1');

    const jpgBtn = Array.from(el.shadowRoot?.querySelectorAll('button') ?? []).find(
      (b) => b.textContent?.trim() === 'JPG',
    );
    jpgBtn?.click();
    await el.updateComplete;
    expect(el.shadowRoot?.querySelector('img')?.getAttribute('src')).toBe(
      '/api/v1/photos/p1/preview?format=jpg',
    );

    el.shadowRoot?.querySelector('img')?.dispatchEvent(new Event('load'));
    await vi.advanceTimersByTimeAsync(2000);
    expect(recordSpy).toHaveBeenCalledTimes(1);

    const zoomBtn = Array.from(el.shadowRoot?.querySelectorAll('button') ?? []).find(
      (b) => b.textContent?.trim() === 'Zoom',
    );
    zoomBtn?.click();
    await el.updateComplete;
    expect(el.shadowRoot?.querySelector('img')?.classList.contains('zoomed')).toBe(true);
  });

  it('supports manual view adjustments, emoji tagging, GPS filter, and keyboard shortcuts', async () => {
    const api = new ApiClient();
    const photo = makePhoto({ view_count: 1, emoji_tags: ['❤️'] });
    vi.spyOn(api, 'getPhoto').mockResolvedValue(photo);
    const adjustSpy = vi
      .spyOn(api, 'adjustViewCount')
      .mockResolvedValue(makePhoto({ view_count: 2 }));
    const addTagSpy = vi
      .spyOn(api, 'addTag')
      .mockResolvedValue(makePhoto({ emoji_tags: ['❤️', '🏖️'] }));
    const removeTagSpy = vi
      .spyOn(api, 'removeTag')
      .mockResolvedValue(makePhoto({ emoji_tags: [] }));

    const el = document.createElement('photo-lightbox') as PhotoLightbox;
    el.api = api;
    el.hasPrev = true;
    el.hasNext = true;
    document.body.appendChild(el);
    el.photo = photo;
    await el.updateComplete;

    const navEvents: string[] = [];
    el.addEventListener('lightbox-navigate', (e) => {
      navEvents.push((e as CustomEvent<{ direction: string }>).detail.direction);
    });

    document.dispatchEvent(new KeyboardEvent('keydown', { key: 'ArrowRight' }));
    document.dispatchEvent(new KeyboardEvent('keydown', { key: 'ArrowLeft' }));
    expect(navEvents).toEqual(['next', 'prev']);

    document.dispatchEvent(new KeyboardEvent('keydown', { key: ' ' }));
    await el.updateComplete;
    await vi.advanceTimersByTimeAsync(4000);
    expect(navEvents).toEqual(['next', 'prev', 'next']);

    const intervalSelect = el.shadowRoot?.querySelector<HTMLSelectElement>(
      'select[aria-label="Slideshow interval"]',
    );
    if (intervalSelect) {
      intervalSelect.value = '3';
      intervalSelect.dispatchEvent(new Event('change'));
    }
    await vi.advanceTimersByTimeAsync(3000);
    expect(navEvents).toEqual(['next', 'prev', 'next', 'next']);

    el.hasNext = false;
    await vi.advanceTimersByTimeAsync(3000);

    document.dispatchEvent(new KeyboardEvent('keydown', { key: 'i' }));
    await el.updateComplete;
    expect(el.shadowRoot?.querySelector('aside.drawer')).toBeNull();
    document.dispatchEvent(new KeyboardEvent('keydown', { key: 'i' }));
    await el.updateComplete;

    const btnByText = (txt: string) =>
      Array.from(el.shadowRoot?.querySelectorAll('button') ?? []).find(
        (b) => b.textContent?.trim() === txt,
      );
    btnByText('+1')?.click();
    await Promise.resolve();
    expect(adjustSpy).toHaveBeenCalledWith('p1', 'increment');

    const emojiInput = el.shadowRoot?.querySelector<HTMLInputElement>(
      'input[aria-label="Custom emoji tag"]',
    );
    if (emojiInput) {
      emojiInput.value = '🏖️';
      emojiInput.dispatchEvent(new Event('input'));
    }
    el.shadowRoot?.querySelector('form.emoji-form')?.dispatchEvent(new Event('submit', { cancelable: true }));
    await Promise.resolve();
    expect(addTagSpy).toHaveBeenCalledWith('p1', '🏖️');

    const removeHeart = el.shadowRoot?.querySelector<HTMLButtonElement>('button[aria-label="Remove ❤️"]');
    removeHeart?.click();
    await Promise.resolve();
    expect(removeTagSpy).toHaveBeenCalledWith('p1', '❤️');

    const gpsEvents: Array<{ lat: number; lng: number }> = [];
    el.addEventListener('filter-nearby-gps', (e) => {
      gpsEvents.push((e as CustomEvent<{ lat: number; lng: number }>).detail);
    });
    btnByText('Filter Nearby Photos')?.click();
    expect(gpsEvents).toEqual([{ lat: -23.5505, lng: -46.6333 }]);
  });

  it('handles video playback error transcode fallback and backdrop click close fallback', async () => {
    const api = new ApiClient();
    const videoPhoto = makePhoto({
      id: 'v1',
      formats: ['mov'],
      media_type: 'video',
      duration_sec: 75,
      gps_latitude: undefined,
      gps_longitude: undefined,
      exif: undefined,
    });
    vi.spyOn(api, 'getPhoto').mockRejectedValue(new ApiError(500, 'INTERNAL', 'detail fail'));

    const el = document.createElement('photo-lightbox') as PhotoLightbox;
    el.api = api;
    document.body.appendChild(el);
    el.photo = videoPhoto;
    await el.updateComplete;
    await Promise.resolve();
    await el.updateComplete;

    expect(el.shadowRoot?.querySelector('[role="alert"]')?.textContent).toBe('detail fail');

    const video = el.shadowRoot?.querySelector('video');
    video?.dispatchEvent(new Event('error'));
    await el.updateComplete;

    const transcodeBtn = Array.from(el.shadowRoot?.querySelectorAll('button') ?? []).find((b) =>
      b.textContent?.includes('Play Transcoded Preview'),
    );
    transcodeBtn?.click();
    await el.updateComplete;
    expect(el.shadowRoot?.querySelector('video')?.getAttribute('src')).toContain('transcode=1');

    const dialog = el.shadowRoot?.querySelector('dialog') as HTMLDialogElement;
    vi.spyOn(dialog, 'getBoundingClientRect').mockReturnValue({
      left: 50,
      top: 50,
      right: 450,
      bottom: 350,
      width: 400,
      height: 300,
      x: 50,
      y: 50,
      toJSON: () => ({}),
    });

    const closeEvents: number[] = [];
    el.addEventListener('lightbox-close', () => closeEvents.push(1));

    dialog.dispatchEvent(new MouseEvent('click', { clientX: 100, clientY: 100 }));
    expect(dialog.open).toBe(true);

    dialog.dispatchEvent(new MouseEvent('click', { clientX: 5, clientY: 5 }));
    expect(dialog.open).toBe(false);
    expect(closeEvents).toHaveLength(1);
  });

  it('handles toolbar controls, error banners, visibilitychange, and photo=null cleanup', async () => {
    const api = new ApiClient();
    const photo = makePhoto({ view_count: 1, emoji_tags: [] });
    vi.spyOn(api, 'getPhoto').mockResolvedValue(photo);
    vi.spyOn(api, 'recordView').mockRejectedValue(new Error('record fail'));
    vi.spyOn(api, 'adjustViewCount').mockRejectedValue(new ApiError(400, 'BAD', 'adjust fail'));
    vi.spyOn(api, 'addTag').mockRejectedValue(new ApiError(400, 'BAD', 'tag fail'));

    const el = document.createElement('photo-lightbox') as PhotoLightbox;
    el.api = api;
    el.hasPrev = true;
    el.hasNext = true;
    document.body.appendChild(el);
    el.photo = photo;
    await el.updateComplete;

    el.shadowRoot?.querySelector('img')?.dispatchEvent(new Event('load'));
    document.dispatchEvent(new Event('visibilitychange'));
    await vi.advanceTimersByTimeAsync(1500);
    await el.updateComplete;
    expect(el.shadowRoot?.querySelector('[role="alert"]')?.textContent).toBe('Failed to record view.');

    const btnByText = (txt: string) =>
      Array.from(el.shadowRoot?.querySelectorAll('button') ?? []).find((b) =>
        b.textContent?.trim().includes(txt),
      );

    btnByText('Zoom')?.click();
    await el.updateComplete;
    btnByText('Fit')?.click();
    btnByText('Details')?.click();
    await el.updateComplete;
    btnByText('Details')?.click();
    await el.updateComplete;

    btnByText('+1')?.click();
    await Promise.resolve();
    await el.updateComplete;
    expect(el.shadowRoot?.querySelector('[role="alert"]')?.textContent).toBe('adjust fail');

    btnByText('⭐')?.click();
    await Promise.resolve();
    await el.updateComplete;
    expect(el.shadowRoot?.querySelector('[role="alert"]')?.textContent).toBe('tag fail');

    const emojiInput = el.shadowRoot?.querySelector<HTMLInputElement>(
      'input[aria-label="Custom emoji tag"]',
    );
    emojiInput?.dispatchEvent(new KeyboardEvent('keydown', { key: 'ArrowRight', bubbles: true }));
    el.shadowRoot?.querySelector('form.emoji-form')?.dispatchEvent(new Event('submit', { cancelable: true }));

    el.shadowRoot?.querySelector<HTMLButtonElement>('button[aria-label="Close lightbox"]')?.click();
    el.photo = null;
    await el.updateComplete;
    expect(el.shadowRoot?.querySelector('dialog')?.open).toBe(false);
  });

  it('starts with hidden HUD overlays, reveals on mouse movement, and hides after idle timeout', async () => {
    const api = new ApiClient();
    const photo = makePhoto({ view_count: 2 });
    vi.spyOn(api, 'getPhoto').mockResolvedValue(photo);

    const el = document.createElement('photo-lightbox') as PhotoLightbox;
    el.api = api;
    document.body.appendChild(el);
    el.photo = photo;
    await el.updateComplete;

    const dialog = el.shadowRoot?.querySelector('dialog') as HTMLDialogElement;
    expect(dialog.classList.contains('ui-hidden')).toBe(true);
    expect(dialog.classList.contains('ui-visible')).toBe(false);
    expect(el.shadowRoot?.querySelector('.stage')).not.toBeNull();
    expect(el.shadowRoot?.querySelector('.toolbar')).not.toBeNull();
    expect(el.shadowRoot?.querySelector('aside.drawer')).not.toBeNull();

    dialog.dispatchEvent(new MouseEvent('mousemove', { bubbles: true }));
    await el.updateComplete;
    expect(dialog.classList.contains('ui-visible')).toBe(true);

    await vi.advanceTimersByTimeAsync(1500);
    await el.updateComplete;
    expect(dialog.classList.contains('ui-visible')).toBe(true);

    dialog.dispatchEvent(new MouseEvent('pointermove', { bubbles: true }));
    await vi.advanceTimersByTimeAsync(1500);
    await el.updateComplete;
    expect(dialog.classList.contains('ui-visible')).toBe(true);

    await vi.advanceTimersByTimeAsync(700);
    await el.updateComplete;
    expect(dialog.classList.contains('ui-hidden')).toBe(true);
  });
});

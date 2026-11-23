import { beforeEach, describe, expect, it, vi } from 'vitest';
import { ApiClient, ApiError } from '../api/client';
import type { PhotoResponse } from '../types/api';
import './gallery-view';
import type { PhotoGalleryView } from './gallery-view';

function samplePhoto(id: string, views = 0, extra: Partial<PhotoResponse> = {}): PhotoResponse {
  return {
    id,
    base_name: `IMG_${id}`,
    folder_path: '2026/Trip',
    formats: ['cr3', 'jpg'],
    media_type: 'photo',
    width: 4000,
    height: 3000,
    view_count: views,
    emoji_tags: ['❤️'],
    files: [],
    status: 'active',
    thumbnail_url: `/api/v1/photos/${id}/thumbnail`,
    preview_url: `/api/v1/photos/${id}/preview`,
    original_url: `/api/v1/photos/${id}/original`,
    ...extra,
  };
}

describe('PhotoGalleryView', () => {
  beforeEach(() => {
    document.body.innerHTML = '';
    localStorage.clear();
    vi.restoreAllMocks();
  });

  it('renders grid cards with image priority attributes, deferred cards, and in-place badge updates', async () => {
    const api = new ApiClient();
    const items = Array.from({ length: 14 }, (_, idx) =>
      samplePhoto(String(idx + 1), idx === 0 ? 0 : 2, idx === 0 ? { gps_latitude: 1, gps_longitude: 2 } : {}),
    );

    vi.spyOn(api, 'listPhotos').mockResolvedValue({
      items,
      total: 100,
      page: 1,
      page_size: 50,
    });
    vi.spyOn(api, 'listTags').mockResolvedValue([{ emoji: '❤️', count: 14 }]);

    const el = document.createElement('photo-gallery-view') as PhotoGalleryView;
    el.api = api;
    el.folders = [{ folder_path: '2026/Trip', count: 14 }];
    document.body.appendChild(el);
    await new Promise((r) => setTimeout(r, 0));
    await el.updateComplete;

    const cards = el.shadowRoot?.querySelectorAll<HTMLButtonElement>('button.photo-card') ?? [];
    expect(cards).toHaveLength(14);
    expect(cards[0].querySelector('img')?.getAttribute('fetchpriority')).toBe('high');
    expect(cards[0].querySelector('img')?.hasAttribute('loading')).toBe(false);
    expect(cards[2].querySelector('img')?.getAttribute('loading')).toBe('lazy');
    expect(cards[2].querySelector('img')?.hasAttribute('fetchpriority')).toBe(false);
    expect(cards[0].classList.contains('deferred-card')).toBe(false);
    expect(cards[12].classList.contains('deferred-card')).toBe(true);
    expect(cards[0].querySelector('.badge-views.unseen')?.textContent?.trim()).toBe('👁 0');
    expect(cards[0].querySelector('.badge-format')?.textContent?.trim()).toBe('CR3 + JPG');
    expect(cards[0].querySelector('.badge-gps')).not.toBeNull();

    el.updatePhotoInPlace(samplePhoto('1', 1));
    await el.updateComplete;
    const updatedCards = el.shadowRoot?.querySelectorAll('button.photo-card') ?? [];
    expect(updatedCards).toHaveLength(14);
    expect(updatedCards[0].querySelector('.badge-views')?.textContent?.trim()).toBe('👁 1');

    const selected: string[] = [];
    el.addEventListener('photo-select', (e) => {
      selected.push((e as CustomEvent<{ photo: PhotoResponse }>).detail.photo.id);
    });
    cards[0].click();
    expect(selected).toEqual(['1']);
  });

  it('dispatches filter updates for presets, tags, formats, GPS bounds, sort, and pagination', async () => {
    const api = new ApiClient();
    const listSpy = vi.spyOn(api, 'listPhotos').mockResolvedValue({
      items: [samplePhoto('1', 0)],
      total: 120,
      page: 1,
      page_size: 50,
    });
    vi.spyOn(api, 'listTags').mockResolvedValue([{ emoji: '❤️', count: 3 }]);

    const el = document.createElement('photo-gallery-view') as PhotoGalleryView;
    el.api = api;
    document.body.appendChild(el);
    await new Promise((r) => setTimeout(r, 0));
    await el.updateComplete;

    const btnByText = (txt: string) =>
      Array.from(el.shadowRoot?.querySelectorAll('button') ?? []).find((b) =>
        b.textContent?.trim().startsWith(txt),
      );

    btnByText('Never Seen (0)')?.click();
    await new Promise((r) => setTimeout(r, 0));
    expect(listSpy).toHaveBeenLastCalledWith(expect.objectContaining({ min_views: 0, max_views: 0 }));

    btnByText('Seen (1+)')?.click();
    await new Promise((r) => setTimeout(r, 0));
    expect(listSpy).toHaveBeenLastCalledWith(expect.objectContaining({ min_views: 1 }));
    expect(listSpy.mock.lastCall?.[0]).not.toHaveProperty('max_views');

    const minInput = el.shadowRoot?.querySelector<HTMLInputElement>('input[aria-label="Min views"]');
    if (minInput) {
      minInput.value = '2';
      minInput.dispatchEvent(new Event('change'));
    }
    await new Promise((r) => setTimeout(r, 0));
    expect(listSpy).toHaveBeenLastCalledWith(expect.objectContaining({ min_views: 2 }));

    btnByText('❤️')?.click();
    await new Promise((r) => setTimeout(r, 0));
    expect(listSpy).toHaveBeenLastCalledWith(expect.objectContaining({ emoji_tags: ['❤️'] }));

    btnByText('Untagged')?.click();
    await new Promise((r) => setTimeout(r, 0));
    expect(listSpy).toHaveBeenLastCalledWith(expect.objectContaining({ untagged: true }));

    btnByText('Photos')?.click();
    await new Promise((r) => setTimeout(r, 0));
    btnByText('CR3')?.click();
    await new Promise((r) => setTimeout(r, 0));
    expect(listSpy).toHaveBeenLastCalledWith(
      expect.objectContaining({ media_type: 'photo', formats: ['cr3'] }),
    );

    const sortSel = el.shadowRoot?.querySelector<HTMLSelectElement>('#sort-select');
    if (sortSel) {
      sortSel.value = 'random';
      sortSel.dispatchEvent(new Event('change'));
    }
    await new Promise((r) => setTimeout(r, 0));
    await el.updateComplete;
    btnByText('🎲 Reshuffle')?.click();
    await new Promise((r) => setTimeout(r, 0));
    expect(listSpy).toHaveBeenLastCalledWith(expect.objectContaining({ sort_by: 'random' }));

    const minLat = el.shadowRoot?.querySelector<HTMLInputElement>('input[aria-label="Min latitude"]');
    if (minLat) {
      minLat.value = '-23.5';
      minLat.dispatchEvent(new Event('change'));
    }
    await new Promise((r) => setTimeout(r, 0));
    expect(listSpy).toHaveBeenLastCalledWith(expect.objectContaining({ min_lat: -23.5 }));

    await el.applyNearbyGPS(-23.55, -46.63);
    expect(listSpy).toHaveBeenLastCalledWith(expect.objectContaining({ has_gps: true }));

    btnByText('Next')?.click();
    await new Promise((r) => setTimeout(r, 0));
    expect(listSpy).toHaveBeenLastCalledWith(expect.objectContaining({ page: 2 }));

    btnByText('Reset Filters')?.click();
    await new Promise((r) => setTimeout(r, 0));
    expect(listSpy).toHaveBeenLastCalledWith({ page: 1, page_size: 50, sort_by: 'views', sort_order: 'asc' });
  });

  it('displays error banner when listPhotos fails', async () => {
    const api = new ApiClient();
    vi.spyOn(api, 'listPhotos').mockRejectedValue(new ApiError(500, 'ERR', 'DB unavailable'));
    vi.spyOn(api, 'listTags').mockResolvedValue([]);

    const el = document.createElement('photo-gallery-view') as PhotoGalleryView;
    el.api = api;
    document.body.appendChild(el);
    await new Promise((r) => setTimeout(r, 0));
    await el.updateComplete;

    const alert = el.shadowRoot?.querySelector('[role="alert"]');
    expect(alert?.textContent).toBe('DB unavailable');
  });

  it('handles search, folder, GPS select, dates, remaining GPS bounds, previous page, and empty state', async () => {
    const api = new ApiClient();
    const listSpy = vi.spyOn(api, 'listPhotos').mockResolvedValue({
      items: [],
      total: 0,
      page: 1,
      page_size: 50,
    });
    vi.spyOn(api, 'listTags').mockResolvedValue([]);

    const el = document.createElement('photo-gallery-view') as PhotoGalleryView;
    el.api = api;
    el.folders = [{ folder_path: '', count: 5 }];
    el.filter = { page: 2, page_size: 50, min_views: 1 };
    document.body.appendChild(el);
    await new Promise((r) => setTimeout(r, 0));
    await el.updateComplete;

    expect(el.shadowRoot?.textContent).toContain('No photos match the current filters.');

    const rowLabels = Array.from(el.shadowRoot?.querySelectorAll('#filter-controls .filter-row > label') ?? []).map(
      (l) => l.textContent?.trim(),
    );
    expect(rowLabels).toEqual(['Views:', 'Emoji Tags:', 'Media:', 'Path:', 'GPS:', 'Date:']);

    el.updatePhotoInPlace(samplePhoto('missing', 2));

    const search = el.shadowRoot?.querySelector<HTMLInputElement>('input[type="search"]');
    if (search) {
      search.value = 'beach';
      search.dispatchEvent(new Event('change'));
    }
    const folderSel = el.shadowRoot?.querySelector<HTMLSelectElement>('select[aria-label="Folder filter"]');
    if (folderSel) {
      folderSel.value = '';
      folderSel.dispatchEvent(new Event('change'));
    }
    const gpsSel = el.shadowRoot?.querySelector<HTMLSelectElement>('select[aria-label="GPS filter"]');
    if (gpsSel) {
      gpsSel.value = 'true';
      gpsSel.dispatchEvent(new Event('change'));
      gpsSel.value = '';
      gpsSel.dispatchEvent(new Event('change'));
    }
    const fromDate = el.shadowRoot?.querySelector<HTMLInputElement>('input[aria-label="From date"]');
    const toDate = el.shadowRoot?.querySelector<HTMLInputElement>('input[aria-label="To date"]');
    expect(fromDate?.placeholder).toBe('Start date');
    expect(toDate?.placeholder).toBe('End date');
    expect(fromDate?.type).toBe('text');
    expect(toDate?.type).toBe('text');
    if (fromDate && toDate) {
      fromDate.dispatchEvent(new Event('focus'));
      expect(fromDate.type).toBe('date');
      fromDate.dispatchEvent(new Event('blur'));
      expect(fromDate.type).toBe('text');

      fromDate.dispatchEvent(new Event('focus'));
      fromDate.value = '2026-01-01';
      fromDate.dispatchEvent(new Event('change'));
      fromDate.dispatchEvent(new Event('blur'));
      expect(fromDate.type).toBe('date');

      toDate.dispatchEvent(new Event('focus'));
      toDate.value = '2026-12-31';
      toDate.dispatchEvent(new Event('change'));
      toDate.dispatchEvent(new Event('blur'));
      expect(toDate.type).toBe('date');
    }
    const minViews = el.shadowRoot?.querySelector<HTMLInputElement>('input[aria-label="Min views"]');
    const maxViews = el.shadowRoot?.querySelector<HTMLInputElement>('input[aria-label="Max views"]');
    if (minViews && maxViews) {
      minViews.value = '';
      minViews.dispatchEvent(new Event('change'));
      maxViews.value = '4';
      maxViews.dispatchEvent(new Event('change'));
    }
    for (const label of ['Max latitude', 'Min longitude', 'Max longitude']) {
      const inp = el.shadowRoot?.querySelector<HTMLInputElement>(`input[aria-label="${label}"]`);
      if (inp) {
        inp.value = '10';
        inp.dispatchEvent(new Event('change'));
        inp.value = '';
        inp.dispatchEvent(new Event('change'));
      }
    }
    await new Promise((r) => setTimeout(r, 0));
    expect(listSpy).toHaveBeenCalled();
  });

  it('toggles filter panel visibility and persists state to localStorage', async () => {
    const api = new ApiClient();
    vi.spyOn(api, 'listPhotos').mockResolvedValue({
      items: [samplePhoto('1', 0)],
      total: 42,
      page: 1,
      page_size: 50,
    });
    vi.spyOn(api, 'listTags').mockResolvedValue([]);

    const el = document.createElement('photo-gallery-view') as PhotoGalleryView;
    el.api = api;
    document.body.appendChild(el);
    await new Promise((r) => setTimeout(r, 0));
    await el.updateComplete;

    const controls = el.shadowRoot?.querySelector<HTMLElement>('#filter-controls');
    const toggle = el.shadowRoot?.querySelector<HTMLButtonElement>('button.filter-toggle');
    expect(controls?.hasAttribute('hidden')).toBe(false);
    expect(toggle?.getAttribute('aria-expanded')).toBe('true');
    expect(toggle?.textContent).toContain('Hide Filters');

    toggle?.click();
    await el.updateComplete;

    expect(controls?.hasAttribute('hidden')).toBe(true);
    expect(toggle?.getAttribute('aria-expanded')).toBe('false');
    expect(toggle?.textContent).toContain('Show Filters');
    const toggleRowButtons = Array.from(el.shadowRoot?.querySelectorAll('.filter-toggle-row button') ?? []);
    expect(toggleRowButtons.some((b) => b.textContent?.trim() === 'Reset Filters')).toBe(true);
    expect(
      el.shadowRoot?.querySelector('.filter-toggle-actions button:last-child')?.classList.contains('filter-toggle'),
    ).toBe(true);
    expect(el.shadowRoot?.querySelector('.filter-toggle-row #sort-select')).not.toBeNull();
    expect(el.shadowRoot?.querySelector('#filter-controls #sort-select')).toBeNull();
    expect(el.shadowRoot?.querySelector('.filter-toggle-row .muted')).toBeNull();
    expect(el.shadowRoot?.querySelector('#filter-controls .filter-row:last-child .muted')?.textContent).toContain(
      '42 item(s)',
    );
    expect(localStorage.getItem('photos.gallery.filtersHidden')).toBe('true');

    toggle?.click();
    await el.updateComplete;

    expect(controls?.hasAttribute('hidden')).toBe(false);
    expect(toggle?.getAttribute('aria-expanded')).toBe('true');
    expect(localStorage.getItem('photos.gallery.filtersHidden')).toBe('false');
  });

  it('initializes collapsed when localStorage has filtersHidden set to true', async () => {
    localStorage.setItem('photos.gallery.filtersHidden', 'true');
    const api = new ApiClient();
    vi.spyOn(api, 'listPhotos').mockResolvedValue({
      items: [],
      total: 0,
      page: 1,
      page_size: 50,
    });
    vi.spyOn(api, 'listTags').mockResolvedValue([]);

    const el = document.createElement('photo-gallery-view') as PhotoGalleryView;
    el.api = api;
    document.body.appendChild(el);
    await new Promise((r) => setTimeout(r, 0));
    await el.updateComplete;

    const controls = el.shadowRoot?.querySelector<HTMLElement>('#filter-controls');
    const toggle = el.shadowRoot?.querySelector<HTMLButtonElement>('button.filter-toggle');
    expect(el.filtersHidden).toBe(true);
    expect(controls?.hasAttribute('hidden')).toBe(true);
    expect(toggle?.getAttribute('aria-expanded')).toBe('false');
  });

  it('handles localStorage read and write errors gracefully', async () => {
    vi.spyOn(window.localStorage, 'getItem').mockImplementation(() => {
      throw new Error('denied');
    });
    vi.spyOn(window.localStorage, 'setItem').mockImplementation(() => {
      throw new Error('denied');
    });
    const api = new ApiClient();
    vi.spyOn(api, 'listPhotos').mockResolvedValue({
      items: [],
      total: 0,
      page: 1,
      page_size: 50,
    });
    vi.spyOn(api, 'listTags').mockResolvedValue([]);

    const el = document.createElement('photo-gallery-view') as PhotoGalleryView;
    el.api = api;
    document.body.appendChild(el);
    await new Promise((r) => setTimeout(r, 0));
    await el.updateComplete;

    const controls = el.shadowRoot?.querySelector<HTMLElement>('#filter-controls');
    const toggle = el.shadowRoot?.querySelector<HTMLButtonElement>('button.filter-toggle');
    expect(el.filtersHidden).toBe(false);
    expect(controls?.hasAttribute('hidden')).toBe(false);

    toggle?.click();
    await el.updateComplete;

    expect(el.filtersHidden).toBe(true);
    expect(controls?.hasAttribute('hidden')).toBe(true);
  });

  it('updates and clears GPS bounds from the map picker inside the popover', async () => {
    const api = new ApiClient();
    const listSpy = vi.spyOn(api, 'listPhotos').mockResolvedValue({
      items: [],
      total: 0,
      page: 1,
      page_size: 50,
    });
    vi.spyOn(api, 'listTags').mockResolvedValue([]);

    const el = document.createElement('photo-gallery-view') as PhotoGalleryView;
    el.api = api;
    document.body.appendChild(el);
    await new Promise((r) => setTimeout(r, 0));
    await el.updateComplete;

    const picker = el.shadowRoot?.querySelector('#gps-bounds-popover photo-gps-map-picker');
    expect(picker).not.toBeNull();

    picker?.dispatchEvent(
      new CustomEvent('gps-bounds-change', {
        detail: { min_lat: -23.6, max_lat: -23.5, min_lng: -46.7, max_lng: -46.6 },
        bubbles: true,
        composed: true,
      }),
    );
    await new Promise((r) => setTimeout(r, 0));
    expect(listSpy).toHaveBeenLastCalledWith(
      expect.objectContaining({
        has_gps: true,
        min_lat: -23.6,
        max_lat: -23.5,
        min_lng: -46.7,
        max_lng: -46.6,
      }),
    );

    picker?.dispatchEvent(
      new CustomEvent('gps-bounds-change', {
        detail: { min_lat: undefined, max_lat: undefined, min_lng: undefined, max_lng: undefined },
        bubbles: true,
        composed: true,
      }),
    );
    await new Promise((r) => setTimeout(r, 0));
    const lastCall = listSpy.mock.lastCall?.[0];
    expect(lastCall).not.toHaveProperty('min_lat');
    expect(lastCall).not.toHaveProperty('max_lat');
    expect(lastCall).not.toHaveProperty('min_lng');
    expect(lastCall).not.toHaveProperty('max_lng');
    expect(lastCall).not.toHaveProperty('has_gps');
  });
});

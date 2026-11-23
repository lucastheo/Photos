import { beforeEach, describe, expect, it, vi } from 'vitest';
import { ApiClient, ApiError } from './api/client';
import './main';
import { PhotoTrackerApp } from './main';
import type { PhotoResponse } from './types/api';

function makePhoto(id: string, views = 0): PhotoResponse {
  return {
    id,
    base_name: `Photo_${id}`,
    folder_path: '2026',
    formats: ['jpg'],
    media_type: 'photo',
    width: 1200,
    height: 800,
    view_count: views,
    emoji_tags: [],
    files: [],
    status: 'active',
    thumbnail_url: `/api/v1/photos/${id}/thumbnail`,
    preview_url: `/api/v1/photos/${id}/preview`,
    original_url: `/api/v1/photos/${id}/original`,
  };
}

describe('PhotoTrackerApp', () => {
  beforeEach(() => {
    document.body.innerHTML = '';
    window.history.replaceState(null, '', '/');
    vi.restoreAllMocks();
  });

  it('redirects to login when unauthenticated and navigates back on login-success', async () => {
    vi.spyOn(ApiClient.prototype, 'getSession').mockResolvedValue({
      authenticated: false,
      auth_disabled: false,
    });
    vi.spyOn(ApiClient.prototype, 'getLibraryStats').mockResolvedValue({
      total_photos: 2,
      unseen_photos: 1,
      seen_photos: 1,
      total_views: 3,
      total_files: 2,
      duplicate_groups: 0,
      duplicate_files: 0,
      missing_photos: 0,
      missing_files: 0,
      by_format: [],
      by_folder: [],
    });
    vi.spyOn(ApiClient.prototype, 'listPhotos').mockResolvedValue({
      items: [makePhoto('1'), makePhoto('2', 1)],
      total: 2,
      page: 1,
      page_size: 50,
    });
    vi.spyOn(ApiClient.prototype, 'listTags').mockResolvedValue([]);

    const el = document.createElement('photo-tracker-app') as PhotoTrackerApp;
    document.body.appendChild(el);
    await new Promise((r) => setTimeout(r, 0));
    await el.updateComplete;

    expect(window.location.pathname).toBe('/login');
    expect(el.shadowRoot?.querySelector('photo-login-view')).not.toBeNull();

    el.shadowRoot?.querySelector('main')?.dispatchEvent(
      new CustomEvent('login-success', {
        detail: { authenticated: true, auth_disabled: false },
        bubbles: true,
        composed: true,
      }),
    );
    await new Promise((r) => setTimeout(r, 0));
    await el.updateComplete;

    expect(window.location.pathname).toBe('/');
    expect(el.shadowRoot?.querySelector('photo-gallery-view')).not.toBeNull();

    const signOutBtn = Array.from(el.shadowRoot?.querySelectorAll('button') ?? []).find(
      (b) => b.textContent?.trim() === 'Sign Out',
    );
    vi.spyOn(ApiClient.prototype, 'logout').mockResolvedValue({
      authenticated: false,
      auth_disabled: false,
    });
    signOutBtn?.click();
    await new Promise((r) => setTimeout(r, 0));
    await el.updateComplete;
    expect(window.location.pathname).toBe('/login');
  });

  it('coordinates gallery, lightbox navigation, in-place photo updates, library route, and quick re-import', async () => {
    const p1 = makePhoto('p1', 0);
    const p2 = makePhoto('p2', 0);
    vi.spyOn(ApiClient.prototype, 'getSession').mockResolvedValue({
      authenticated: true,
      auth_disabled: true,
    });
    vi.spyOn(ApiClient.prototype, 'getLibraryStats').mockResolvedValue({
      total_photos: 2,
      unseen_photos: 2,
      seen_photos: 0,
      total_views: 0,
      total_files: 2,
      duplicate_groups: 0,
      duplicate_files: 0,
      missing_photos: 0,
      missing_files: 0,
      by_format: [{ format: 'jpg', count: 2 }],
      by_folder: [{ folder_path: '2026', count: 2 }],
    });
    vi.spyOn(ApiClient.prototype, 'listPhotos').mockResolvedValue({
      items: [p1, p2],
      total: 2,
      page: 1,
      page_size: 50,
    });
    vi.spyOn(ApiClient.prototype, 'listTags').mockResolvedValue([]);
    vi.spyOn(ApiClient.prototype, 'getPhoto').mockResolvedValue(p1);
    vi.spyOn(ApiClient.prototype, 'getSyncStatus').mockResolvedValue({
      status: 'idle',
      scanned_count: 0,
      added_count: 0,
      updated_count: 0,
      moved_count: 0,
      stacked_count: 0,
      duplicate_count: 0,
      unchanged_count: 0,
      missing_count: 0,
    });
    vi.spyOn(ApiClient.prototype, 'listDuplicates').mockResolvedValue([]);
    vi.spyOn(ApiClient.prototype, 'listMissing').mockResolvedValue([]);
    const syncSpy = vi.spyOn(ApiClient.prototype, 'triggerSync').mockResolvedValue({
      status: 'completed',
      scanned_count: 2,
      added_count: 0,
      updated_count: 0,
      moved_count: 0,
      stacked_count: 0,
      duplicate_count: 0,
      unchanged_count: 2,
      missing_count: 0,
    });

    const el = document.createElement('photo-tracker-app') as PhotoTrackerApp;
    document.body.appendChild(el);
    await new Promise((r) => setTimeout(r, 0));
    await el.updateComplete;

    expect(el.shadowRoot?.textContent).toContain('Unseen: 2');

    const mainEl = el.shadowRoot?.querySelector('main');
    mainEl?.dispatchEvent(
      new CustomEvent('photo-select', {
        detail: { photo: p1, index: 0 },
        bubbles: true,
        composed: true,
      }),
    );
    await el.updateComplete;
    expect(window.location.pathname).toBe('/photo/p1');

    mainEl?.dispatchEvent(
      new CustomEvent('lightbox-navigate', {
        detail: { direction: 'next' },
        bubbles: true,
        composed: true,
      }),
    );
    await el.updateComplete;
    expect(window.location.pathname).toBe('/photo/p2');

    mainEl?.dispatchEvent(
      new CustomEvent('photo-updated', {
        detail: makePhoto('p2', 1),
        bubbles: true,
        composed: true,
      }),
    );
    await el.updateComplete;

    mainEl?.dispatchEvent(new CustomEvent('lightbox-close', { bubbles: true, composed: true }));
    await el.updateComplete;
    expect(window.location.pathname).toBe('/');

    const btnByText = (txt: string) =>
      Array.from(el.shadowRoot?.querySelectorAll('button') ?? []).find(
        (b) => b.textContent?.trim() === txt,
      );

    btnByText('↻ Re-import')?.click();
    await new Promise((r) => setTimeout(r, 0));
    expect(syncSpy).toHaveBeenCalledTimes(1);

    btnByText('Library & Sync')?.click();
    await new Promise((r) => setTimeout(r, 0));
    await el.updateComplete;
    expect(window.location.pathname).toBe('/library');
    expect(el.shadowRoot?.querySelector('photo-library-view')).not.toBeNull();
  });

  it('handles deep-linked /photo/:id, popstate, filter-change, nearby GPS, and error paths', async () => {
    window.history.replaceState(null, '', '/photo/deep-1?min_views=0&max_views=0');
    vi.spyOn(ApiClient.prototype, 'getSession').mockResolvedValue({
      authenticated: true,
      auth_disabled: true,
    });
    vi.spyOn(ApiClient.prototype, 'getLibraryStats').mockResolvedValue({
      total_photos: 1,
      unseen_photos: 1,
      seen_photos: 0,
      total_views: 0,
      total_files: 1,
      duplicate_groups: 0,
      duplicate_files: 0,
      missing_photos: 0,
      missing_files: 0,
      by_format: [],
      by_folder: [],
    });
    vi.spyOn(ApiClient.prototype, 'listPhotos').mockResolvedValue({
      items: [makePhoto('deep-1')],
      total: 1,
      page: 1,
      page_size: 50,
    });
    vi.spyOn(ApiClient.prototype, 'listTags').mockResolvedValue([]);
    vi.spyOn(ApiClient.prototype, 'getPhoto').mockResolvedValue(makePhoto('deep-1'));

    const el = document.createElement('photo-tracker-app') as PhotoTrackerApp;
    document.body.appendChild(el);
    await new Promise((r) => setTimeout(r, 0));
    await el.updateComplete;

    expect(el.shadowRoot?.querySelector('photo-lightbox')).not.toBeNull();

    const mainEl = el.shadowRoot?.querySelector('main');
    mainEl?.dispatchEvent(
      new CustomEvent('filter-change', {
        detail: { min_views: 1 },
        bubbles: true,
        composed: true,
      }),
    );
    expect(window.location.search).toBe('?min_views=1');

    mainEl?.dispatchEvent(
      new CustomEvent('filter-nearby-gps', {
        detail: { lat: 10, lng: 20 },
        bubbles: true,
        composed: true,
      }),
    );
    await new Promise((r) => setTimeout(r, 0));

    vi.spyOn(ApiClient.prototype, 'triggerSync').mockRejectedValue(
      new ApiError(409, 'SYNC_IN_PROGRESS', 'Already syncing'),
    );
    const reimportBtn = Array.from(el.shadowRoot?.querySelectorAll('button') ?? []).find(
      (b) => b.textContent?.trim() === '↻ Re-import',
    );
    reimportBtn?.click();
    await new Promise((r) => setTimeout(r, 0));
    await el.updateComplete;
    expect(el.shadowRoot?.querySelector('[role="alert"]')?.textContent).toBe('Already syncing');

    window.history.replaceState(null, '', '/');
    window.dispatchEvent(new PopStateEvent('popstate'));
    await new Promise((r) => setTimeout(r, 0));
    await el.updateComplete;

    const galleryBtn = Array.from(el.shadowRoot?.querySelectorAll('button') ?? []).find(
      (b) => b.textContent?.trim() === 'Gallery',
    );
    galleryBtn?.click();
    await el.updateComplete;
    expect(window.location.pathname).toBe('/');
  });

  it('shows error banner if initial session check fails', async () => {
    vi.spyOn(ApiClient.prototype, 'getSession').mockRejectedValue(
      new ApiError(500, 'ERR', 'Server offline'),
    );
    const el = document.createElement('photo-tracker-app') as PhotoTrackerApp;
    document.body.appendChild(el);
    await new Promise((r) => setTimeout(r, 0));
    await el.updateComplete;
    expect(el.shadowRoot?.querySelector('[role="alert"]')?.textContent).toBe('Server offline');
  });
});

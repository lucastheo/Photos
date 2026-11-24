import { beforeEach, describe, expect, it, vi } from 'vitest';
import { ApiClient, ApiError } from '../api/client';
import type { LibraryStatsResponse, PhotoResponse, SyncReportResponse } from '../types/api';
import './library-view';
import type { PhotoLibraryView } from './library-view';

const baseStats: LibraryStatsResponse = {
  total_photos: 10,
  unseen_photos: 4,
  seen_photos: 6,
  total_views: 15,
  total_files: 12,
  duplicate_groups: 1,
  duplicate_files: 2,
  missing_photos: 1,
  missing_files: 1,
  by_format: [{ format: 'cr3', count: 5 }],
  by_folder: [{ folder_path: '2026', count: 10 }],
};

const idleReport: SyncReportResponse = {
  status: 'completed',
  scanned_count: 12,
  added_count: 1,
  updated_count: 0,
  moved_count: 1,
  stacked_count: 2,
  duplicate_count: 1,
  unchanged_count: 7,
  missing_count: 1,
  finished_at: '2026-11-20T12:00:00Z',
};

const missingPhoto: PhotoResponse = {
  id: 'm1',
  base_name: 'MISSING_01',
  folder_path: '2025',
  formats: ['jpg'],
  media_type: 'photo',
  width: 100,
  height: 100,
  view_count: 3,
  emoji_tags: [],
  files: [
    {
      id: 'mf1',
      relative_path: '2025/MISSING_01.jpg',
      file_name: 'MISSING_01.jpg',
      folder_path: '2025',
      content_hash: 'deadbeef',
      format: 'jpg',
      file_size: 1024,
      status: 'missing',
    },
  ],
  status: 'missing',
  thumbnail_url: '/t',
  preview_url: '/p',
  original_url: '/o',
};

describe('PhotoLibraryView', () => {
  beforeEach(() => {
    document.body.innerHTML = '';
    vi.restoreAllMocks();
  });

  it('renders coverage progress ring, sync counters, duplicates table, and missing purge', async () => {
    const api = new ApiClient();
    vi.spyOn(api, 'getLibraryStats').mockResolvedValue(baseStats);
    vi.spyOn(api, 'getSyncStatus')
      .mockResolvedValueOnce(idleReport)
      .mockResolvedValueOnce({ ...idleReport, status: 'completed', added_count: 2 })
      .mockResolvedValue(idleReport);
    vi.spyOn(api, 'listDuplicates').mockResolvedValue([
      {
        content_hash: 'abcdef1234567890abcdef',
        photo_id: 'p1',
        file_count: 2,
        files: [
          {
            id: 'f1',
            relative_path: 'a.jpg',
            file_name: 'a.jpg',
            folder_path: '',
            content_hash: 'abcdef1234567890abcdef',
            format: 'jpg',
            file_size: 2048,
            status: 'active',
          },
          {
            id: 'f2',
            relative_path: 'backup/a.jpg',
            file_name: 'a.jpg',
            folder_path: 'backup',
            content_hash: 'abcdef1234567890abcdef',
            format: 'jpg',
            file_size: 2048,
            status: 'active',
          },
        ],
      },
    ]);
    vi.spyOn(api, 'listMissing').mockResolvedValue([missingPhoto]);
    const triggerSpy = vi
      .spyOn(api, 'triggerSync')
      .mockResolvedValue({ ...idleReport, status: 'running' });
    const purgeSpy = vi.spyOn(api, 'purgeMissing').mockResolvedValue({ deleted_count: 1 });

    const el = document.createElement('photo-library-view') as PhotoLibraryView;
    el.api = api;
    el.pollIntervalMs = 1;
    document.body.appendChild(el);
    await new Promise((r) => setTimeout(r, 0));
    await el.updateComplete;

    const ring = el.shadowRoot?.querySelector('progress.progress-ring');
    expect(ring?.getAttribute('value')).toBe('60');
    expect(el.shadowRoot?.querySelector('.progress-ring-content')?.textContent).toBe('60%');
    expect(el.shadowRoot?.querySelectorAll('table')).toHaveLength(2);

    const syncedEvents: number[] = [];
    el.addEventListener('library-synced', () => syncedEvents.push(1));

    const btnByText = (txt: string) =>
      Array.from(el.shadowRoot?.querySelectorAll('button') ?? []).find(
        (b) => b.textContent?.trim() === txt,
      );

    btnByText('Re-import / Sync Library')?.click();
    await new Promise((r) => setTimeout(r, 15));
    await el.updateComplete;
    expect(triggerSpy).toHaveBeenCalledTimes(1);
    expect(syncedEvents.length).toBeGreaterThanOrEqual(1);

    btnByText('Purge Missing Records')?.click();
    await new Promise((r) => setTimeout(r, 10));
    await el.updateComplete;
    expect(purgeSpy).toHaveBeenCalledTimes(1);
    expect(el.shadowRoot?.querySelector('[role="status"]')?.textContent).toContain(
      'Purged 1 missing record(s).',
    );
  });

  it('displays error banner when library data load or sync fails', async () => {
    const api = new ApiClient();
    vi.spyOn(api, 'getLibraryStats').mockRejectedValue(new ApiError(503, 'UNAVAILABLE', 'DB offline'));
    vi.spyOn(api, 'getSyncStatus').mockResolvedValue(idleReport);
    vi.spyOn(api, 'listDuplicates').mockResolvedValue([]);
    vi.spyOn(api, 'listMissing').mockResolvedValue([]);
    vi.spyOn(api, 'triggerSync').mockRejectedValue(new ApiError(409, 'SYNC_IN_PROGRESS', 'Sync running'));
    vi.spyOn(api, 'purgeMissing').mockRejectedValue(new Error('purge boom'));

    const el = document.createElement('photo-library-view') as PhotoLibraryView;
    el.api = api;
    document.body.appendChild(el);
    await new Promise((r) => setTimeout(r, 0));
    await el.updateComplete;
    expect(el.shadowRoot?.querySelector('[role="alert"]')?.textContent).toBe('DB offline');

    await el.triggerReimport(false);
    await el.updateComplete;
    expect(el.shadowRoot?.querySelector('[role="alert"]')?.textContent).toBe('Sync running');

    await el.handlePurgeMissing();
    await el.updateComplete;
    expect(el.shadowRoot?.querySelector('[role="alert"]')?.textContent).toBe(
      'Failed to purge missing records.',
    );
  });

  it('renders empty states, zero total coverage, and sync error messages', async () => {
    const api = new ApiClient();
    vi.spyOn(api, 'getLibraryStats').mockResolvedValue({
      ...baseStats,
      total_photos: 0,
      seen_photos: 0,
      by_format: [],
    });
    vi.spyOn(api, 'getSyncStatus').mockResolvedValue({
      ...idleReport,
      status: 'failed',
      finished_at: undefined,
      error_message: 'Library path unreadable',
    });
    vi.spyOn(api, 'listDuplicates').mockResolvedValue([]);
    vi.spyOn(api, 'listMissing').mockResolvedValue([]);

    const el = document.createElement('photo-library-view') as PhotoLibraryView;
    el.api = api;
    document.body.appendChild(el);
    await new Promise((r) => setTimeout(r, 0));
    await el.updateComplete;

    expect(el.shadowRoot?.querySelector('.progress-ring-content')?.textContent).toBe('0%');
    expect(el.shadowRoot?.textContent).toContain('No exact SHA-256 duplicate files detected.');
    expect(el.shadowRoot?.textContent).toContain('No missing files recorded.');
    expect(el.shadowRoot?.textContent).toContain('Library path unreadable');
  });
});

import { describe, expect, it, vi } from 'vitest';
import { ApiClient, ApiError } from './client';

function mockJsonResponse(body: unknown, status = 200, statusText = 'OK'): Response {
  return new Response(JSON.stringify(body), {
    status,
    statusText,
    headers: { 'Content-Type': 'application/json' },
  });
}

describe('ApiClient', () => {
  it('calls auth session, login, and logout endpoints', async () => {
    const fetchMock = vi
      .fn<typeof fetch>()
      .mockResolvedValueOnce(mockJsonResponse({ authenticated: false, auth_disabled: false }))
      .mockResolvedValueOnce(mockJsonResponse({ authenticated: true, auth_disabled: false }))
      .mockResolvedValueOnce(mockJsonResponse({ authenticated: false, auth_disabled: false }));

    const client = new ApiClient('http://localhost:8080', fetchMock);
    expect(await client.getSession()).toEqual({ authenticated: false, auth_disabled: false });
    expect(await client.login('secret')).toEqual({ authenticated: true, auth_disabled: false });
    expect(await client.logout()).toEqual({ authenticated: false, auth_disabled: false });

    expect(fetchMock).toHaveBeenNthCalledWith(
      1,
      'http://localhost:8080/api/v1/auth/session',
      expect.objectContaining({ credentials: 'same-origin' }),
    );
    expect(fetchMock).toHaveBeenNthCalledWith(
      2,
      'http://localhost:8080/api/v1/auth/login',
      expect.objectContaining({ method: 'POST', body: JSON.stringify({ password: 'secret' }) }),
    );
    expect(fetchMock).toHaveBeenNthCalledWith(
      3,
      'http://localhost:8080/api/v1/auth/logout',
      expect.objectContaining({ method: 'POST' }),
    );
  });

  it('calls photo list, detail, view, and tag endpoints', async () => {
    const samplePhoto = { id: 'p1', view_count: 1, emoji_tags: ['❤️'] };
    const fetchMock = vi
      .fn<typeof fetch>()
      .mockResolvedValueOnce(mockJsonResponse({ items: [], total: 0, page: 1, page_size: 50 }))
      .mockResolvedValueOnce(mockJsonResponse({ items: [], total: 0, page: 1, page_size: 50 }))
      .mockResolvedValueOnce(mockJsonResponse(samplePhoto))
      .mockResolvedValueOnce(mockJsonResponse(samplePhoto))
      .mockResolvedValueOnce(mockJsonResponse(samplePhoto))
      .mockResolvedValueOnce(mockJsonResponse(samplePhoto))
      .mockResolvedValueOnce(mockJsonResponse(samplePhoto))
      .mockResolvedValueOnce(mockJsonResponse([{ emoji: '❤️', count: 1 }]));

    const client = new ApiClient('', fetchMock);
    await client.listPhotos();
    await client.listPhotos({ min_views: 0, max_views: 0, emoji_tags: ['❤️'] });
    await client.getPhoto('p1');
    await client.recordView('p1', { view_id: 'v1', source: 'dwell' });
    await client.adjustViewCount('p1', 'increment');
    await client.addTag('p1', '❤️');
    await client.removeTag('p1', '❤️');
    expect(await client.listTags()).toEqual([{ emoji: '❤️', count: 1 }]);

    expect(fetchMock).toHaveBeenNthCalledWith(1, '/api/v1/photos', expect.anything());
    expect(fetchMock).toHaveBeenNthCalledWith(
      2,
      '/api/v1/photos?min_views=0&max_views=0&emoji_tags=%E2%9D%A4%EF%B8%8F',
      expect.anything(),
    );
    expect(fetchMock).toHaveBeenNthCalledWith(
      7,
      '/api/v1/photos/p1/tags/%E2%9D%A4%EF%B8%8F',
      expect.objectContaining({ method: 'DELETE' }),
    );
  });

  it('calls library stats, duplicates, missing, purge, and sync endpoints', async () => {
    const fetchMock = vi
      .fn<typeof fetch>()
      .mockResolvedValueOnce(mockJsonResponse({ total_photos: 5 }))
      .mockResolvedValueOnce(mockJsonResponse([]))
      .mockResolvedValueOnce(mockJsonResponse([]))
      .mockResolvedValueOnce(mockJsonResponse({ deleted_count: 2 }))
      .mockResolvedValueOnce(mockJsonResponse({ status: 'completed' }))
      .mockResolvedValueOnce(mockJsonResponse({ status: 'running' }))
      .mockResolvedValueOnce(mockJsonResponse({ status: 'idle' }));

    const client = new ApiClient('', fetchMock);
    await client.getLibraryStats();
    await client.listDuplicates();
    await client.listMissing();
    expect(await client.purgeMissing()).toEqual({ deleted_count: 2 });
    await client.triggerSync(false);
    await client.triggerSync(true);
    await client.getSyncStatus();

    expect(fetchMock).toHaveBeenNthCalledWith(
      5,
      '/api/v1/library/sync',
      expect.objectContaining({ method: 'POST' }),
    );
    expect(fetchMock).toHaveBeenNthCalledWith(
      6,
      '/api/v1/library/sync?async=true',
      expect.objectContaining({ method: 'POST' }),
    );
  });

  it('throws ApiError with structured ProblemResponse or status fallback', async () => {
    const fetchMock = vi
      .fn<typeof fetch>()
      .mockResolvedValueOnce(
        mockJsonResponse({ code: 'PHOTO_NOT_FOUND', message: 'Not found', request_id: 'req_1' }, 404),
      )
      .mockResolvedValueOnce(new Response('bad gateway', { status: 502, statusText: 'Bad Gateway' }))
      .mockResolvedValueOnce(new Response('oops', { status: 500, statusText: '' }));

    const client = new ApiClient('', fetchMock);

    await expect(client.getPhoto('missing')).rejects.toMatchObject({
      name: 'ApiError',
      status: 404,
      code: 'PHOTO_NOT_FOUND',
      message: 'Not found',
      requestId: 'req_1',
    });

    await expect(client.getSession()).rejects.toMatchObject({
      status: 502,
      code: 'HTTP_502',
      message: 'Bad Gateway',
    });

    const err3 = await client.getSyncStatus().catch((e: unknown) => e);
    expect(err3).toBeInstanceOf(ApiError);
    expect((err3 as ApiError).message).toBe('Request failed');
  });
});

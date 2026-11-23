import type {
  AdjustViewRequest,
  DuplicateGroupDTO,
  EmojiTagCountDTO,
  LibraryStatsResponse,
  PaginatedPhotosResponse,
  PhotoFilterParams,
  PhotoResponse,
  ProblemResponse,
  PurgeMissingResponse,
  RecordViewRequest,
  SessionResponse,
  SyncReportResponse,
} from '../types/api';
import { filterToSearchParams } from '../utils/format';

export class ApiError extends Error {
  readonly status: number;
  readonly code: string;
  readonly requestId?: string;

  constructor(status: number, code: string, message: string, requestId?: string) {
    super(message);
    this.name = 'ApiError';
    this.status = status;
    this.code = code;
    this.requestId = requestId;
  }
}

export class ApiClient {
  private readonly baseUrl: string;
  private readonly fetchFn: typeof fetch;

  constructor(baseUrl = '', fetchFn: typeof fetch = globalThis.fetch.bind(globalThis)) {
    this.baseUrl = baseUrl;
    this.fetchFn = fetchFn;
  }

  private async request<T>(path: string, init?: RequestInit): Promise<T> {
    const res = await this.fetchFn(`${this.baseUrl}${path}`, {
      credentials: 'same-origin',
      ...init,
      headers: {
        ...(init?.body ? { 'Content-Type': 'application/json' } : {}),
        ...(init?.headers ?? {}),
      },
    });
    if (!res.ok) {
      let problem: Partial<ProblemResponse> = {};
      try {
        problem = (await res.json()) as Partial<ProblemResponse>;
      } catch {
        problem = {};
      }
      throw new ApiError(
        res.status,
        problem.code ?? `HTTP_${res.status}`,
        problem.message ?? (res.statusText || 'Request failed'),
        problem.request_id,
      );
    }
    return (await res.json()) as T;
  }

  getSession(): Promise<SessionResponse> {
    return this.request<SessionResponse>('/api/v1/auth/session');
  }

  login(password: string): Promise<SessionResponse> {
    return this.request<SessionResponse>('/api/v1/auth/login', {
      method: 'POST',
      body: JSON.stringify({ password }),
    });
  }

  logout(): Promise<SessionResponse> {
    return this.request<SessionResponse>('/api/v1/auth/logout', { method: 'POST' });
  }

  listPhotos(filter: PhotoFilterParams = {}): Promise<PaginatedPhotosResponse> {
    const qs = filterToSearchParams(filter).toString();
    return this.request<PaginatedPhotosResponse>(qs ? `/api/v1/photos?${qs}` : '/api/v1/photos');
  }

  getPhoto(id: string): Promise<PhotoResponse> {
    return this.request<PhotoResponse>(`/api/v1/photos/${encodeURIComponent(id)}`);
  }

  recordView(id: string, req: RecordViewRequest): Promise<PhotoResponse> {
    return this.request<PhotoResponse>(`/api/v1/photos/${encodeURIComponent(id)}/views`, {
      method: 'POST',
      body: JSON.stringify(req),
    });
  }

  adjustViewCount(id: string, action: AdjustViewRequest['action']): Promise<PhotoResponse> {
    return this.request<PhotoResponse>(`/api/v1/photos/${encodeURIComponent(id)}/views`, {
      method: 'PATCH',
      body: JSON.stringify({ action }),
    });
  }

  addTag(id: string, emoji: string): Promise<PhotoResponse> {
    return this.request<PhotoResponse>(`/api/v1/photos/${encodeURIComponent(id)}/tags`, {
      method: 'POST',
      body: JSON.stringify({ emoji }),
    });
  }

  removeTag(id: string, emoji: string): Promise<PhotoResponse> {
    return this.request<PhotoResponse>(
      `/api/v1/photos/${encodeURIComponent(id)}/tags/${encodeURIComponent(emoji)}`,
      { method: 'DELETE' },
    );
  }

  listTags(): Promise<EmojiTagCountDTO[]> {
    return this.request<EmojiTagCountDTO[]>('/api/v1/tags');
  }

  getLibraryStats(): Promise<LibraryStatsResponse> {
    return this.request<LibraryStatsResponse>('/api/v1/library/stats');
  }

  listDuplicates(): Promise<DuplicateGroupDTO[]> {
    return this.request<DuplicateGroupDTO[]>('/api/v1/library/duplicates');
  }

  listMissing(): Promise<PhotoResponse[]> {
    return this.request<PhotoResponse[]>('/api/v1/library/missing');
  }

  purgeMissing(): Promise<PurgeMissingResponse> {
    return this.request<PurgeMissingResponse>('/api/v1/library/missing', { method: 'DELETE' });
  }

  triggerSync(asyncMode = false): Promise<SyncReportResponse> {
    const suffix = asyncMode ? '?async=true' : '';
    return this.request<SyncReportResponse>(`/api/v1/library/sync${suffix}`, { method: 'POST' });
  }

  getSyncStatus(): Promise<SyncReportResponse> {
    return this.request<SyncReportResponse>('/api/v1/library/sync/status');
  }
}

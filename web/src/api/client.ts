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

const API_PREFIX = '/api/v1';
const LOGIN_PATH = `${API_PREFIX}/auth/login`;

export class ApiClient {
  /** Invoked when a non-login request is rejected with 401 (expired session). */
  onUnauthorized?: () => void;

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
      if (res.status === 401 && path !== LOGIN_PATH) {
        this.onUnauthorized?.();
      }
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
    return this.request<SessionResponse>(`${API_PREFIX}/auth/session`);
  }

  login(password: string): Promise<SessionResponse> {
    return this.request<SessionResponse>(`${API_PREFIX}/auth/login`, {
      method: 'POST',
      body: JSON.stringify({ password }),
    });
  }

  logout(): Promise<SessionResponse> {
    return this.request<SessionResponse>(`${API_PREFIX}/auth/logout`, { method: 'POST' });
  }

  listPhotos(filter: PhotoFilterParams = {}): Promise<PaginatedPhotosResponse> {
    const qs = filterToSearchParams(filter).toString();
    return this.request<PaginatedPhotosResponse>(qs ? `${API_PREFIX}/photos?${qs}` : `${API_PREFIX}/photos`);
  }

  getPhoto(id: string): Promise<PhotoResponse> {
    return this.request<PhotoResponse>(`${API_PREFIX}/photos/${encodeURIComponent(id)}`);
  }

  recordView(id: string, req: RecordViewRequest): Promise<PhotoResponse> {
    return this.request<PhotoResponse>(`${API_PREFIX}/photos/${encodeURIComponent(id)}/views`, {
      method: 'POST',
      body: JSON.stringify(req),
    });
  }

  adjustViewCount(id: string, action: AdjustViewRequest['action']): Promise<PhotoResponse> {
    return this.request<PhotoResponse>(`${API_PREFIX}/photos/${encodeURIComponent(id)}/views`, {
      method: 'PATCH',
      body: JSON.stringify({ action }),
    });
  }

  addTag(id: string, emoji: string): Promise<PhotoResponse> {
    return this.request<PhotoResponse>(`${API_PREFIX}/photos/${encodeURIComponent(id)}/tags`, {
      method: 'POST',
      body: JSON.stringify({ emoji }),
    });
  }

  removeTag(id: string, emoji: string): Promise<PhotoResponse> {
    return this.request<PhotoResponse>(
      `${API_PREFIX}/photos/${encodeURIComponent(id)}/tags/${encodeURIComponent(emoji)}`,
      { method: 'DELETE' },
    );
  }

  listTags(): Promise<EmojiTagCountDTO[]> {
    return this.request<EmojiTagCountDTO[]>(`${API_PREFIX}/tags`);
  }

  getLibraryStats(): Promise<LibraryStatsResponse> {
    return this.request<LibraryStatsResponse>(`${API_PREFIX}/library/stats`);
  }

  listDuplicates(): Promise<DuplicateGroupDTO[]> {
    return this.request<DuplicateGroupDTO[]>(`${API_PREFIX}/library/duplicates`);
  }

  listMissing(): Promise<PhotoResponse[]> {
    return this.request<PhotoResponse[]>(`${API_PREFIX}/library/missing`);
  }

  purgeMissing(): Promise<PurgeMissingResponse> {
    return this.request<PurgeMissingResponse>(`${API_PREFIX}/library/missing`, { method: 'DELETE' });
  }

  triggerSync(asyncMode = false): Promise<SyncReportResponse> {
    const suffix = asyncMode ? '?async=true' : '';
    return this.request<SyncReportResponse>(`${API_PREFIX}/library/sync${suffix}`, { method: 'POST' });
  }

  getSyncStatus(): Promise<SyncReportResponse> {
    return this.request<SyncReportResponse>(`${API_PREFIX}/library/sync/status`);
  }
}

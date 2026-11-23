import type {
  PhotoFilterParams,
  PhotoResponse,
  SortOption,
  ViewCountPreset,
} from '../types/api';

export const DEFAULT_PAGE_SIZE = 50;

export const DEFAULT_PHOTO_FILTER: Readonly<PhotoFilterParams> = {
  page: 1,
  page_size: DEFAULT_PAGE_SIZE,
  sort_by: 'views',
  sort_order: 'asc',
};

function appendNumericParams(params: URLSearchParams, filter: PhotoFilterParams): void {
  const numericEntries: Array<[string, number | undefined]> = [
    ['min_views', filter.min_views],
    ['max_views', filter.max_views],
    ['exact_views', filter.exact_views],
    ['min_lat', filter.min_lat],
    ['max_lat', filter.max_lat],
    ['min_lng', filter.min_lng],
    ['max_lng', filter.max_lng],
  ];
  for (const [key, val] of numericEntries) {
    if (val !== undefined && !Number.isNaN(val)) {
      params.set(key, String(val));
    }
  }
}

export function filterToSearchParams(filter: PhotoFilterParams): URLSearchParams {
  const params = new URLSearchParams();
  appendNumericParams(params, filter);
  if (filter.emoji_tags?.length) params.set('emoji_tags', filter.emoji_tags.join(','));
  if (filter.untagged) params.set('untagged', 'true');
  if (filter.formats?.length) params.set('formats', filter.formats.join(','));
  if (filter.media_type) params.set('media_type', filter.media_type);
  if (filter.folder) params.set('folder', filter.folder);
  if (filter.has_gps !== undefined) params.set('has_gps', String(filter.has_gps));
  if (filter.from_date) params.set('from_date', filter.from_date);
  if (filter.to_date) params.set('to_date', filter.to_date);
  if (filter.q?.trim()) params.set('q', filter.q.trim());
  if (filter.sort_by) params.set('sort_by', filter.sort_by);
  if (filter.sort_order) params.set('sort_order', filter.sort_order);
  if (filter.sort_by === 'random' && filter.seed !== undefined) {
    params.set('seed', String(filter.seed));
  }
  if (filter.page && filter.page > 1) params.set('page', String(filter.page));
  if (filter.page_size && filter.page_size !== DEFAULT_PAGE_SIZE) {
    params.set('page_size', String(filter.page_size));
  }
  return params;
}

function parseOptionalNumber(raw: string | null): number | undefined {
  if (!raw || raw.trim() === '') return undefined;
  const parsed = Number(raw);
  return Number.isFinite(parsed) ? parsed : undefined;
}

function parseCSVParam(raw: string | null): string[] | undefined {
  if (!raw) return undefined;
  const items = raw.split(',').map((s) => s.trim()).filter(Boolean);
  return items.length ? items : undefined;
}

export function searchParamsToFilter(params: URLSearchParams): PhotoFilterParams {
  const filter: PhotoFilterParams = {};
  const setNum = (k: keyof PhotoFilterParams, v: number | undefined) => {
    if (v !== undefined) (filter as Record<string, unknown>)[k] = v;
  };
  setNum('min_views', parseOptionalNumber(params.get('min_views')));
  setNum('max_views', parseOptionalNumber(params.get('max_views')));
  setNum('exact_views', parseOptionalNumber(params.get('exact_views')));
  setNum('min_lat', parseOptionalNumber(params.get('min_lat')));
  setNum('max_lat', parseOptionalNumber(params.get('max_lat')));
  setNum('min_lng', parseOptionalNumber(params.get('min_lng')));
  setNum('max_lng', parseOptionalNumber(params.get('max_lng')));
  setNum('seed', parseOptionalNumber(params.get('seed')));
  setNum('page', parseOptionalNumber(params.get('page')));
  setNum('page_size', parseOptionalNumber(params.get('page_size')));

  const tags = parseCSVParam(params.get('emoji_tags'));
  if (tags) filter.emoji_tags = tags;
  if (params.get('untagged') === 'true') filter.untagged = true;
  const formats = parseCSVParam(params.get('formats'));
  if (formats) filter.formats = formats;

  const mediaType = params.get('media_type');
  if (mediaType === 'photo' || mediaType === 'video') filter.media_type = mediaType;
  const folder = params.get('folder');
  if (folder) filter.folder = folder;
  const hasGps = params.get('has_gps');
  if (hasGps === 'true') filter.has_gps = true;
  if (hasGps === 'false') filter.has_gps = false;
  const fromDate = params.get('from_date');
  if (fromDate) filter.from_date = fromDate;
  const toDate = params.get('to_date');
  if (toDate) filter.to_date = toDate;
  const q = params.get('q');
  if (q) filter.q = q;
  const sortBy = params.get('sort_by');
  if (
    sortBy === 'views' ||
    sortBy === 'date_taken' ||
    sortBy === 'last_viewed' ||
    sortBy === 'random'
  ) {
    filter.sort_by = sortBy;
  }
  const sortOrder = params.get('sort_order');
  if (sortOrder === 'asc' || sortOrder === 'desc') {
    filter.sort_order = sortOrder;
  }
  return filter;
}

export function applyViewPreset(
  filter: PhotoFilterParams,
  preset: ViewCountPreset,
): PhotoFilterParams {
  const next: PhotoFilterParams = { ...filter, page: 1 };
  delete next.exact_views;
  if (preset === 'all') {
    delete next.min_views;
    delete next.max_views;
  } else if (preset === 'unseen') {
    next.min_views = 0;
    next.max_views = 0;
  } else if (preset === 'seen') {
    next.min_views = 1;
    delete next.max_views;
  }
  return next;
}

export function detectViewPreset(filter: PhotoFilterParams): ViewCountPreset {
  if (filter.exact_views !== undefined) return 'custom';
  if (filter.min_views === undefined && filter.max_views === undefined) return 'all';
  if (filter.min_views === 0 && filter.max_views === 0) return 'unseen';
  if (filter.min_views === 1 && filter.max_views === undefined) return 'seen';
  return 'custom';
}

export function applySortOption(
  filter: PhotoFilterParams,
  option: SortOption,
  seed = 42,
): PhotoFilterParams {
  const next: PhotoFilterParams = { ...filter, page: 1 };
  delete next.seed;
  if (option === 'views_asc') {
    next.sort_by = 'views';
    next.sort_order = 'asc';
  } else if (option === 'views_desc') {
    next.sort_by = 'views';
    next.sort_order = 'desc';
  } else if (option === 'date_desc') {
    next.sort_by = 'date_taken';
    next.sort_order = 'desc';
  } else if (option === 'date_asc') {
    next.sort_by = 'date_taken';
    next.sort_order = 'asc';
  } else if (option === 'last_viewed_desc') {
    next.sort_by = 'last_viewed';
    next.sort_order = 'desc';
  } else if (option === 'last_viewed_asc') {
    next.sort_by = 'last_viewed';
    next.sort_order = 'asc';
  } else {
    next.sort_by = 'random';
    next.sort_order = 'asc';
    next.seed = seed;
  }
  return next;
}

export function detectSortOption(filter: PhotoFilterParams): SortOption {
  const by = filter.sort_by ?? 'views';
  const order = filter.sort_order ?? 'asc';
  if (by === 'random') return 'random';
  if (by === 'date_taken') return order === 'asc' ? 'date_asc' : 'date_desc';
  if (by === 'last_viewed') return order === 'asc' ? 'last_viewed_asc' : 'last_viewed_desc';
  return order === 'desc' ? 'views_desc' : 'views_asc';
}

export function toggleEmojiTagFilter(
  filter: PhotoFilterParams,
  emoji: string,
): PhotoFilterParams {
  const current = filter.emoji_tags ?? [];
  const exists = current.includes(emoji);
  const emoji_tags = exists ? current.filter((t) => t !== emoji) : [...current, emoji];
  return {
    ...filter,
    untagged: false,
    emoji_tags,
    page: 1,
  };
}

export function toggleUntaggedFilter(filter: PhotoFilterParams): PhotoFilterParams {
  return {
    ...filter,
    untagged: !filter.untagged,
    emoji_tags: [],
    page: 1,
  };
}

export function toggleFormatFilter(
  filter: PhotoFilterParams,
  format: string,
): PhotoFilterParams {
  const lower = format.toLowerCase();
  const current = filter.formats ?? [];
  const exists = current.includes(lower);
  const formats = exists ? current.filter((f) => f !== lower) : [...current, lower];
  return {
    ...filter,
    formats,
    page: 1,
  };
}

export function formatDuration(seconds?: number): string {
  if (!seconds || seconds <= 0) return '';
  const rounded = Math.round(seconds);
  const mins = Math.floor(rounded / 60);
  const secs = rounded % 60;
  return `${String(mins).padStart(2, '0')}:${String(secs).padStart(2, '0')}`;
}

export function formatFileSize(bytes: number): string {
  if (bytes < 1024) return `${bytes} B`;
  if (bytes < 1024 * 1024) return `${(bytes / 1024).toFixed(1)} KB`;
  if (bytes < 1024 * 1024 * 1024) return `${(bytes / (1024 * 1024)).toFixed(1)} MB`;
  return `${(bytes / (1024 * 1024 * 1024)).toFixed(2)} GB`;
}

export function formatStackBadge(
  photo: Pick<PhotoResponse, 'formats' | 'media_type' | 'duration_sec'>,
): string {
  const formats = photo.formats.map((f) => f.toUpperCase()).join(' + ');
  if (photo.media_type === 'video' && photo.duration_sec && photo.duration_sec > 0) {
    return `${formats} ${formatDuration(photo.duration_sec)}`;
  }
  return formats;
}

export function formatDateTime(iso?: string | null): string {
  if (!iso) return 'Unknown';
  const parsed = new Date(iso);
  if (Number.isNaN(parsed.getTime())) return iso;
  return parsed.toLocaleString('en-US', {
    month: 'short',
    day: 'numeric',
    year: 'numeric',
    hour: '2-digit',
    minute: '2-digit',
  });
}

export function buildNearbyGPSFilter(
  lat: number,
  lng: number,
  delta = 0.05,
): Partial<PhotoFilterParams> {
  const round = (v: number) => Math.round(v * 10000) / 10000;
  return {
    has_gps: true,
    min_lat: round(Math.max(-90, lat - delta)),
    max_lat: round(Math.min(90, lat + delta)),
    min_lng: round(Math.max(-180, lng - delta)),
    max_lng: round(Math.min(180, lng + delta)),
    page: 1,
  };
}

export function buildOpenStreetMapUrl(lat: number, lng: number): string {
  return `https://www.openstreetmap.org/?mlat=${lat}&mlon=${lng}#map=14/${lat}/${lng}`;
}

export function runViewTransition(
  update: () => void,
  direction?: 'forward' | 'backward',
): void {
  const reducedMotion =
    typeof window !== 'undefined' &&
    typeof window.matchMedia === 'function' &&
    window.matchMedia('(prefers-reduced-motion: reduce)').matches;
  const startTransition =
    typeof document !== 'undefined'
      ? (document as Document & {
          startViewTransition?: (
            arg: (() => void) | { update: () => void; types?: string[] },
          ) => unknown;
        }).startViewTransition
      : undefined;
  if (!startTransition || reducedMotion) {
    update();
    return;
  }
  if (direction) {
    try {
      startTransition.call(document, { update, types: [direction] });
      return;
    } catch {
      startTransition.call(document, update);
      return;
    }
  }
  startTransition.call(document, update);
}

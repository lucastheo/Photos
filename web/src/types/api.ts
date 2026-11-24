export interface ProblemResponse {
  code: string;
  message: string;
  request_id?: string;
}

export interface LoginRequest {
  password: string;
}

export interface SessionResponse {
  authenticated: boolean;
  auth_disabled: boolean;
}

export interface PhotoFileDTO {
  id: string;
  relative_path: string;
  file_name: string;
  folder_path: string;
  content_hash: string;
  format: string;
  file_size: number;
  status: string;
}

export interface EXIFMetadataDTO {
  orientation: number;
  camera_make?: string;
  camera_model?: string;
  lens_model?: string;
  iso?: number;
  f_number?: number;
  exposure_time?: string;
  focal_length_mm?: number;
  gps_latitude?: number;
  gps_longitude?: number;
}

export interface ViewEventDTO {
  id: number;
  view_id?: string;
  photo_id: string;
  viewed_at: string;
  source: string;
}

export interface PhotoResponse {
  id: string;
  base_name: string;
  folder_path: string;
  formats: string[];
  media_type: 'photo' | 'video' | string;
  width: number;
  height: number;
  duration_sec?: number;
  taken_at?: string;
  gps_latitude?: number;
  gps_longitude?: number;
  exif?: EXIFMetadataDTO;
  view_count: number;
  emoji_tags: string[];
  files: PhotoFileDTO[];
  view_history?: ViewEventDTO[];
  last_viewed_at?: string;
  status: string;
  thumbnail_url: string;
  preview_url: string;
  original_url: string;
}

export interface PaginatedPhotosResponse {
  items: PhotoResponse[];
  total: number;
  page: number;
  page_size: number;
}

export interface RecordViewRequest {
  view_id: string;
  source: 'dwell' | 'manual' | 'slideshow';
}

export interface AdjustViewRequest {
  action: 'increment' | 'decrement' | 'reset';
}

export interface AddTagRequest {
  emoji: string;
}

export interface EmojiTagCountDTO {
  emoji: string;
  count: number;
}

export interface SyncReportResponse {
  id?: string;
  started_at?: string;
  finished_at?: string;
  status: string;
  scanned_count: number;
  added_count: number;
  updated_count: number;
  moved_count: number;
  stacked_count: number;
  duplicate_count: number;
  unchanged_count: number;
  missing_count: number;
  error_message?: string;
}

export interface DuplicateGroupDTO {
  content_hash: string;
  photo_id: string;
  file_count: number;
  files: PhotoFileDTO[];
}

export interface FormatStatDTO {
  format: string;
  count: number;
}

export interface FolderStatDTO {
  folder_path: string;
  count: number;
}

export interface LibraryStatsResponse {
  total_photos: number;
  unseen_photos: number;
  seen_photos: number;
  total_views: number;
  total_files: number;
  duplicate_groups: number;
  duplicate_files: number;
  missing_photos: number;
  missing_files: number;
  by_format: FormatStatDTO[];
  by_folder: FolderStatDTO[];
}

export interface PurgeMissingResponse {
  deleted_count: number;
}

export type ViewCountPreset = 'all' | 'unseen' | 'seen' | 'custom';

export type SortOption =
  | 'views_asc'
  | 'views_desc'
  | 'date_desc'
  | 'date_asc'
  | 'last_viewed_desc'
  | 'last_viewed_asc'
  | 'random';

export interface PhotoFilterParams {
  min_views?: number;
  max_views?: number;
  exact_views?: number;
  emoji_tags?: string[];
  untagged?: boolean;
  formats?: string[];
  media_type?: '' | 'photo' | 'video';
  folder?: string;
  has_gps?: boolean;
  min_lat?: number;
  max_lat?: number;
  min_lng?: number;
  max_lng?: number;
  from_date?: string;
  to_date?: string;
  q?: string;
  sort_by?: 'views' | 'date_taken' | 'last_viewed' | 'random';
  sort_order?: 'asc' | 'desc';
  seed?: number;
  page?: number;
  page_size?: number;
}

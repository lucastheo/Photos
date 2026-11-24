import { LitElement, css, html, nothing } from 'lit';
import { customElement, property, state } from 'lit/decorators.js';
import { ApiClient, ApiError } from '../api/client';
import '../components/gps-map-picker';
import type { GpsBoundsEventDetail } from '../components/gps-map-picker';
import { sharedStyles } from '../styles/shared';
import type {
  EmojiTagCountDTO,
  FolderStatDTO,
  PhotoFilterParams,
  PhotoResponse,
  SortOption,
  ViewCountPreset,
} from '../types/api';
import {
  applySortOption,
  applyViewPreset,
  buildNearbyGPSFilter,
  DEFAULT_PAGE_SIZE,
  DEFAULT_PHOTO_FILTER,
  detectSortOption,
  detectViewPreset,
  formatStackBadge,
  toggleEmojiTagFilter,
  toggleFormatFilter,
  toggleUntaggedFilter,
} from '../utils/format';

const COMMON_FORMATS = ['cr3', 'heic', 'jpg', 'png', 'webp', 'mp4', 'mov'];
const MEDIA_TYPE_LABELS: Record<'' | 'photo' | 'video', string> = {
  '': 'All Media',
  photo: 'Photos',
  video: 'Videos',
};
const FILTERS_HIDDEN_STORAGE_KEY = 'photos.gallery.filtersHidden';

function readFiltersHidden(): boolean {
  try {
    return globalThis.localStorage?.getItem(FILTERS_HIDDEN_STORAGE_KEY) === 'true';
  } catch {
    return false;
  }
}

/** Cards at or after this index get content-visibility deferral. */
const DEFERRED_CARD_START_INDEX = 12;
/** Leading cards loaded eagerly with high fetch priority. */
const LCP_CANDIDATE_COUNT = 2;

@customElement('photo-gallery-view')
export class PhotoGalleryView extends LitElement {
  static override styles = [
    sharedStyles,
    css`
      :host {
        display: grid;
        gap: 1rem;
      }

      .filter-panel {
        display: grid;
        gap: 0.75rem;
        padding-block-end: 0.85rem;
        border-bottom: 1px solid var(--border-color, #cbd5e1);
      }

      .filter-controls {
        display: grid;
        gap: 0.75rem;
      }

      .filter-controls[hidden] {
        display: none;
      }

      .filter-toggle-actions {
        display: flex;
        align-items: center;
        gap: 0.45rem;
        margin-inline-start: auto;
      }

      .filter-toggle {
        inline-size: 8.75rem;
        justify-content: center;
      }

      .filter-row {
        display: flex;
        flex-wrap: wrap;
        align-items: center;
        gap: 0.45rem;
      }

      .filter-row label {
        font-size: 0.8125rem;
        color: var(--text-muted, #475569);
      }

      .filter-input,
      .filter-select {
        padding: 0.35rem 0.55rem;
        border-radius: 0.45rem;
        border: 1px solid var(--border-color, #cbd5e1);
        background: var(--bg-canvas, #f8fafc);
        font-size: 0.85rem;
      }

      .num-input {
        inline-size: 4.75rem;
      }

      .date-input {
        inline-size: 9.75rem;
        block-size: 2.15rem;
      }

      .popover-panel {
        inset: 0;
        margin: auto;
        padding: 1rem;
        border-radius: 0.75rem;
        border: 1px solid var(--border-color, #cbd5e1);
        background: var(--bg-surface, #ffffff);
        color: var(--text-primary, inherit);
        inline-size: 80vw;
        max-inline-size: 80vw;
        block-size: 80vh;
        max-block-size: 80vh;
        box-shadow: 0 20px 25px -5px rgb(0 0 0 / 0.2);
      }

      .popover-panel::backdrop {
        background: rgb(9 9 11 / 0.55);
      }

      .popover-header {
        display: flex;
        align-items: center;
        justify-content: space-between;
        font-weight: 600;
        font-size: 0.9rem;
      }

      [popover]:is(:popover-open, .\:popover-open) {
        display: block;
      }

      [popover].popover-panel:is(:popover-open, .\:popover-open) {
        display: grid;
        grid-template-rows: auto 1fr auto;
        gap: 0.6rem;
      }

      .gps-bounds-grid {
        display: grid;
        grid-template-columns: repeat(auto-fit, minmax(8rem, 1fr));
        gap: 0.5rem;
      }

      .photo-grid {
        display: grid;
        grid-template-columns: repeat(auto-fill, minmax(220px, 1fr));
        gap: 0.85rem;
      }

      .photo-card {
        display: grid;
        grid-template-rows: auto 1fr;
        border-radius: 0.75rem;
        border: 1px solid var(--border-color, #cbd5e1);
        background: var(--bg-surface, #ffffff);
        overflow: hidden;
        padding: 0;
        text-align: start;
        cursor: pointer;
      }

      .photo-card.deferred-card {
        content-visibility: auto;
        contain-intrinsic-size: auto none auto 240px;
      }

      .thumb-wrap {
        position: relative;
        aspect-ratio: 4 / 3;
        inline-size: 100%;
        background: #09090b;
      }

      .thumb-wrap img {
        display: block;
        inline-size: 100%;
        block-size: 100%;
        object-fit: contain;
      }

      .badge {
        position: absolute;
        padding: 0.18rem 0.45rem;
        border-radius: 999px;
        font-size: 0.75rem;
        font-weight: 600;
        background: rgb(9 9 11 / 0.82);
        color: #fafafa;
      }

      .badge-views {
        inset-block-start: 0.45rem;
        inset-inline-end: 0.45rem;
      }

      .badge-views.unseen {
        background: var(--unseen, #d97706);
        color: #111827;
      }

      .badge-format {
        inset-block-end: 0.45rem;
        inset-inline-start: 0.45rem;
      }

      .badge-gps {
        inset-block-start: 0.45rem;
        inset-inline-start: 0.45rem;
      }

      .card-meta {
        padding: 0.55rem 0.7rem;
        display: grid;
        gap: 0.2rem;
      }

      .card-title {
        font-weight: 600;
        font-size: 0.875rem;
        overflow: hidden;
        text-overflow: ellipsis;
        white-space: nowrap;
      }

      .pagination {
        display: flex;
        align-items: center;
        justify-content: space-between;
        flex-wrap: wrap;
        gap: 0.75rem;
      }
    `,
  ];

  @property({ attribute: false })
  api = new ApiClient();

  @property({ attribute: false })
  filter: PhotoFilterParams = { ...DEFAULT_PHOTO_FILTER };

  @property({ attribute: false })
  folders: FolderStatDTO[] = [];

  @property({ type: Number })
  libraryTotal?: number;

  @state()
  items: PhotoResponse[] = [];

  @state()
  total = 0;

  @state()
  tags: EmojiTagCountDTO[] = [];

  @state()
  private loading = false;

  @state()
  private errorMessage = '';

  @state()
  filtersHidden = readFiltersHidden();

  @state()
  private focusedDateField: 'from' | 'to' | null = null;

  private loadSeq = 0;

  override connectedCallback(): void {
    super.connectedCallback();
    void this.loadGallery();
  }

  async loadGallery(): Promise<void> {
    const seq = ++this.loadSeq;
    this.loading = true;
    this.errorMessage = '';
    try {
      const [pageRes, tagList] = await Promise.all([
        this.api.listPhotos(this.filter),
        this.api.listTags(),
      ]);
      if (seq !== this.loadSeq) return;
      this.items = pageRes.items ?? [];
      this.total = pageRes.total;
      this.tags = tagList ?? [];
      this.dispatchEvent(
        new CustomEvent<PhotoResponse[]>('gallery-loaded', {
          detail: this.items,
          bubbles: true,
          composed: true,
        }),
      );
    } catch (err) {
      if (seq !== this.loadSeq) return;
      this.errorMessage = err instanceof ApiError ? err.message : 'Failed to load gallery.';
    } finally {
      if (seq === this.loadSeq) {
        this.loading = false;
      }
    }
  }

  updatePhotoInPlace(updated: PhotoResponse): void {
    const idx = this.items.findIndex((p) => p.id === updated.id);
    if (idx === -1) return;
    const next = [...this.items];
    next[idx] = updated;
    this.items = next;
    void this.refreshTagsOnly();
  }

  async applyNearbyGPS(lat: number, lng: number): Promise<void> {
    await this.updateFilter({ ...this.filter, ...buildNearbyGPSFilter(lat, lng) });
  }

  private async refreshTagsOnly(): Promise<void> {
    try {
      this.tags = await this.api.listTags();
    } catch {
      // Tag refresh failure does not disturb active browsing session.
    }
  }

  private async updateFilter(next: PhotoFilterParams): Promise<void> {
    this.filter = next;
    this.dispatchEvent(
      new CustomEvent<PhotoFilterParams>('filter-change', {
        detail: next,
        bubbles: true,
        composed: true,
      }),
    );
    await this.loadGallery();
  }

  private setPreset(preset: ViewCountPreset): void {
    void this.updateFilter(applyViewPreset(this.filter, preset));
  }

  private setCustomViewBound(field: 'min_views' | 'max_views', raw: string): void {
    const next: PhotoFilterParams = { ...this.filter, page: 1 };
    delete next.exact_views;
    const parsed = Number(raw);
    if (raw.trim() === '' || !Number.isFinite(parsed)) {
      delete next[field];
    } else {
      next[field] = Math.max(0, parsed);
    }
    void this.updateFilter(next);
  }

  private setGpsBound(field: 'min_lat' | 'max_lat' | 'min_lng' | 'max_lng', raw: string): void {
    const next: PhotoFilterParams = { ...this.filter, page: 1 };
    const parsed = Number(raw);
    if (raw.trim() === '' || !Number.isFinite(parsed)) {
      delete next[field];
    } else {
      next[field] = parsed;
    }
    void this.updateFilter(next);
  }

  private handleGpsBoundsChange(e: CustomEvent<GpsBoundsEventDetail>): void {
    const { min_lat, max_lat, min_lng, max_lng } = e.detail;
    const next: PhotoFilterParams = { ...this.filter, page: 1 };
    const fields = [
      ['min_lat', min_lat],
      ['max_lat', max_lat],
      ['min_lng', min_lng],
      ['max_lng', max_lng],
    ] as const;
    for (const [key, val] of fields) {
      if (val === undefined) {
        delete next[key];
      } else {
        next[key] = val;
      }
    }
    if (min_lat !== undefined && max_lat !== undefined && min_lng !== undefined && max_lng !== undefined) {
      next.has_gps = true;
    } else if (min_lat === undefined && max_lat === undefined && min_lng === undefined && max_lng === undefined) {
      delete next.has_gps;
    }
    void this.updateFilter(next);
  }

  private handleSortChange(e: Event): void {
    const option = (e.target as HTMLSelectElement).value as SortOption;
    const seed = this.filter.seed ?? Math.floor(Date.now() % 1000000);
    void this.updateFilter(applySortOption(this.filter, option, seed));
  }

  private reshuffleSeed(): void {
    const seed = ((this.filter.seed ?? 1) * 1103515245 + 12345) % 1000000;
    void this.updateFilter({ ...this.filter, sort_by: 'random', seed: Math.abs(seed) || 7, page: 1 });
  }

  private resetFilters(): void {
    void this.updateFilter({ ...DEFAULT_PHOTO_FILTER });
  }

  private toggleFilters(): void {
    this.filtersHidden = !this.filtersHidden;
    try {
      globalThis.localStorage?.setItem(FILTERS_HIDDEN_STORAGE_KEY, String(this.filtersHidden));
    } catch {
      // Storage unavailable (private mode / quota); keep in-memory state only.
    }
  }

  private selectCard(photo: PhotoResponse, index: number): void {
    this.dispatchEvent(
      new CustomEvent<{ photo: PhotoResponse; index: number }>('photo-select', {
        detail: { photo, index },
        bubbles: true,
        composed: true,
      }),
    );
  }

  private renderFilterToggle() {
    return html`
      <div class="filter-row filter-toggle-row">
        ${this.renderSortControls()}
        <div class="filter-toggle-actions">
          <button type="button" class="btn" @click=${this.resetFilters}>Reset Filters</button>
          <button
            type="button"
            class="btn filter-toggle"
            aria-expanded=${this.filtersHidden ? 'false' : 'true'}
            aria-controls="filter-controls"
            @click=${this.toggleFilters}
          >
            ${this.filtersHidden ? '▸ Show Filters' : '▾ Hide Filters'}
          </button>
        </div>
      </div>
    `;
  }

  private renderViewPresets() {
    const activePreset = detectViewPreset(this.filter);
    const presets: Array<[ViewCountPreset, string]> = [
      ['all', 'All'],
      ['unseen', 'Never Seen (0)'],
      ['seen', 'Seen (1+)'],
    ];
    return html`
      <div class="filter-row">
        <label>Views:</label>
        ${presets.map(
          ([key, label]) => html`
            <button
              type="button"
              class="chip"
              aria-pressed=${activePreset === key ? 'true' : 'false'}
              @click=${() => this.setPreset(key)}
            >
              ${label}
            </button>
          `,
        )}
        <input
          type="number"
          min="0"
          class="filter-input num-input"
          aria-label="Min views"
          placeholder="Min"
          .value=${this.filter.min_views !== undefined ? String(this.filter.min_views) : ''}
          @change=${(e: Event) => this.setCustomViewBound('min_views', (e.target as HTMLInputElement).value)}
        />
        <input
          type="number"
          min="0"
          class="filter-input num-input"
          aria-label="Max views"
          placeholder="Max"
          .value=${this.filter.max_views !== undefined ? String(this.filter.max_views) : ''}
          @change=${(e: Event) => this.setCustomViewBound('max_views', (e.target as HTMLInputElement).value)}
        />
      </div>
    `;
  }

  private renderEmojiAndFormatRows() {
    const selectedTags = this.filter.emoji_tags ?? [];
    const selectedFormats = this.filter.formats ?? [];
    return html`
      <div class="filter-row">
        <label>Emoji Tags:</label>
        <button
          type="button"
          class="chip"
          aria-pressed=${this.filter.untagged ? 'true' : 'false'}
          @click=${() => void this.updateFilter(toggleUntaggedFilter(this.filter))}
        >
          Untagged
        </button>
        ${this.tags.map(
          (t) => html`
            <button
              type="button"
              class="chip"
              aria-pressed=${selectedTags.includes(t.emoji) ? 'true' : 'false'}
              @click=${() => void this.updateFilter(toggleEmojiTagFilter(this.filter, t.emoji))}
            >
              ${t.emoji} ${t.count}
            </button>
          `,
        )}
      </div>
      <div class="filter-row">
        <label>Media:</label>
        ${(['', 'photo', 'video'] as const).map(
          (mt) => html`
            <button
              type="button"
              class="chip"
              aria-pressed=${(this.filter.media_type ?? '') === mt ? 'true' : 'false'}
              @click=${() => void this.updateFilter({ ...this.filter, media_type: mt || undefined, page: 1 })}
            >
              ${MEDIA_TYPE_LABELS[mt]}
            </button>
          `,
        )}
        ${COMMON_FORMATS.map(
          (fmt) => html`
            <button
              type="button"
              class="chip"
              aria-pressed=${selectedFormats.includes(fmt) ? 'true' : 'false'}
              @click=${() => void this.updateFilter(toggleFormatFilter(this.filter, fmt))}
            >
              ${fmt.toUpperCase()}
            </button>
          `,
        )}
      </div>
    `;
  }

  private renderPathRow() {
    return html`
      <div class="filter-row">
        <label>Path:</label>
        <input
          type="search"
          class="filter-input"
          aria-label="Search filename or path"
          placeholder="Search files…"
          enterkeyhint="search"
          .value=${this.filter.q ?? ''}
          @change=${(e: Event) => void this.updateFilter({ ...this.filter, q: (e.target as HTMLInputElement).value, page: 1 })}
        />
        <select
          class="filter-select"
          aria-label="Folder filter"
          .value=${this.filter.folder ?? ''}
          @change=${(e: Event) => void this.updateFilter({ ...this.filter, folder: (e.target as HTMLSelectElement).value || undefined, page: 1 })}
        >
          <option value="">All Folders</option>
          ${this.folders.map((f) => html`<option value=${f.folder_path}>${f.folder_path || '/'} (${f.count})</option>`)}
        </select>
      </div>
    `;
  }

  private renderGpsRow() {
    const gpsVal = this.filter.has_gps === undefined ? '' : String(this.filter.has_gps);
    return html`
      <div class="filter-row">
        <label>GPS:</label>
        <select
          class="filter-select"
          aria-label="GPS filter"
          .value=${gpsVal}
          @change=${(e: Event) => {
            const v = (e.target as HTMLSelectElement).value;
            void this.updateFilter({ ...this.filter, has_gps: v === '' ? undefined : v === 'true', page: 1 });
          }}
        >
          <option value="">All Locations</option>
          <option value="true">📍 Has GPS</option>
          <option value="false">No GPS</option>
        </select>
        <button type="button" class="btn" popovertarget="gps-bounds-popover">GPS Bounds</button>
        ${this.renderGpsBoundsPopover()}
      </div>
    `;
  }

  private handleDateFocus(field: 'from' | 'to', e: Event): void {
    this.focusedDateField = field;
    (e.target as HTMLInputElement).type = 'date';
  }

  private handleDateBlur(e: Event): void {
    this.focusedDateField = null;
    const input = e.target as HTMLInputElement;
    if (!input.value) {
      input.type = 'text';
    }
  }

  private renderDateRow() {
    const fromVal = this.filter.from_date ?? '';
    const toVal = this.filter.to_date ?? '';
    return html`
      <div class="filter-row">
        <label>Date:</label>
        <input
          type=${fromVal || this.focusedDateField === 'from' ? 'date' : 'text'}
          class="filter-input date-input"
          aria-label="From date"
          placeholder="Start date"
          .value=${fromVal}
          @focus=${(e: Event) => this.handleDateFocus('from', e)}
          @blur=${this.handleDateBlur}
          @change=${(e: Event) => void this.updateFilter({ ...this.filter, from_date: (e.target as HTMLInputElement).value || undefined, page: 1 })}
        />
        <input
          type=${toVal || this.focusedDateField === 'to' ? 'date' : 'text'}
          class="filter-input date-input"
          aria-label="To date"
          placeholder="End date"
          .value=${toVal}
          @focus=${(e: Event) => this.handleDateFocus('to', e)}
          @blur=${this.handleDateBlur}
          @change=${(e: Event) => void this.updateFilter({ ...this.filter, to_date: (e.target as HTMLInputElement).value || undefined, page: 1 })}
        />
      </div>
    `;
  }

  private renderGpsBoundsPopover() {
    return html`
      <div id="gps-bounds-popover" popover="auto" class="popover-panel">
        <div class="popover-header">
          <span>Select GPS Area</span>
          <button
            type="button"
            class="btn"
            popovertarget="gps-bounds-popover"
            popovertargetaction="hide"
            aria-label="Close GPS popup"
          >
            ✕ Close
          </button>
        </div>
        <photo-gps-map-picker
          .minLat=${this.filter.min_lat}
          .maxLat=${this.filter.max_lat}
          .minLng=${this.filter.min_lng}
          .maxLng=${this.filter.max_lng}
          @gps-bounds-change=${this.handleGpsBoundsChange}
        ></photo-gps-map-picker>
        <div class="gps-bounds-grid">
          <input type="number" step="0.0001" class="filter-input" aria-label="Min latitude" placeholder="Min Lat"
            .value=${this.filter.min_lat !== undefined ? String(this.filter.min_lat) : ''}
            @change=${(e: Event) => this.setGpsBound('min_lat', (e.target as HTMLInputElement).value)} />
          <input type="number" step="0.0001" class="filter-input" aria-label="Max latitude" placeholder="Max Lat"
            .value=${this.filter.max_lat !== undefined ? String(this.filter.max_lat) : ''}
            @change=${(e: Event) => this.setGpsBound('max_lat', (e.target as HTMLInputElement).value)} />
          <input type="number" step="0.0001" class="filter-input" aria-label="Min longitude" placeholder="Min Lng"
            .value=${this.filter.min_lng !== undefined ? String(this.filter.min_lng) : ''}
            @change=${(e: Event) => this.setGpsBound('min_lng', (e.target as HTMLInputElement).value)} />
          <input type="number" step="0.0001" class="filter-input" aria-label="Max longitude" placeholder="Max Lng"
            .value=${this.filter.max_lng !== undefined ? String(this.filter.max_lng) : ''}
            @change=${(e: Event) => this.setGpsBound('max_lng', (e.target as HTMLInputElement).value)} />
        </div>
      </div>
    `;
  }

  private renderSortControls() {
    const sortVal = detectSortOption(this.filter);
    return html`
      <label for="sort-select">Sort:</label>
      <select id="sort-select" class="filter-select" .value=${sortVal} @change=${this.handleSortChange}>
        <option value="views_asc">Least Viewed First</option>
        <option value="views_desc">Most Viewed First</option>
        <option value="date_desc">Date Taken (Newest)</option>
        <option value="date_asc">Date Taken (Oldest)</option>
        <option value="last_viewed_desc">Last Viewed (Recent)</option>
        <option value="last_viewed_asc">Last Viewed (Oldest)</option>
        <option value="random">Random Discovery</option>
      </select>
      ${sortVal === 'random'
        ? html`<button type="button" class="btn" @click=${this.reshuffleSeed}>🎲 Reshuffle</button>`
        : null}
    `;
  }

  private renderPhotoCard(photo: PhotoResponse, index: number) {
    const isDeferred = index >= DEFERRED_CARD_START_INDEX;
    const isLcpCandidate = index < LCP_CANDIDATE_COUNT;
    const hasGps =
      photo.gps_latitude !== undefined &&
      photo.gps_latitude !== null &&
      photo.gps_longitude !== undefined &&
      photo.gps_longitude !== null;
    return html`
      <button
        type="button"
        class="photo-card ${isDeferred ? 'deferred-card' : ''}"
        @click=${() => this.selectCard(photo, index)}
      >
        <div class="thumb-wrap">
          <img
            src=${photo.thumbnail_url}
            alt=${photo.base_name}
            decoding="async"
            fetchpriority=${isLcpCandidate ? 'high' : nothing}
            loading=${isLcpCandidate ? nothing : 'lazy'}
          />
          <span class="badge badge-views ${photo.view_count === 0 ? 'unseen' : ''}">
            👁 ${photo.view_count}
          </span>
          ${hasGps ? html`<span class="badge badge-gps" aria-label="Has GPS">📍</span>` : null}
          <span class="badge badge-format">${formatStackBadge(photo)}</span>
        </div>
        <div class="card-meta">
          <span class="card-title">${photo.base_name}</span>
          <span class="muted">${photo.folder_path || '/'} ${photo.emoji_tags.join(' ')}</span>
        </div>
      </button>
    `;
  }

  private renderPagination() {
    const page = this.filter.page ?? 1;
    const pageSize = this.filter.page_size ?? DEFAULT_PAGE_SIZE;
    const totalPages = Math.max(1, Math.ceil(this.total / pageSize));
    return html`
      <nav class="pagination" aria-label="Gallery pagination">
        <button
          type="button"
          class="btn"
          ?disabled=${page <= 1}
          @click=${() => void this.updateFilter({ ...this.filter, page: page - 1 })}
        >
          Previous
        </button>
        <span class="muted">Page ${page} of ${totalPages} (${this.total} total)</span>
        <button
          type="button"
          class="btn"
          ?disabled=${page >= totalPages}
          @click=${() => void this.updateFilter({ ...this.filter, page: page + 1 })}
        >
          Next
        </button>
      </nav>
    `;
  }

  private renderGalleryContent() {
    if (this.loading && this.items.length === 0) {
      return html`<p class="muted" role="status">Loading gallery…</p>`;
    }
    if (this.items.length === 0) {
      return html`<p class="card muted">No photos match the current filters.</p>`;
    }
    return html`
      <section class="photo-grid" aria-label="Photo grid">
        ${this.items.map((photo, idx) => this.renderPhotoCard(photo, idx))}
      </section>
      ${this.renderPagination()}
    `;
  }

  override render() {
    return html`
      <section class="filter-panel" aria-label="Search and filter controls">
        ${this.renderFilterToggle()}
        <div id="filter-controls" class="filter-controls" ?hidden=${this.filtersHidden}>
          ${this.renderViewPresets()}
          ${this.renderEmojiAndFormatRows()}
          ${this.renderPathRow()}
          ${this.renderGpsRow()}
          ${this.renderDateRow()}
          ${this.libraryTotal !== undefined && this.libraryTotal !== null
            ? html`
                <div class="filter-row">
                  <span class="muted">Showing ${this.total} of ${this.libraryTotal} photos</span>
                </div>
              `
            : null}
        </div>
      </section>
      ${this.errorMessage ? html`<p class="error-banner" role="alert">${this.errorMessage}</p>` : null}
      ${this.renderGalleryContent()}
    `;
  }
}

declare global {
  interface HTMLElementTagNameMap {
    'photo-gallery-view': PhotoGalleryView;
  }
}

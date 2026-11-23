import { LitElement, css, html } from 'lit';
import { customElement, property, query, state } from 'lit/decorators.js';
import { ApiClient, ApiError } from '../api/client';
import { sharedStyles } from '../styles/shared';
import type { PhotoResponse } from '../types/api';
import { DwellTimer, type DwellTriggerEvent } from '../utils/dwell-timer';
import {
  buildOpenStreetMapUrl,
  formatDateTime,
  formatFileSize,
} from '../utils/format';

const QUICK_EMOJIS = ['❤️', '⭐', '🔥', '🏖️', '😂', '🎉', '🗑️'];

@customElement('photo-lightbox')
export class PhotoLightbox extends LitElement {
  static override styles = [
    sharedStyles,
    css`
      dialog,
      dialog:modal {
        --bg-surface: rgb(14 14 17 / 0.88);
        --bg-canvas: rgb(24 24 27 / 0.9);
        --bg-subtle: rgb(39 39 42 / 0.9);
        --text-primary: #fafafa;
        --text-muted: #d4d4d8;
        --border-color: rgb(255 255 255 / 0.18);
        position: fixed;
        inset: 0;
        margin: 0;
        padding: 0;
        border: 0;
        border-radius: 0;
        background: #000000;
        color: var(--text-primary);
        inline-size: 100dvw;
        block-size: 100dvh;
        max-inline-size: 100dvw;
        max-block-size: 100dvh;
        overflow: hidden;
      }

      dialog:modal {
        overscroll-behavior: contain;
      }

      dialog::backdrop {
        background: #000000;
        overflow: hidden;
        overscroll-behavior: contain;
      }

      dialog[open] {
        display: block;
      }

      .toolbar {
        position: absolute;
        inset-block-start: 0;
        inset-inline: 0;
        z-index: 10;
        display: flex;
        flex-wrap: wrap;
        align-items: center;
        gap: 0.5rem;
        padding: 0.65rem 0.9rem;
        background: rgb(9 9 11 / 0.78);
        backdrop-filter: blur(8px);
        border-bottom: 1px solid var(--border-color);
        transition: opacity 0.2s ease;
      }

      .toolbar-title {
        margin: 0;
        font-size: 1rem;
        margin-inline-end: auto;
      }

      .variant-group,
      .quick-emoji-row {
        display: inline-flex;
        align-items: center;
        gap: 0.25rem;
        flex-wrap: wrap;
      }

      .popover-panel {
        padding: 0.75rem;
        border-radius: 0.65rem;
        border: 1px solid var(--border-color);
        background: var(--bg-surface);
        color: var(--text-primary);
      }

      [popover]:is(:popover-open, .\:popover-open) {
        display: block;
      }

      @supports (position-area: block-end) {
        [popover].anchored-overlay {
          inset: auto;
          margin: 0;
          position-area: block-end span-inline-end;
          position-try-fallbacks: flip-block, flip-inline, flip-block flip-inline;
        }
      }

      .body-layout {
        position: relative;
        inline-size: 100%;
        block-size: 100%;
        overflow: hidden;
      }

      .stage {
        position: absolute;
        inset: 0;
        display: flex;
        align-items: center;
        justify-content: center;
        background: #000000;
        inline-size: 100dvw;
        block-size: 100dvh;
        overflow: hidden;
        padding: 0;
      }

      .stage.zoomed {
        overflow: auto;
        align-items: flex-start;
        justify-content: flex-start;
      }

      .stage img,
      .stage video {
        display: block;
        object-fit: contain;
        object-position: center;
      }

      .stage img:not(.zoomed),
      .stage video {
        inline-size: 100dvw;
        block-size: 100dvh;
        max-inline-size: 100dvw;
        max-block-size: 100dvh;
      }

      .stage img.zoomed {
        max-inline-size: none;
        max-block-size: none;
        cursor: zoom-out;
      }

      dialog.ui-hidden .stage {
        cursor: none;
      }

      .drawer {
        position: absolute;
        inset-block: 3.75rem 0.75rem;
        inset-inline-end: 0.75rem;
        inline-size: min(22rem, 92vw);
        z-index: 10;
        border: 1px solid var(--border-color);
        border-radius: 0.75rem;
        background: rgb(14 14 17 / 0.84);
        backdrop-filter: blur(10px);
        padding: 0.85rem;
        overflow-y: auto;
        display: grid;
        gap: 0.75rem;
        align-content: start;
        scrollbar-gutter: stable;
        overscroll-behavior: contain;
        transition: opacity 0.2s ease;
      }

      dialog.ui-hidden .toolbar,
      dialog.ui-hidden .drawer {
        opacity: 0;
        pointer-events: none;
      }

      dialog.ui-visible .toolbar,
      dialog.ui-visible .drawer {
        opacity: 1;
        pointer-events: auto;
      }

      @media (prefers-reduced-motion: reduce) {
        .toolbar,
        .drawer {
          transition: none;
        }
      }

      .drawer-card {
        display: grid;
        gap: 0.5rem;
      }

      .drawer-card h3 {
        margin: 0;
        font-size: 0.9rem;
      }

      .view-count-big {
        font-size: 1.5rem;
        font-weight: 700;
      }

      .history-list {
        list-style: none;
        padding: 0;
        margin: 0;
        max-block-size: 9rem;
        overflow-y: auto;
        display: grid;
        gap: 0.25rem;
        font-size: 0.8125rem;
      }

      .meta-list {
        display: grid;
        grid-template-columns: auto 1fr;
        gap: 0.25rem 0.65rem;
        margin: 0;
        font-size: 0.8125rem;
      }

      .meta-list dt {
        color: var(--text-muted, #475569);
      }

      .meta-list dd {
        margin: 0;
        word-break: break-word;
      }

      .file-item {
        padding: 0.4rem 0.5rem;
        border-radius: 0.45rem;
        background: var(--bg-canvas, #f8fafc);
        font-size: 0.8rem;
        display: grid;
        gap: 0.15rem;
      }

      .hash {
        font-family: ui-monospace, monospace;
        font-size: 0.72rem;
        word-break: break-all;
      }

      .video-fallback {
        margin-top: 0.75rem;
        padding: 0.75rem;
        background: var(--bg-surface, #ffffff);
        border-radius: 0.5rem;
        display: flex;
        flex-wrap: wrap;
        gap: 0.5rem;
        align-items: center;
      }

      .emoji-form {
        display: flex;
        gap: 0.35rem;
      }

      .emoji-form input {
        inline-size: 6rem;
        padding: 0.3rem 0.5rem;
        border-radius: 0.4rem;
        border: 1px solid var(--border-color, #cbd5e1);
        background: var(--bg-canvas, #f8fafc);
      }
    `,
  ];

  @property({ attribute: false })
  api = new ApiClient();

  @property({ attribute: false })
  photo: PhotoResponse | null = null;

  @property({ type: Boolean })
  hasPrev = false;

  @property({ type: Boolean })
  hasNext = false;

  @property({ type: Number })
  uiIdleMs = 2200;

  @state()
  private uiVisible = false;

  @state()
  private selectedFormat = '';

  @state()
  private drawerOpen = true;

  @state()
  private zoomed = false;

  @state()
  private slideshowActive = false;

  @state()
  private slideshowIntervalSec = 4;

  @state()
  private videoTranscode = false;

  @state()
  private videoError = false;

  @state()
  private customEmoji = '';

  @state()
  private errorMessage = '';

  @query('#photo-lightbox-dialog')
  private dialogEl?: HTMLDialogElement;

  private lastOpenedPhotoId: string | null = null;
  private slideshowTimer: ReturnType<typeof setInterval> | null = null;
  private uiIdleTimer: ReturnType<typeof setTimeout> | null = null;

  private readonly dwellTimer = new DwellTimer((ev) => {
    void this.handleDwellTriggered(ev);
  });

  private readonly onVisibilityChange = () => {
    this.dwellTimer.handleVisibilityChange();
  };

  private readonly onKeyDown = (e: KeyboardEvent) => {
    if (!this.photo) return;
    const tag = (e.target as HTMLElement | null)?.tagName;
    if (tag === 'INPUT' || tag === 'TEXTAREA' || tag === 'SELECT') return;
    if (e.key === 'ArrowLeft' && this.hasPrev) {
      e.preventDefault();
      this.emitNavigate('prev');
    } else if (e.key === 'ArrowRight' && this.hasNext) {
      e.preventDefault();
      this.emitNavigate('next');
    } else if (e.key === ' ') {
      if ((e.target as HTMLElement | null)?.closest?.('button, a, [role="button"]')) return;
      e.preventDefault();
      this.toggleSlideshow();
    } else if (e.key.toLowerCase() === 'i') {
      e.preventDefault();
      this.drawerOpen = !this.drawerOpen;
    }
  };

  override connectedCallback(): void {
    super.connectedCallback();
    document.addEventListener('visibilitychange', this.onVisibilityChange);
    document.addEventListener('keydown', this.onKeyDown);
  }

  override disconnectedCallback(): void {
    super.disconnectedCallback();
    document.removeEventListener('visibilitychange', this.onVisibilityChange);
    document.removeEventListener('keydown', this.onKeyDown);
    this.dwellTimer.cancel();
    this.stopSlideshow();
    this.clearUiIdleTimer();
  }

  override willUpdate(changed: Map<string, unknown>): void {
    if (!changed.has('photo') || !this.photo) return;
    if (this.photo.id !== this.lastOpenedPhotoId) {
      this.initializeOpenedPhoto(this.photo);
    }
  }

  override updated(changed: Map<string, unknown>): void {
    if (!changed.has('photo')) return;
    if (!this.photo) {
      this.lastOpenedPhotoId = null;
      this.uiVisible = false;
      this.clearUiIdleTimer();
      this.dwellTimer.cancel();
      this.stopSlideshow();
      if (this.dialogEl?.open) this.dialogEl.close();
      return;
    }
    if (this.dialogEl && !this.dialogEl.open) {
      this.dialogEl.showModal();
    }
  }

  private initializeOpenedPhoto(photo: PhotoResponse): void {
    this.lastOpenedPhotoId = photo.id;
    this.uiVisible = false;
    this.clearUiIdleTimer();
    this.selectedFormat = photo.formats[0] ?? '';
    this.zoomed = false;
    this.videoTranscode = false;
    this.videoError = false;
    this.errorMessage = '';
    this.dwellTimer.openPhoto(photo.id, this.slideshowActive ? 'slideshow' : 'dwell');
    void this.fetchFullPhoto(photo.id);
  }

  private async fetchFullPhoto(id: string): Promise<void> {
    try {
      const full = await this.api.getPhoto(id);
      if (this.photo?.id === id) {
        this.photo = full;
      }
    } catch (err) {
      if (this.photo?.id === id) {
        this.errorMessage = err instanceof ApiError ? err.message : 'Failed to load photo details.';
      }
    }
  }

  private async handleDwellTriggered(ev: DwellTriggerEvent): Promise<void> {
    try {
      const updated = await this.api.recordView(ev.photoId, {
        view_id: ev.viewId,
        source: ev.source,
      });
      this.applyUpdatedPhoto(updated);
    } catch (err) {
      if (this.photo?.id === ev.photoId) {
        this.errorMessage = err instanceof ApiError ? err.message : 'Failed to record view.';
      }
    }
  }

  private applyUpdatedPhoto(updated: PhotoResponse): void {
    if (this.photo?.id === updated.id) {
      this.photo = updated;
    }
    this.dispatchEvent(
      new CustomEvent<PhotoResponse>('photo-updated', {
        detail: updated,
        bubbles: true,
        composed: true,
      }),
    );
  }

  private handleMediaLoad(): void {
    if (this.photo) {
      this.dwellTimer.notifyMediaLoaded(this.photo.id);
    }
  }

  private handleVideoError(): void {
    this.videoError = true;
  }

  private enableTranscode(): void {
    this.videoTranscode = true;
    this.videoError = false;
  }

  private selectVariant(fmt: string): void {
    if (!this.photo || fmt === this.selectedFormat) return;
    this.selectedFormat = fmt;
    this.videoError = false;
    this.dwellTimer.switchVariant(this.photo.id);
  }

  private toggleSlideshow(): void {
    this.slideshowActive = !this.slideshowActive;
    if (this.slideshowActive) {
      this.startSlideshowTimer();
    } else {
      this.stopSlideshow();
    }
  }

  private startSlideshowTimer(): void {
    this.stopSlideshow();
    this.slideshowTimer = setInterval(() => {
      if (this.hasNext) {
        this.emitNavigate('next');
      } else {
        this.slideshowActive = false;
        this.stopSlideshow();
      }
    }, this.slideshowIntervalSec * 1000);
  }

  private stopSlideshow(): void {
    if (this.slideshowTimer !== null) {
      clearInterval(this.slideshowTimer);
      this.slideshowTimer = null;
    }
  }

  private handleIntervalChange(e: Event): void {
    const value = Number((e.target as HTMLSelectElement).value);
    this.slideshowIntervalSec = value;
    if (this.slideshowActive) {
      this.startSlideshowTimer();
    }
  }

  private async adjustViews(action: 'increment' | 'decrement' | 'reset'): Promise<void> {
    if (!this.photo) return;
    try {
      const updated = await this.api.adjustViewCount(this.photo.id, action);
      this.applyUpdatedPhoto(updated);
    } catch (err) {
      this.errorMessage = err instanceof ApiError ? err.message : 'Failed to update view count.';
    }
  }

  private async toggleEmojiTag(emoji: string): Promise<void> {
    if (!this.photo) return;
    const exists = this.photo.emoji_tags.includes(emoji);
    try {
      const updated = exists
        ? await this.api.removeTag(this.photo.id, emoji)
        : await this.api.addTag(this.photo.id, emoji);
      this.applyUpdatedPhoto(updated);
    } catch (err) {
      this.errorMessage = err instanceof ApiError ? err.message : 'Failed to update emoji tag.';
    }
  }

  private async submitCustomEmoji(e: Event): Promise<void> {
    e.preventDefault();
    const emoji = this.customEmoji.trim();
    if (!this.photo || !emoji) return;
    try {
      const updated = await this.api.addTag(this.photo.id, emoji);
      this.customEmoji = '';
      this.applyUpdatedPhoto(updated);
    } catch (err) {
      this.errorMessage = err instanceof ApiError ? err.message : 'Invalid emoji tag.';
    }
  }

  private emitNavigate(direction: 'prev' | 'next'): void {
    this.dispatchEvent(
      new CustomEvent<{ direction: 'prev' | 'next' }>('lightbox-navigate', {
        detail: { direction },
        bubbles: true,
        composed: true,
      }),
    );
  }

  private requestClose(): void {
    if (this.dialogEl?.open) {
      this.dialogEl.close();
      return;
    }
    this.handleDialogClose();
  }

  private handlePointerActivity(): void {
    this.uiVisible = true;
    this.clearUiIdleTimer();
    this.uiIdleTimer = setTimeout(() => {
      this.uiVisible = false;
      this.uiIdleTimer = null;
    }, this.uiIdleMs);
  }

  private clearUiIdleTimer(): void {
    if (this.uiIdleTimer !== null) {
      clearTimeout(this.uiIdleTimer);
      this.uiIdleTimer = null;
    }
  }

  private handleDialogClose(): void {
    if (this.dialogEl) {
      this.dialogEl.returnValue = '';
    }
    this.uiVisible = false;
    this.clearUiIdleTimer();
    this.dwellTimer.cancel();
    this.stopSlideshow();
    this.slideshowActive = false;
    this.dispatchEvent(
      new CustomEvent('lightbox-close', { bubbles: true, composed: true }),
    );
  }

  private handleDialogBackdropClick(e: MouseEvent): void {
    if ('closedBy' in HTMLDialogElement.prototype) return;
    const dialog = e.currentTarget as HTMLDialogElement;
    if (e.target !== dialog) return;
    const r = dialog.getBoundingClientRect();
    const inside =
      r.top <= e.clientY &&
      e.clientY <= r.top + r.height &&
      r.left <= e.clientX &&
      e.clientX <= r.left + r.width;
    if (!inside) dialog.close();
  }

  private emitFilterNearby(lat: number, lng: number): void {
    this.dispatchEvent(
      new CustomEvent<{ lat: number; lng: number }>('filter-nearby-gps', {
        detail: { lat, lng },
        bubbles: true,
        composed: true,
      }),
    );
    this.requestClose();
  }

  private buildPreviewUrl(photo: PhotoResponse): string {
    const params = new URLSearchParams();
    if (this.selectedFormat) params.set('format', this.selectedFormat);
    if (this.videoTranscode) params.set('transcode', '1');
    const qs = params.toString();
    return qs ? `${photo.preview_url}?${qs}` : photo.preview_url;
  }

  private buildOriginalDownloadUrl(photo: PhotoResponse): string {
    const params = new URLSearchParams({ download: '1' });
    if (this.selectedFormat) params.set('format', this.selectedFormat);
    return `${photo.original_url}?${params.toString()}`;
  }

  private renderToolbar(photo: PhotoResponse) {
    return html`
      <header class="toolbar">
        <h2 id="lightbox-title" class="toolbar-title">${photo.base_name}</h2>
        <button type="button" class="btn" ?disabled=${!this.hasPrev} @click=${() => this.emitNavigate('prev')}>
          ← Prev
        </button>
        <button type="button" class="btn" ?disabled=${!this.hasNext} @click=${() => this.emitNavigate('next')}>
          Next →
        </button>
        <button type="button" class="btn" aria-pressed=${this.slideshowActive ? 'true' : 'false'} @click=${this.toggleSlideshow}>
          ${this.slideshowActive ? '⏸ Pause' : '▶ Slideshow'}
        </button>
        <select aria-label="Slideshow interval" .value=${String(this.slideshowIntervalSec)} @change=${this.handleIntervalChange}>
          <option value="3">3s</option>
          <option value="4">4s</option>
          <option value="6">6s</option>
          <option value="10">10s</option>
        </select>
        ${this.renderVariantSwitcher(photo)}
        ${this.renderToolbarActions(photo)}
      </header>
    `;
  }

  private renderVariantSwitcher(photo: PhotoResponse) {
    if (photo.formats.length <= 1) return null;
    return html`
      <div class="variant-group" aria-label="Format variant">
        ${photo.formats.map(
          (fmt) => html`
            <button
              type="button"
              class="chip"
              aria-pressed=${this.selectedFormat === fmt ? 'true' : 'false'}
              @click=${() => this.selectVariant(fmt)}
            >
              ${fmt.toUpperCase()}
            </button>
          `,
        )}
      </div>
    `;
  }

  private renderToolbarActions(photo: PhotoResponse) {
    return html`
      <button type="button" class="btn" popovertarget="lightbox-emoji-popover">🏷️ Tag</button>
      <div id="lightbox-emoji-popover" popover="auto" class="popover-panel anchored-overlay">
        <div class="quick-emoji-row">
          ${QUICK_EMOJIS.map(
            (em) => html`
              <button
                type="button"
                class="chip"
                aria-pressed=${photo.emoji_tags.includes(em) ? 'true' : 'false'}
                @click=${() => this.toggleEmojiTag(em)}
              >
                ${em}
              </button>
            `,
          )}
        </div>
      </div>
      ${photo.media_type !== 'video'
        ? html`<button type="button" class="btn" @click=${() => (this.zoomed = !this.zoomed)}>
            ${this.zoomed ? 'Fit' : 'Zoom'}
          </button>`
        : null}
      <a class="btn" href=${this.buildOriginalDownloadUrl(photo)} download>Download</a>
      <button type="button" class="btn" aria-pressed=${this.drawerOpen ? 'true' : 'false'} @click=${() => (this.drawerOpen = !this.drawerOpen)}>
        ℹ Details
      </button>
      <button type="button" class="btn" aria-label="Close lightbox" @click=${this.requestClose}>✕</button>
    `;
  }

  private renderStage(photo: PhotoResponse) {
    const previewUrl = this.buildPreviewUrl(photo);
    if (photo.media_type === 'video') {
      return html`
        <div class="stage">
          <video
            controls
            autoplay
            playsinline
            src=${previewUrl}
            @loadeddata=${this.handleMediaLoad}
            @error=${this.handleVideoError}
          ></video>
          ${this.videoError
            ? html`
                <div class="video-fallback" role="status">
                  <span>Browser could not decode original video stream.</span>
                  <button type="button" class="btn btn-primary" @click=${this.enableTranscode}>
                    Play Transcoded Preview
                  </button>
                  <a class="btn" href=${this.buildOriginalDownloadUrl(photo)} download>
                    Download Original
                  </a>
                </div>
              `
            : null}
        </div>
      `;
    }
    return html`
      <div class="stage ${this.zoomed ? 'zoomed' : ''}">
        <img
          src=${previewUrl}
          alt=${photo.base_name}
          class=${this.zoomed ? 'zoomed' : ''}
          @load=${this.handleMediaLoad}
        />
      </div>
    `;
  }

  private renderViewCounterCard(photo: PhotoResponse) {
    const history = photo.view_history ?? [];
    return html`
      <section class="card drawer-card">
        <h3>View Counter</h3>
        <div class="view-count-big">👁 ${photo.view_count}</div>
        <div class="quick-emoji-row">
          <button type="button" class="btn" @click=${() => this.adjustViews('increment')}>+1</button>
          <button type="button" class="btn" @click=${() => this.adjustViews('decrement')}>-1</button>
          <button type="button" class="btn btn-danger" @click=${() => this.adjustViews('reset')}>Reset to 0</button>
        </div>
        <ul class="history-list" aria-label="View history">
          ${history.length === 0
            ? html`<li class="muted">No recorded views yet.</li>`
            : history.map(
                (ev) => html`<li>Viewed ${formatDateTime(ev.viewed_at)} (${ev.source})</li>`,
              )}
        </ul>
      </section>
    `;
  }

  private renderEmojiCard(photo: PhotoResponse) {
    return html`
      <section class="card drawer-card">
        <h3>Emoji Tags</h3>
        <div class="quick-emoji-row">
          ${photo.emoji_tags.length === 0
            ? html`<span class="muted">No tags</span>`
            : photo.emoji_tags.map(
                (em) => html`
                  <button type="button" class="chip active" aria-label=${`Remove ${em}`} @click=${() => this.toggleEmojiTag(em)}>
                    ${em} ×
                  </button>
                `,
              )}
        </div>
        <div class="quick-emoji-row">
          ${QUICK_EMOJIS.map(
            (em) => html`
              <button
                type="button"
                class="chip"
                aria-pressed=${photo.emoji_tags.includes(em) ? 'true' : 'false'}
                @click=${() => this.toggleEmojiTag(em)}
              >
                ${em}
              </button>
            `,
          )}
        </div>
        <form class="emoji-form" @submit=${this.submitCustomEmoji}>
          <input
            type="text"
            aria-label="Custom emoji tag"
            placeholder="Add emoji…"
            .value=${this.customEmoji}
            @input=${(e: Event) => (this.customEmoji = (e.target as HTMLInputElement).value)}
          />
          <button type="submit" class="btn">Add</button>
        </form>
      </section>
    `;
  }

  private renderFilesAndExifCards(photo: PhotoResponse) {
    const exif = photo.exif;
    return html`
      <section class="card drawer-card">
        <h3>Files &amp; Locations (${photo.files.length})</h3>
        ${photo.files.map(
          (f) => html`
            <div class="file-item">
              <strong>${f.relative_path}</strong>
              <span>${f.format.toUpperCase()} · ${formatFileSize(f.file_size)} · ${f.status}</span>
              <span class="hash">${f.content_hash}</span>
            </div>
          `,
        )}
      </section>
      <section class="card drawer-card">
        <h3>EXIF &amp; Camera</h3>
        <dl class="meta-list">
          <dt>Dimensions</dt><dd>${photo.width} × ${photo.height}</dd>
          <dt>Taken</dt><dd>${formatDateTime(photo.taken_at)}</dd>
          ${exif?.camera_model ? html`<dt>Camera</dt><dd>${exif.camera_make ?? ''} ${exif.camera_model}</dd>` : null}
          ${exif?.lens_model ? html`<dt>Lens</dt><dd>${exif.lens_model}</dd>` : null}
          ${exif?.focal_length_mm ? html`<dt>Focal</dt><dd>${exif.focal_length_mm}mm</dd>` : null}
          ${exif?.f_number ? html`<dt>Aperture</dt><dd>f/${exif.f_number}</dd>` : null}
          ${exif?.exposure_time ? html`<dt>Shutter</dt><dd>${exif.exposure_time}s</dd>` : null}
          ${exif?.iso ? html`<dt>ISO</dt><dd>${exif.iso}</dd>` : null}
        </dl>
      </section>
    `;
  }

  private renderGpsCard(photo: PhotoResponse) {
    if (
      photo.gps_latitude === undefined ||
      photo.gps_latitude === null ||
      photo.gps_longitude === undefined ||
      photo.gps_longitude === null
    ) {
      return null;
    }
    const lat = photo.gps_latitude;
    const lng = photo.gps_longitude;
    return html`
      <section class="card drawer-card">
        <h3>GPS Location</h3>
        <div class="muted">${lat.toFixed(5)}, ${lng.toFixed(5)}</div>
        <div class="quick-emoji-row">
          <button type="button" class="btn btn-primary" @click=${() => this.emitFilterNearby(lat, lng)}>
            Filter Nearby Photos
          </button>
          <a class="btn" href=${buildOpenStreetMapUrl(lat, lng)} target="_blank" rel="noopener noreferrer">
            Open Map
          </a>
        </div>
      </section>
    `;
  }

  override render() {
    const photo = this.photo;
    return html`
      <dialog
        id="photo-lightbox-dialog"
        class=${this.uiVisible ? 'ui-visible' : 'ui-hidden'}
        closedby="any"
        aria-labelledby="lightbox-title"
        @mousemove=${this.handlePointerActivity}
        @pointermove=${this.handlePointerActivity}
        @close=${this.handleDialogClose}
        @click=${this.handleDialogBackdropClick}
      >
        ${photo
          ? html`
              <div class="body-layout">
                ${this.renderToolbar(photo)}
                ${this.renderStage(photo)}
                ${this.drawerOpen
                  ? html`
                      <aside class="drawer" aria-label="Photo details">
                        ${this.errorMessage
                          ? html`<p class="error-banner" role="alert">${this.errorMessage}</p>`
                          : null}
                        ${this.renderViewCounterCard(photo)}
                        ${this.renderEmojiCard(photo)}
                        ${this.renderFilesAndExifCards(photo)}
                        ${this.renderGpsCard(photo)}
                      </aside>
                    `
                  : null}
              </div>
            `
          : null}
      </dialog>
    `;
  }
}

declare global {
  interface HTMLElementTagNameMap {
    'photo-lightbox': PhotoLightbox;
  }
}

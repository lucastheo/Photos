import { LitElement, css, html } from 'lit';
import { customElement, query, state } from 'lit/decorators.js';
import { ApiClient, ApiError } from './api/client';
import './components/photo-lightbox';
import { sharedStyles } from './styles/shared';
import type {
  LibraryStatsResponse,
  PhotoFilterParams,
  PhotoResponse,
  SessionResponse,
} from './types/api';
import {
  DEFAULT_PHOTO_FILTER,
  filterToSearchParams,
  runViewTransition,
  searchParamsToFilter,
} from './utils/format';
import './views/gallery-view';
import type { PhotoGalleryView } from './views/gallery-view';
import './views/library-view';
import type { PhotoLibraryView } from './views/library-view';
import './views/login-view';

type AppRoute = 'gallery' | 'library' | 'login';

@customElement('photo-tracker-app')
export class PhotoTrackerApp extends LitElement {
  static override styles = [
    sharedStyles,
    css`
      :host {
        display: block;
        inline-size: 100%;
        padding: 1rem 1.25rem 2.5rem;
      }

      .app-header {
        display: flex;
        flex-wrap: wrap;
        align-items: center;
        justify-content: space-between;
        gap: 0.75rem;
        margin-block-end: 1rem;
        padding-block-end: 0.75rem;
        border-bottom: 1px solid var(--border-color, #cbd5e1);
      }

      .brand h1 {
        font-size: 1.4rem;
        margin: 0 0 0.2rem;
      }

      .brand p {
        margin: 0;
        color: var(--text-muted, #475569);
        font-size: 0.875rem;
      }

      .header-actions {
        display: flex;
        flex-wrap: wrap;
        align-items: center;
        gap: 0.5rem;
      }

      .stat-pill {
        padding: 0.28rem 0.6rem;
        border-radius: 999px;
        background: var(--bg-surface, #ffffff);
        border: 1px solid var(--border-color, #cbd5e1);
        font-size: 0.8125rem;
      }
    `,
  ];

  api = new ApiClient();

  @state()
  private authReady = false;

  @state()
  private route: AppRoute = 'gallery';

  @state()
  private session: SessionResponse = { authenticated: true, auth_disabled: true };

  @state()
  private stats: LibraryStatsResponse | null = null;

  @state()
  private galleryFilter: PhotoFilterParams = { ...DEFAULT_PHOTO_FILTER };

  @state()
  private activePhoto: PhotoResponse | null = null;

  @state()
  private activeIndex = -1;

  @state()
  private quickSyncing = false;

  @state()
  private headerError = '';

  @query('photo-gallery-view')
  private galleryView?: PhotoGalleryView;

  @query('photo-library-view')
  private libraryView?: PhotoLibraryView;

  private readonly onPopState = () => {
    void this.syncFromLocation();
  };

  override connectedCallback(): void {
    super.connectedCallback();
    window.addEventListener('popstate', this.onPopState);
    void this.initializeApp();
  }

  override disconnectedCallback(): void {
    super.disconnectedCallback();
    window.removeEventListener('popstate', this.onPopState);
  }

  async initializeApp(): Promise<void> {
    this.parseLocationState();
    try {
      const sess = await this.api.getSession();
      this.session = sess;
      if (!sess.authenticated && !sess.auth_disabled) {
        this.navigateTo('login', '/login');
        return;
      }
      await this.refreshStats();
      await this.openPhotoFromLocation();
    } catch (err) {
      this.headerError = err instanceof ApiError ? err.message : 'Unable to reach server.';
    } finally {
      this.authReady = true;
    }
  }

  private parseLocationState(): void {
    const pathname = window.location.pathname;
    const parsedFilter = searchParamsToFilter(new URLSearchParams(window.location.search));
    if (Object.keys(parsedFilter).length > 0) {
      this.galleryFilter = { ...DEFAULT_PHOTO_FILTER, ...parsedFilter };
    }
    if (pathname === '/library') {
      this.route = 'library';
    } else if (pathname === '/login') {
      this.route = 'login';
    } else {
      this.route = 'gallery';
    }
  }

  private async openPhotoFromLocation(): Promise<void> {
    const match = window.location.pathname.match(/^\/photo\/([^/]+)$/);
    if (!match) {
      this.activePhoto = null;
      this.activeIndex = -1;
      return;
    }
    const id = decodeURIComponent(match[1]);
    try {
      this.activePhoto = await this.api.getPhoto(id);
      this.activeIndex = this.galleryView?.items.findIndex((p) => p.id === id) ?? -1;
    } catch {
      this.activePhoto = null;
    }
  }

  private async syncFromLocation(): Promise<void> {
    this.parseLocationState();
    await this.openPhotoFromLocation();
  }

  private navigateTo(route: AppRoute, path: string): void {
    runViewTransition(() => {
      this.route = route;
      if (route !== 'gallery') {
        this.activePhoto = null;
      }
    });
    if (window.location.pathname !== path) {
      window.history.pushState(null, '', path);
    }
    void this.updateComplete.then(() => {
      this.renderRoot.querySelector<HTMLElement>('main')?.focus();
    });
  }

  private async refreshStats(): Promise<void> {
    try {
      this.stats = await this.api.getLibraryStats();
    } catch {
      // Header stats refresh errors are non-fatal.
    }
  }

  private async handleLoginSuccess(e: CustomEvent<SessionResponse>): Promise<void> {
    this.session = e.detail;
    this.headerError = '';
    this.navigateTo('gallery', '/');
    await this.refreshStats();
  }

  private async handleLogout(): Promise<void> {
    try {
      this.session = await this.api.logout();
      this.stats = null;
      this.navigateTo('login', '/login');
    } catch (err) {
      this.headerError = err instanceof ApiError ? err.message : 'Logout failed.';
    }
  }

  private async handleQuickReimport(): Promise<void> {
    if (this.quickSyncing) return;
    this.quickSyncing = true;
    this.headerError = '';
    try {
      await this.api.triggerSync(false);
      await this.refreshStats();
      await this.galleryView?.loadGallery();
      await this.libraryView?.loadLibraryData();
    } catch (err) {
      this.headerError = err instanceof ApiError ? err.message : 'Quick re-import failed.';
    } finally {
      this.quickSyncing = false;
    }
  }

  private handleFilterChange(e: CustomEvent<PhotoFilterParams>): void {
    this.galleryFilter = e.detail;
    const qs = filterToSearchParams(e.detail).toString();
    const targetUrl = qs ? `/?${qs}` : '/';
    window.history.replaceState(null, '', targetUrl);
  }

  private handlePhotoSelect(e: CustomEvent<{ photo: PhotoResponse; index: number }>): void {
    const { photo, index } = e.detail;
    runViewTransition(() => {
      this.activePhoto = photo;
      this.activeIndex = index;
    });
    window.history.pushState(null, '', `/photo/${encodeURIComponent(photo.id)}`);
  }

  private handlePhotoUpdated(e: CustomEvent<PhotoResponse>): void {
    const updated = e.detail;
    if (this.activePhoto?.id === updated.id) {
      this.activePhoto = updated;
    }
    this.galleryView?.updatePhotoInPlace(updated);
    void this.refreshStats();
  }

  private handleLightboxNavigate(e: CustomEvent<{ direction: 'prev' | 'next' }>): void {
    const items = this.galleryView?.items ?? [];
    const offset = e.detail.direction === 'next' ? 1 : -1;
    const nextIdx = this.activeIndex + offset;
    if (nextIdx < 0 || nextIdx >= items.length) return;
    const target = items[nextIdx];
    const transitionDir = e.detail.direction === 'next' ? 'forward' : 'backward';
    runViewTransition(() => {
      this.activePhoto = target;
      this.activeIndex = nextIdx;
    }, transitionDir);
    window.history.replaceState(null, '', `/photo/${encodeURIComponent(target.id)}`);
  }

  private handleLightboxClose(): void {
    this.activePhoto = null;
    this.activeIndex = -1;
    if (window.location.pathname.startsWith('/photo/')) {
      const qs = filterToSearchParams(this.galleryFilter).toString();
      window.history.replaceState(null, '', qs ? `/?${qs}` : '/');
    }
  }

  private handleFilterNearby(e: CustomEvent<{ lat: number; lng: number }>): void {
    this.route = 'gallery';
    void this.galleryView?.applyNearbyGPS(e.detail.lat, e.detail.lng);
  }

  private handleGalleryLoaded(e: CustomEvent<PhotoResponse[]>): void {
    if (!this.activePhoto) return;
    this.activeIndex = e.detail.findIndex((p) => p.id === this.activePhoto?.id);
  }

  private renderHeader() {
    const showNav = this.session.authenticated || this.session.auth_disabled;
    const showSignOut = !this.session.auth_disabled && this.session.authenticated;
    return html`
      <header class="app-header">
        <div class="brand">
          <h1>Photo View Tracker</h1>
          <p>Personal photo &amp; video library view tracker.</p>
        </div>
        ${showNav
          ? html`
              <nav class="header-actions" aria-label="Main navigation">
                ${this.stats
                  ? html`<span class="stat-pill">Unseen: ${this.stats.unseen_photos}</span>
                      <span class="stat-pill">Total: ${this.stats.total_photos}</span>`
                  : null}
                <button type="button" class="btn ${this.route === 'gallery' ? 'btn-primary' : ''}" @click=${() => this.navigateTo('gallery', '/')}>
                  Gallery
                </button>
                <button type="button" class="btn ${this.route === 'library' ? 'btn-primary' : ''}" @click=${() => this.navigateTo('library', '/library')}>
                  Library &amp; Sync
                </button>
                <button type="button" class="btn" ?disabled=${this.quickSyncing} @click=${this.handleQuickReimport}>
                  ${this.quickSyncing ? 'Syncing…' : '↻ Re-import'}
                </button>
                ${showSignOut
                  ? html`<button type="button" class="btn" @click=${this.handleLogout}>Sign Out</button>`
                  : null}
              </nav>
            `
          : null}
      </header>
    `;
  }

  private renderRouteView() {
    if (!this.authReady) {
      return html`<p class="muted" role="status">Checking session…</p>`;
    }
    if (this.route === 'login') {
      return html`<photo-login-view .api=${this.api}></photo-login-view>`;
    }
    if (this.route === 'library') {
      return html`<photo-library-view .api=${this.api}></photo-library-view>`;
    }
    return html`<photo-gallery-view
      .api=${this.api}
      .filter=${this.galleryFilter}
      .folders=${this.stats?.by_folder ?? []}
    ></photo-gallery-view>`;
  }

  override render() {
    const itemsCount = this.galleryView?.items.length ?? 0;
    return html`
      ${this.renderHeader()}
      ${this.headerError ? html`<p class="error-banner" role="alert">${this.headerError}</p>` : null}
      <main
        tabindex="-1"
        @login-success=${this.handleLoginSuccess}
        @filter-change=${this.handleFilterChange}
        @gallery-loaded=${this.handleGalleryLoaded}
        @photo-select=${this.handlePhotoSelect}
        @photo-updated=${this.handlePhotoUpdated}
        @lightbox-navigate=${this.handleLightboxNavigate}
        @lightbox-close=${this.handleLightboxClose}
        @filter-nearby-gps=${this.handleFilterNearby}
        @library-synced=${() => void this.refreshStats()}
      >
        ${this.renderRouteView()}
        <photo-lightbox
          .api=${this.api}
          .photo=${this.activePhoto}
          .hasPrev=${this.activeIndex > 0}
          .hasNext=${this.activeIndex >= 0 && this.activeIndex < itemsCount - 1}
        ></photo-lightbox>
      </main>
    `;
  }
}

declare global {
  interface HTMLElementTagNameMap {
    'photo-tracker-app': PhotoTrackerApp;
  }
}

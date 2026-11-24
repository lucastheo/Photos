import { LitElement, css, html } from 'lit';
import { customElement, property, state } from 'lit/decorators.js';
import { ApiClient, ApiError } from '../api/client';
import { sharedStyles } from '../styles/shared';
import type {
  DuplicateGroupDTO,
  LibraryStatsResponse,
  PhotoResponse,
  SyncReportResponse,
} from '../types/api';
import { formatDateTime, formatFileSize } from '../utils/format';

@customElement('photo-library-view')
export class PhotoLibraryView extends LitElement {
  static override styles = [
    sharedStyles,
    css`
      :host {
        display: grid;
        gap: 1rem;
      }

      .dashboard-grid {
        display: grid;
        grid-template-columns: auto 1fr;
        gap: 1.25rem;
        align-items: center;
      }

      @media (max-width: 680px) {
        .dashboard-grid {
          grid-template-columns: 1fr;
        }
      }

      .progress-ring-wrapper {
        position: relative;
        display: grid;
        place-items: center;
      }

      progress.progress-ring {
        --size: 132px;
        --thickness: 14px;
        --track-color: var(--bg-subtle, #e2e8f0);
        --fill-color: var(--accent, #18181b);
        --value: attr(value type(<number>));
        inline-size: var(--size);
        block-size: var(--size);
        border-radius: 50%;
        background: conic-gradient(var(--fill-color) calc(var(--value) * 1%), var(--track-color) 0);
        background-clip: border-area;
        border: var(--thickness) solid transparent;
        background-origin: border-box;
      }

      @supports not (background-clip: border-area) {
        progress.progress-ring {
          mask-image: radial-gradient(
            transparent calc(50% - var(--thickness)),
            black calc(50% - var(--thickness) + 0.5px)
          );
          border: 0;
        }
      }

      progress.progress-ring::-webkit-progress-bar {
        display: none;
        background: none;
      }

      progress.progress-ring::-webkit-progress-value {
        display: none;
        background: none;
      }

      progress.progress-ring::-moz-progress-bar {
        display: none;
        background: none;
      }

      .progress-ring-content {
        position: absolute;
        font-weight: 700;
        font-size: 1.15rem;
      }

      .stats-cards {
        display: grid;
        grid-template-columns: repeat(auto-fit, minmax(130px, 1fr));
        gap: 0.65rem;
      }

      .stat-box {
        padding: 0.6rem 0.75rem;
        border-radius: 0.55rem;
        background: var(--bg-canvas, #f8fafc);
        border: 1px solid var(--border-color, #cbd5e1);
      }

      .stat-val {
        font-size: 1.25rem;
        font-weight: 700;
      }

      .sync-header {
        display: flex;
        flex-wrap: wrap;
        align-items: center;
        justify-content: space-between;
        gap: 0.75rem;
      }

      progress.sync-bar {
        inline-size: 100%;
        block-size: 0.65rem;
        margin: 0.75rem 0;
      }

      .table-wrapper {
        overflow: auto;
        max-inline-size: 100%;
        max-block-size: min(460px, 70vh);
        container-type: inline-size;
      }

      table {
        inline-size: 100%;
        border-collapse: separate;
        border-spacing: 0;
        font-size: 0.875rem;
      }

      th,
      td {
        text-align: start;
        padding: 0.5rem 0.65rem;
        border-bottom: 1px solid var(--border-color, #cbd5e1);
      }

      thead {
        position: sticky;
        inset-block-start: 0;
        z-index: 1;
        background: var(--bg-surface, #ffffff);
      }

      tbody th {
        position: sticky;
        inset-inline-start: 0;
        background: var(--bg-surface, #ffffff);
      }

      @container (width < 600px) {
        table,
        thead,
        tbody,
        tr,
        th,
        td {
          display: block;
        }

        thead {
          display: none;
        }

        tr {
          margin-block-end: 0.85rem;
          border: 1px solid var(--border-color, #cbd5e1);
          border-radius: 0.5rem;
          padding: 0.35rem;
        }

        td[data-label]::before {
          content: attr(data-label) ': ';
          font-weight: 600;
        }
      }

      .mt-sm {
        margin-block-start: 0.75rem;
      }

      .heading-reset {
        margin: 0;
      }

      .heading-top-reset {
        margin-block-start: 0;
      }
    `,
  ];

  @property({ attribute: false })
  api = new ApiClient();

  @property({ type: Number })
  pollIntervalMs = 400;

  @state()
  stats: LibraryStatsResponse | null = null;

  @state()
  syncReport: SyncReportResponse | null = null;

  @state()
  duplicates: DuplicateGroupDTO[] = [];

  @state()
  missingPhotos: PhotoResponse[] = [];

  @state()
  private syncing = false;

  @state()
  private purging = false;

  @state()
  private statusMessage = '';

  @state()
  private errorMessage = '';

  override connectedCallback(): void {
    super.connectedCallback();
    void this.loadLibraryData();
  }

  async loadLibraryData(): Promise<void> {
    this.errorMessage = '';
    try {
      const [stats, syncReport, duplicates, missing] = await Promise.all([
        this.api.getLibraryStats(),
        this.api.getSyncStatus(),
        this.api.listDuplicates(),
        this.api.listMissing(),
      ]);
      this.stats = stats;
      this.syncReport = syncReport;
      this.duplicates = duplicates;
      this.missingPhotos = missing;
    } catch (err) {
      this.errorMessage = err instanceof ApiError ? err.message : 'Failed to load library data.';
    }
  }

  async triggerReimport(asyncMode = false): Promise<void> {
    if (this.syncing) return;
    this.syncing = true;
    this.errorMessage = '';
    this.statusMessage = '';
    try {
      let report = await this.api.triggerSync(asyncMode);
      this.syncReport = report;
      while (this.isConnected && report.status === 'running') {
        await new Promise((resolve) => setTimeout(resolve, this.pollIntervalMs));
        report = await this.api.getSyncStatus();
        this.syncReport = report;
      }
      this.statusMessage = `Sync ${report.status}: ${report.scanned_count} scanned, ${report.added_count} added.`;
      await this.loadLibraryData();
      this.dispatchEvent(new CustomEvent('library-synced', { bubbles: true, composed: true }));
    } catch (err) {
      this.errorMessage = err instanceof ApiError ? err.message : 'Library sync failed.';
    } finally {
      this.syncing = false;
    }
  }

  async handlePurgeMissing(): Promise<void> {
    if (this.purging) return;
    this.purging = true;
    this.errorMessage = '';
    try {
      const res = await this.api.purgeMissing();
      this.statusMessage = `Purged ${res.deleted_count} missing record(s).`;
      await this.loadLibraryData();
      this.dispatchEvent(new CustomEvent('library-synced', { bubbles: true, composed: true }));
    } catch (err) {
      this.errorMessage = err instanceof ApiError ? err.message : 'Failed to purge missing records.';
    } finally {
      this.purging = false;
    }
  }

  private renderDashboard() {
    const s = this.stats;
    if (!s) return html`<p class="card muted">Loading library statistics…</p>`;
    const seenPct = s.total_photos > 0 ? Math.round((s.seen_photos / s.total_photos) * 100) : 0;
    return html`
      <section class="card" aria-labelledby="coverage-heading">
        <h2 id="coverage-heading">Library Coverage</h2>
        <div class="dashboard-grid">
          <div class="progress-ring-wrapper">
            <progress
              class="progress-ring"
              value=${seenPct}
              max="100"
              aria-label="Library view coverage"
              style=${`--value: ${seenPct}`}
            ></progress>
            <div class="progress-ring-content" aria-hidden="true">${seenPct}%</div>
          </div>
          <div class="stats-cards">
            <div class="stat-box"><div class="muted">Total Photos</div><div class="stat-val">${s.total_photos}</div></div>
            <div class="stat-box"><div class="muted">Never Seen (0)</div><div class="stat-val">${s.unseen_photos}</div></div>
            <div class="stat-box"><div class="muted">Seen (≥1)</div><div class="stat-val">${s.seen_photos}</div></div>
            <div class="stat-box"><div class="muted">Total Views</div><div class="stat-val">${s.total_views}</div></div>
            <div class="stat-box"><div class="muted">Total Files</div><div class="stat-val">${s.total_files}</div></div>
            <div class="stat-box"><div class="muted">Duplicates</div><div class="stat-val">${s.duplicate_groups}</div></div>
            <div class="stat-box"><div class="muted">Missing</div><div class="stat-val">${s.missing_files}</div></div>
          </div>
        </div>
        <div class="sync-header mt-sm">
          <span class="muted">Formats: ${s.by_format.map((f) => `${f.format.toUpperCase()} (${f.count})`).join(' · ') || 'None'}</span>
        </div>
      </section>
    `;
  }

  private renderSyncCard() {
    const r = this.syncReport;
    const isRunning = this.syncing || r?.status === 'running';
    return html`
      <section class="card" aria-labelledby="sync-heading">
        <div class="sync-header">
          <div>
            <h2 id="sync-heading" class="heading-reset">Re-import / Sync Library</h2>
            <span class="muted">Status: ${r?.status ?? 'idle'} ${r?.finished_at ? `· Finished ${formatDateTime(r.finished_at)}` : ''}</span>
          </div>
          <div class="sync-header">
            <button type="button" class="btn btn-primary" ?disabled=${isRunning} @click=${() => this.triggerReimport(false)}>
              ${isRunning ? 'Syncing…' : 'Re-import / Sync Library'}
            </button>
          </div>
        </div>
        ${isRunning
          ? html`<progress class="sync-bar" aria-label="Library sync progress"></progress>`
          : html`<progress class="sync-bar" value=${r?.status === 'completed' ? 100 : 0} max="100" aria-label="Library sync progress"></progress>`}
        ${r
          ? html`
              <div class="stats-cards">
                <div class="stat-box"><div class="muted">Scanned</div><div class="stat-val">${r.scanned_count}</div></div>
                <div class="stat-box"><div class="muted">Added</div><div class="stat-val">${r.added_count}</div></div>
                <div class="stat-box"><div class="muted">Updated</div><div class="stat-val">${r.updated_count}</div></div>
                <div class="stat-box"><div class="muted">Moved</div><div class="stat-val">${r.moved_count}</div></div>
                <div class="stat-box"><div class="muted">Stacked</div><div class="stat-val">${r.stacked_count}</div></div>
                <div class="stat-box"><div class="muted">Duplicates</div><div class="stat-val">${r.duplicate_count}</div></div>
                <div class="stat-box"><div class="muted">Unchanged</div><div class="stat-val">${r.unchanged_count}</div></div>
                <div class="stat-box"><div class="muted">Missing</div><div class="stat-val">${r.missing_count}</div></div>
              </div>
              ${r.error_message ? html`<p class="error-banner" role="alert">${r.error_message}</p>` : null}
            `
          : null}
      </section>
    `;
  }

  private renderDuplicatesSection() {
    return html`
      <section class="card" aria-labelledby="duplicates-heading">
        <h2 id="duplicates-heading" class="heading-top-reset">Duplicate Files Inspector (${this.duplicates.length})</h2>
        ${this.duplicates.length === 0
          ? html`<p class="muted">No exact SHA-256 duplicate files detected.</p>`
          : html`
              <div tabindex="0" role="region" aria-labelledby="duplicates-table-caption" class="table-wrapper">
                <table>
                  <caption id="duplicates-table-caption">Exact duplicate file groups</caption>
                  <thead>
                    <tr><th>SHA-256</th><th>Copies</th><th>Locations</th></tr>
                  </thead>
                  <tbody>
                    ${this.duplicates.map(
                      (g) => html`
                        <tr>
                          <th><code>${g.content_hash.slice(0, 16)}…</code></th>
                          <td data-label="Copies">${g.file_count}</td>
                          <td data-label="Locations">${g.files.map((f) => `${f.relative_path} (${formatFileSize(f.file_size)})`).join(', ')}</td>
                        </tr>
                      `,
                    )}
                  </tbody>
                </table>
              </div>
            `}
      </section>
    `;
  }

  private renderMissingSection() {
    return html`
      <section class="card" aria-labelledby="missing-heading">
        <div class="sync-header">
          <h2 id="missing-heading" class="heading-reset">Missing Files (${this.missingPhotos.length})</h2>
          <button
            type="button"
            class="btn btn-danger"
            ?disabled=${this.missingPhotos.length === 0 || this.purging}
            @click=${this.handlePurgeMissing}
          >
            ${this.purging ? 'Purging…' : 'Purge Missing Records'}
          </button>
        </div>
        ${this.missingPhotos.length === 0
          ? html`<p class="muted">No missing files recorded.</p>`
          : html`
              <div tabindex="0" role="region" aria-labelledby="missing-table-caption" class="table-wrapper">
                <table>
                  <caption id="missing-table-caption">Missing library files</caption>
                  <thead>
                    <tr><th>Photo</th><th>Views</th><th>Last Known Paths</th></tr>
                  </thead>
                  <tbody>
                    ${this.missingPhotos.map(
                      (p) => html`
                        <tr>
                          <th>${p.base_name}</th>
                          <td data-label="Views">👁 ${p.view_count}</td>
                          <td data-label="Last Known Paths">${p.files.map((f) => f.relative_path).join(', ')}</td>
                        </tr>
                      `,
                    )}
                  </tbody>
                </table>
              </div>
            `}
      </section>
    `;
  }

  override render() {
    return html`
      ${this.statusMessage ? html`<p class="card" role="status">${this.statusMessage}</p>` : null}
      ${this.errorMessage ? html`<p class="error-banner" role="alert">${this.errorMessage}</p>` : null}
      ${this.renderDashboard()}
      ${this.renderSyncCard()}
      ${this.renderDuplicatesSection()}
      ${this.renderMissingSection()}
    `;
  }
}

declare global {
  interface HTMLElementTagNameMap {
    'photo-library-view': PhotoLibraryView;
  }
}

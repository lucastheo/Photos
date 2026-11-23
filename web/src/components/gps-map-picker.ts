import { LitElement, css, html, nothing } from 'lit';
import { customElement, property, state } from 'lit/decorators.js';
import { sharedStyles } from '../styles/shared';

export const MAP_VIEW_W = 960;
export const MAP_VIEW_H = 560;
export const MAP_TILE_SIZE = 256;
const MAX_MERCATOR_LAT = 85.0511;
const MIN_ZOOM = 1;
const MAX_ZOOM = 10;
const DEFAULT_LAT = 15;
const DEFAULT_LNG = 0;
const DEFAULT_ZOOM = 2;
const OSM_TILE_BASE_URL = 'https://tile.openstreetmap.org';
const GRID_LNGS = [-150, -120, -90, -60, -30, 0, 30, 60, 90, 120, 150];
const GRID_LATS = [-60, -40, -20, 0, 20, 40, 60];

export interface GpsBoundsEventDetail {
  min_lat?: number;
  max_lat?: number;
  min_lng?: number;
  max_lng?: number;
}

interface Point {
  x: number;
  y: number;
}

interface TileSpec {
  key: string;
  x: number;
  y: number;
  left: number;
  top: number;
  src: string;
}

function clamp(val: number, min: number, max: number): number {
  return Math.min(max, Math.max(min, val));
}

function roundCoord(val: number): number {
  return Math.round(val * 10000) / 10000;
}

export function lngToWorldX(lng: number, zoom: number): number {
  const clamped = clamp(lng, -180, 180);
  const worldSize = MAP_TILE_SIZE * 2 ** zoom;
  return ((clamped + 180) / 360) * worldSize;
}

export function latToWorldY(lat: number, zoom: number): number {
  const clamped = clamp(lat, -MAX_MERCATOR_LAT, MAX_MERCATOR_LAT);
  const rad = (clamped * Math.PI) / 180;
  const mercN = Math.log(Math.tan(Math.PI / 4 + rad / 2));
  const worldSize = MAP_TILE_SIZE * 2 ** zoom;
  return ((1 - mercN / Math.PI) / 2) * worldSize;
}

export function worldXToLng(x: number, zoom: number): number {
  const worldSize = MAP_TILE_SIZE * 2 ** zoom;
  const lng = (x / worldSize) * 360 - 180;
  return roundCoord(clamp(lng, -180, 180));
}

export function worldYToLat(y: number, zoom: number): number {
  const worldSize = MAP_TILE_SIZE * 2 ** zoom;
  const n = Math.PI - (2 * Math.PI * y) / worldSize;
  const lat = (180 / Math.PI) * Math.atan(0.5 * (Math.exp(n) - Math.exp(-n)));
  return roundCoord(clamp(lat, -MAX_MERCATOR_LAT, MAX_MERCATOR_LAT));
}

@customElement('photo-gps-map-picker')
export class PhotoGpsMapPicker extends LitElement {
  static override styles = [
    sharedStyles,
    css`
      :host {
        display: grid;
        grid-template-rows: auto 1fr auto;
        gap: 0.45rem;
        block-size: 100%;
        min-block-size: 0;
      }

      .map-toolbar {
        display: flex;
        flex-wrap: wrap;
        align-items: center;
        gap: 0.35rem;
      }

      .map-toolbar .btn {
        padding: 0.25rem 0.5rem;
        font-size: 0.78rem;
      }

      .map-toolbar .btn[aria-pressed='true'] {
        background: var(--accent, #18181b);
        color: var(--accent-text, #ffffff);
        border-color: transparent;
      }

      .map-stage {
        position: relative;
        inline-size: 100%;
        block-size: 100%;
        min-block-size: 260px;
        overflow: hidden;
        border-radius: 0.5rem;
        border: 1px solid var(--border-color, #cbd5e1);
        background: #e2e8f0;
        user-select: none;
        touch-action: none;
      }

      .map-stage[data-mode='select'] {
        cursor: crosshair;
      }

      .map-stage[data-mode='pan'] {
        cursor: grab;
      }

      .map-grid {
        position: absolute;
        inset: 0;
        inline-size: 100%;
        block-size: 100%;
        pointer-events: none;
      }

      .map-tile {
        position: absolute;
        inline-size: 256px;
        block-size: 256px;
        pointer-events: none;
      }

      .map-tile[hidden] {
        display: none;
      }

      .selection-box {
        position: absolute;
        border: 2px solid #2563eb;
        background: rgb(37 99 235 / 0.2);
        pointer-events: none;
      }

      .map-hint {
        font-size: 0.75rem;
        color: var(--text-muted, #475569);
      }
    `,
  ];

  @property({ type: Number })
  minLat?: number;

  @property({ type: Number })
  maxLat?: number;

  @property({ type: Number })
  minLng?: number;

  @property({ type: Number })
  maxLng?: number;

  @state()
  zoom = DEFAULT_ZOOM;

  @state()
  centerLat = DEFAULT_LAT;

  @state()
  centerLng = DEFAULT_LNG;

  @state()
  mode: 'select' | 'pan' = 'select';

  @state()
  viewW = MAP_VIEW_W;

  @state()
  viewH = MAP_VIEW_H;

  @state()
  private dragStart: Point | null = null;

  @state()
  private dragCurrent: Point | null = null;

  @state()
  private failedTiles = new Set<string>();

  private panStart: Point | null = null;
  private panOriginWorld: Point | null = null;
  private resizeObserver: ResizeObserver | null = null;

  override firstUpdated(): void {
    const stage = this.renderRoot.querySelector('.map-stage');
    this.syncStageDimensions(stage);
    if (stage && typeof ResizeObserver === 'function') {
      this.resizeObserver = new ResizeObserver(() => this.syncStageDimensions(stage));
      this.resizeObserver.observe(stage);
    }
  }

  override disconnectedCallback(): void {
    super.disconnectedCallback();
    this.resizeObserver?.disconnect();
    this.resizeObserver = null;
  }

  syncStageDimensions(stage: Element | null): void {
    if (!stage) return;
    const rect = stage.getBoundingClientRect();
    if (rect.width > 0 && rect.height > 0) {
      this.viewW = Math.round(rect.width);
      this.viewH = Math.round(rect.height);
    }
  }

  private getViewportOrigin(): Point {
    return {
      x: lngToWorldX(this.centerLng, this.zoom) - this.viewW / 2,
      y: latToWorldY(this.centerLat, this.zoom) - this.viewH / 2,
    };
  }

  private getStagePoint(e: PointerEvent): Point {
    const stage = e.currentTarget as HTMLElement;
    this.syncStageDimensions(stage);
    const rect = stage.getBoundingClientRect();
    return {
      x: clamp(e.clientX - rect.left, 0, this.viewW),
      y: clamp(e.clientY - rect.top, 0, this.viewH),
    };
  }

  private emitBounds(detail: GpsBoundsEventDetail): void {
    this.dispatchEvent(
      new CustomEvent<GpsBoundsEventDetail>('gps-bounds-change', {
        detail,
        bubbles: true,
        composed: true,
      }),
    );
  }

  private clearBounds(): void {
    this.dragStart = null;
    this.dragCurrent = null;
    this.emitBounds({ min_lat: undefined, max_lat: undefined, min_lng: undefined, max_lng: undefined });
  }

  private adjustZoom(delta: number): void {
    this.zoom = clamp(this.zoom + delta, MIN_ZOOM, MAX_ZOOM);
  }

  private resetView(): void {
    this.zoom = DEFAULT_ZOOM;
    this.centerLat = DEFAULT_LAT;
    this.centerLng = DEFAULT_LNG;
  }

  private onPointerDown(e: PointerEvent): void {
    if (e.button !== 0 && e.button !== 1) return;
    if (e.button === 1) {
      e.preventDefault();
    }
    const stage = e.currentTarget as HTMLElement;
    stage.setPointerCapture?.(e.pointerId);
    const pt = this.getStagePoint(e);
    if (e.button === 1 || this.mode === 'pan' || e.shiftKey) {
      this.panStart = pt;
      this.panOriginWorld = {
        x: lngToWorldX(this.centerLng, this.zoom),
        y: latToWorldY(this.centerLat, this.zoom),
      };
      this.dragStart = null;
      this.dragCurrent = null;
      return;
    }
    this.panStart = null;
    this.panOriginWorld = null;
    this.dragStart = pt;
    this.dragCurrent = pt;
  }

  private onPointerMove(e: PointerEvent): void {
    const pt = this.getStagePoint(e);
    if (this.panStart && this.panOriginWorld) {
      const worldSize = MAP_TILE_SIZE * 2 ** this.zoom;
      const nextX = clamp(this.panOriginWorld.x - (pt.x - this.panStart.x), 0, worldSize);
      const nextY = clamp(this.panOriginWorld.y - (pt.y - this.panStart.y), 0, worldSize);
      this.centerLng = worldXToLng(nextX, this.zoom);
      this.centerLat = worldYToLat(nextY, this.zoom);
      return;
    }
    if (this.dragStart) {
      this.dragCurrent = pt;
    }
  }

  private onPointerUp(e: PointerEvent): void {
    const stage = e.currentTarget as HTMLElement;
    stage.releasePointerCapture?.(e.pointerId);
    if (this.panStart) {
      this.panStart = null;
      this.panOriginWorld = null;
      return;
    }
    if (!this.dragStart || !this.dragCurrent) return;
    const start = this.dragStart;
    const end = this.dragCurrent;
    this.dragStart = null;
    this.dragCurrent = null;
    if (Math.abs(end.x - start.x) < 3 && Math.abs(end.y - start.y) < 3) return;
    const origin = this.getViewportOrigin();
    const lngA = worldXToLng(origin.x + Math.min(start.x, end.x), this.zoom);
    const lngB = worldXToLng(origin.x + Math.max(start.x, end.x), this.zoom);
    const latTop = worldYToLat(origin.y + Math.min(start.y, end.y), this.zoom);
    const latBottom = worldYToLat(origin.y + Math.max(start.y, end.y), this.zoom);
    this.emitBounds({
      min_lat: Math.min(latBottom, latTop),
      max_lat: Math.max(latBottom, latTop),
      min_lng: Math.min(lngA, lngB),
      max_lng: Math.max(lngA, lngB),
    });
  }

  private onPointerCancel(e: PointerEvent): void {
    const stage = e.currentTarget as HTMLElement;
    stage.releasePointerCapture?.(e.pointerId);
    this.panStart = null;
    this.panOriginWorld = null;
    this.dragStart = null;
    this.dragCurrent = null;
  }

  private onWheel(e: WheelEvent): void {
    e.preventDefault();
    this.adjustZoom(e.deltaY < 0 ? 1 : -1);
  }

  private handleTileError(key: string): void {
    if (this.failedTiles.has(key)) return;
    const next = new Set(this.failedTiles);
    next.add(key);
    this.failedTiles = next;
  }

  private computeVisibleTiles(origin: Point): TileSpec[] {
    const count = 2 ** this.zoom;
    const minTx = clamp(Math.floor(origin.x / MAP_TILE_SIZE), 0, count - 1);
    const maxTx = clamp(Math.floor((origin.x + this.viewW) / MAP_TILE_SIZE), 0, count - 1);
    const minTy = clamp(Math.floor(origin.y / MAP_TILE_SIZE), 0, count - 1);
    const maxTy = clamp(Math.floor((origin.y + this.viewH) / MAP_TILE_SIZE), 0, count - 1);
    const tiles: TileSpec[] = [];
    for (let ty = minTy; ty <= maxTy; ty += 1) {
      for (let tx = minTx; tx <= maxTx; tx += 1) {
        const key = `${this.zoom}/${tx}/${ty}`;
        tiles.push({
          key,
          x: tx,
          y: ty,
          left: Math.round(tx * MAP_TILE_SIZE - origin.x),
          top: Math.round(ty * MAP_TILE_SIZE - origin.y),
          src: `${OSM_TILE_BASE_URL}/${key}.png`,
        });
      }
    }
    return tiles;
  }

  private getSelectionRect(origin: Point): { left: number; top: number; width: number; height: number } | null {
    if (this.dragStart && this.dragCurrent) {
      return {
        left: Math.min(this.dragStart.x, this.dragCurrent.x),
        top: Math.min(this.dragStart.y, this.dragCurrent.y),
        width: Math.max(2, Math.abs(this.dragCurrent.x - this.dragStart.x)),
        height: Math.max(2, Math.abs(this.dragCurrent.y - this.dragStart.y)),
      };
    }
    if (
      this.minLat === undefined ||
      this.maxLat === undefined ||
      this.minLng === undefined ||
      this.maxLng === undefined ||
      Number.isNaN(this.minLat) ||
      Number.isNaN(this.maxLat) ||
      Number.isNaN(this.minLng) ||
      Number.isNaN(this.maxLng)
    ) {
      return null;
    }
    const x1 = lngToWorldX(this.minLng, this.zoom) - origin.x;
    const x2 = lngToWorldX(this.maxLng, this.zoom) - origin.x;
    const y1 = latToWorldY(this.maxLat, this.zoom) - origin.y;
    const y2 = latToWorldY(this.minLat, this.zoom) - origin.y;
    return {
      left: Math.min(x1, x2),
      top: Math.min(y1, y2),
      width: Math.max(2, Math.abs(x2 - x1)),
      height: Math.max(2, Math.abs(y2 - y1)),
    };
  }

  private renderToolbar() {
    return html`
      <div class="map-toolbar">
        <button type="button" class="btn" aria-pressed=${this.mode === 'select' ? 'true' : 'false'}
          @click=${() => { this.mode = 'select'; }}>⬚ Select Box</button>
        <button type="button" class="btn" aria-pressed=${this.mode === 'pan' ? 'true' : 'false'}
          @click=${() => { this.mode = 'pan'; }}>✋ Pan</button>
        <button type="button" class="btn" aria-label="Zoom in" @click=${() => this.adjustZoom(1)}>+</button>
        <button type="button" class="btn" aria-label="Zoom out" @click=${() => this.adjustZoom(-1)}>−</button>
        <button type="button" class="btn" aria-label="Reset map view" @click=${this.resetView}>Reset View</button>
        <button type="button" class="btn" aria-label="Clear GPS bounds" @click=${this.clearBounds}>Clear Box</button>
      </div>
    `;
  }

  private renderGrid(origin: Point) {
    return html`
      <svg class="map-grid" viewBox="0 0 ${this.viewW} ${this.viewH}" aria-hidden="true">
        ${GRID_LNGS.map((lng) => {
          const x = lngToWorldX(lng, this.zoom) - origin.x;
          return html`<line x1=${x} y1="0" x2=${x} y2=${this.viewH}
            stroke=${lng === 0 ? '#94a3b8' : '#cbd5e1'} stroke-width=${lng === 0 ? '1.2' : '0.8'} />`;
        })}
        ${GRID_LATS.map((lat) => {
          const y = latToWorldY(lat, this.zoom) - origin.y;
          return html`<line x1="0" y1=${y} x2=${this.viewW} y2=${y}
            stroke=${lat === 0 ? '#94a3b8' : '#cbd5e1'} stroke-width=${lat === 0 ? '1.2' : '0.8'} />`;
        })}
      </svg>
    `;
  }

  private renderStage() {
    const origin = this.getViewportOrigin();
    const tiles = this.computeVisibleTiles(origin);
    const box = this.getSelectionRect(origin);
    return html`
      <div
        class="map-stage"
        data-mode=${this.mode}
        aria-label="Interactive GPS bounding box map"
        @pointerdown=${this.onPointerDown}
        @pointermove=${this.onPointerMove}
        @pointerup=${this.onPointerUp}
        @pointercancel=${this.onPointerCancel}
        @wheel=${this.onWheel}
      >
        ${this.renderGrid(origin)}
        ${tiles.map(
          (t) => html`
            <img
              class="map-tile"
              src=${t.src}
              alt=""
              draggable="false"
              ?hidden=${this.failedTiles.has(t.key)}
              style="left:${t.left}px;top:${t.top}px"
              @error=${() => this.handleTileError(t.key)}
            />
          `,
        )}
        ${box
          ? html`<div
              class="selection-box"
              style="left:${box.left}px;top:${box.top}px;width:${box.width}px;height:${box.height}px"
            ></div>`
          : nothing}
      </div>
    `;
  }

  override render() {
    return html`
      ${this.renderToolbar()}
      ${this.renderStage()}
      <span class="map-hint">Left-drag to select area · Middle-drag (or Pan / Shift+drag) to move map · Scroll to zoom.</span>
    `;
  }
}

declare global {
  interface HTMLElementTagNameMap {
    'photo-gps-map-picker': PhotoGpsMapPicker;
  }
}

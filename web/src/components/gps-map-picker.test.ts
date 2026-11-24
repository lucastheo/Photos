import { beforeEach, describe, expect, it } from 'vitest';
import {
  latToWorldY,
  lngToWorldX,
  PhotoGpsMapPicker,
  worldXToLng,
  worldYToLat,
} from './gps-map-picker';
import './gps-map-picker';

describe('PhotoGpsMapPicker', () => {
  beforeEach(() => {
    document.body.innerHTML = '';
  });

  it('converts coordinates cleanly with Web Mercator helpers', () => {
    const x = lngToWorldX(-46.6333, 4);
    const y = latToWorldY(-23.5505, 4);
    expect(worldXToLng(x, 4)).toBeCloseTo(-46.6333, 3);
    expect(worldYToLat(y, 4)).toBeCloseTo(-23.5505, 3);
    expect(worldXToLng(-100, 1)).toBe(-180);
    expect(worldYToLat(-100, 1)).toBeCloseTo(85.0511, 4);
  });

  it('draws a selection box on drag and emits gps-bounds-change', async () => {
    const el = document.createElement('photo-gps-map-picker') as PhotoGpsMapPicker;
    document.body.appendChild(el);
    await el.updateComplete;

    const stage = el.shadowRoot?.querySelector<HTMLElement>('.map-stage');
    expect(stage).not.toBeNull();
    viMockStageRect(stage!);

    const events: Array<Record<string, number | undefined>> = [];
    el.addEventListener('gps-bounds-change', (e) => {
      events.push((e as CustomEvent<Record<string, number | undefined>>).detail);
    });

    stage!.dispatchEvent(new PointerEvent('pointerdown', { clientX: 80, clientY: 60, button: 0 }));
    stage!.dispatchEvent(new PointerEvent('pointermove', { clientX: 200, clientY: 160 }));
    await el.updateComplete;
    expect(el.shadowRoot?.querySelector('.selection-box')).not.toBeNull();

    stage!.dispatchEvent(new PointerEvent('pointerup', { clientX: 200, clientY: 160 }));
    await el.updateComplete;

    expect(events).toHaveLength(1);
    expect(events[0].min_lat).toBeLessThan(events[0].max_lat!);
    expect(events[0].min_lng).toBeLessThan(events[0].max_lng!);

    // Ignores tiny clicks (< 3px) and non-primary clicks
    stage!.dispatchEvent(new PointerEvent('pointerdown', { clientX: 10, clientY: 10, button: 2 }));
    stage!.dispatchEvent(new PointerEvent('pointerdown', { clientX: 100, clientY: 100, button: 0 }));
    stage!.dispatchEvent(new PointerEvent('pointerup', { clientX: 101, clientY: 101 }));
    // Canceling an active drag resets selection state without emitting
    stage!.dispatchEvent(new PointerEvent('pointerdown', { clientX: 50, clientY: 50, button: 0 }));
    stage!.dispatchEvent(new PointerEvent('pointermove', { clientX: 120, clientY: 120 }));
    stage!.dispatchEvent(new PointerEvent('pointercancel', { clientX: 120, clientY: 120 }));
    await el.updateComplete;
    expect(el.shadowRoot?.querySelector('.selection-box')).toBeNull();
    expect(events).toHaveLength(1);
    el.remove();
  });

  it('renders prop bounds, pans, zooms, resets view, handles tile errors, and clears box', async () => {
    const el = document.createElement('photo-gps-map-picker') as PhotoGpsMapPicker;
    el.minLat = -24;
    el.maxLat = -23;
    el.minLng = -47;
    el.maxLng = -46;
    document.body.appendChild(el);
    await el.updateComplete;

    expect(el.shadowRoot?.querySelector('.selection-box')).not.toBeNull();

    const stage = el.shadowRoot?.querySelector<HTMLElement>('.map-stage');
    const panBtn = Array.from(el.shadowRoot?.querySelectorAll('button') ?? []).find((b) =>
      b.textContent?.includes('Pan'),
    );
    const selectBtn = Array.from(el.shadowRoot?.querySelectorAll('button') ?? []).find((b) =>
      b.textContent?.includes('Select Box'),
    );
    panBtn?.click();
    await el.updateComplete;
    expect(stage?.dataset.mode).toBe('pan');

    const initialLng = el.centerLng;
    const initialLat = el.centerLat;
    stage?.dispatchEvent(new PointerEvent('pointerdown', { clientX: 160, clientY: 110, button: 0 }));
    stage?.dispatchEvent(new PointerEvent('pointermove', { clientX: 100, clientY: 80 }));
    stage?.dispatchEvent(new PointerEvent('pointerup', { clientX: 100, clientY: 80 }));
    await el.updateComplete;
    expect(el.centerLng).not.toBe(initialLng);
    expect(el.centerLat).not.toBe(initialLat);

    selectBtn?.click();
    await el.updateComplete;
    expect(stage?.dataset.mode).toBe('select');

    // Middle-click drag pans in select mode, suppresses autoscroll, and emits no selection box
    const beforeMiddleLng = el.centerLng;
    const beforeMiddleLat = el.centerLat;
    const middleEvents: unknown[] = [];
    const onBounds = (e: Event) => middleEvents.push(e);
    el.addEventListener('gps-bounds-change', onBounds);
    const middleDown = new PointerEvent('pointerdown', {
      clientX: 180,
      clientY: 140,
      button: 1,
      cancelable: true,
    });
    stage?.dispatchEvent(middleDown);
    expect(middleDown.defaultPrevented).toBe(true);
    stage?.dispatchEvent(new PointerEvent('pointermove', { clientX: 120, clientY: 100 }));
    stage?.dispatchEvent(new PointerEvent('pointerup', { clientX: 120, clientY: 100, button: 1 }));
    el.removeEventListener('gps-bounds-change', onBounds);
    await el.updateComplete;
    expect(el.centerLng).not.toBe(beforeMiddleLng);
    expect(el.centerLat).not.toBe(beforeMiddleLat);
    expect(middleEvents).toHaveLength(0);

    // Shift-drag also pans while in select mode
    const beforeShiftLng = el.centerLng;
    stage?.dispatchEvent(
      new PointerEvent('pointerdown', { clientX: 100, clientY: 100, button: 0, shiftKey: true }),
    );
    stage?.dispatchEvent(new PointerEvent('pointermove', { clientX: 140, clientY: 100 }));
    stage?.dispatchEvent(new PointerEvent('pointerup', { clientX: 140, clientY: 100 }));
    expect(el.centerLng).not.toBe(beforeShiftLng);

    const zoomIn = el.shadowRoot?.querySelector<HTMLButtonElement>('button[aria-label="Zoom in"]');
    const zoomOut = el.shadowRoot?.querySelector<HTMLButtonElement>('button[aria-label="Zoom out"]');
    const resetBtn = el.shadowRoot?.querySelector<HTMLButtonElement>('button[aria-label="Reset map view"]');
    zoomIn?.click();
    await el.updateComplete;
    expect(el.zoom).toBe(3);

    stage?.dispatchEvent(new WheelEvent('wheel', { deltaY: -100 }));
    expect(el.zoom).toBe(4);
    stage?.dispatchEvent(new WheelEvent('wheel', { deltaY: 100 }));
    expect(el.zoom).toBe(3);
    zoomOut?.click();
    expect(el.zoom).toBe(2);

    resetBtn?.click();
    await el.updateComplete;
    expect(el.zoom).toBe(2);
    expect(el.centerLat).toBe(15);
    expect(el.centerLng).toBe(0);

    const tile = el.shadowRoot?.querySelector<HTMLImageElement>('.map-tile');
    expect(tile).not.toBeNull();
    tile?.dispatchEvent(new Event('error'));
    await el.updateComplete;
    expect(tile?.hasAttribute('hidden')).toBe(true);
    tile?.dispatchEvent(new Event('error'));

    const cleared: Array<Record<string, number | undefined>> = [];
    el.addEventListener('gps-bounds-change', (e) => {
      cleared.push((e as CustomEvent<Record<string, number | undefined>>).detail);
    });
    el.shadowRoot?.querySelector<HTMLButtonElement>('button[aria-label="Clear GPS bounds"]')?.click();
    expect(cleared).toEqual([
      { min_lat: undefined, max_lat: undefined, min_lng: undefined, max_lng: undefined },
    ]);
  });
});

function viMockStageRect(stage: HTMLElement): void {
  stage.getBoundingClientRect = () =>
    ({
      left: 0,
      top: 0,
      right: 960,
      bottom: 560,
      width: 960,
      height: 560,
      x: 0,
      y: 0,
      toJSON: () => ({}),
    }) as DOMRect;
}

export const DEFAULT_DWELL_MS = 1500;

export interface DwellTriggerEvent {
  photoId: string;
  viewId: string;
  source: 'dwell' | 'slideshow';
}

export interface DwellTimerOptions {
  dwellMs?: number;
  getVisibilityState?: () => DocumentVisibilityState;
  generateViewId?: () => string;
}

function defaultViewId(): string {
  if (typeof crypto !== 'undefined' && typeof crypto.randomUUID === 'function') {
    return crypto.randomUUID();
  }
  // Non-crypto fallback is fine: view_id is only an idempotency key, not a secret.
  return `view-${Date.now()}-${Math.random().toString(16).slice(2, 10)}`;
}

function defaultVisibilityState(): DocumentVisibilityState {
  return typeof document !== 'undefined' ? document.visibilityState : 'visible';
}

export class DwellTimer {
  private readonly onTrigger: (ev: DwellTriggerEvent) => void;
  private readonly dwellMs: number;
  private readonly getVisibilityState: () => DocumentVisibilityState;
  private readonly generateViewId: () => string;

  private currentPhotoId: string | null = null;
  private currentViewId: string | null = null;
  private currentSource: 'dwell' | 'slideshow' = 'dwell';
  private mediaLoaded = false;
  private countedForCurrentOpen = false;
  private timerId: ReturnType<typeof setTimeout> | null = null;

  constructor(onTrigger: (ev: DwellTriggerEvent) => void, options: DwellTimerOptions = {}) {
    this.onTrigger = onTrigger;
    this.dwellMs = options.dwellMs ?? DEFAULT_DWELL_MS;
    this.getVisibilityState = options.getVisibilityState ?? defaultVisibilityState;
    this.generateViewId = options.generateViewId ?? defaultViewId;
  }

  openPhoto(photoId: string, source: 'dwell' | 'slideshow' = 'dwell'): void {
    this.clearTimer();
    this.currentPhotoId = photoId;
    this.currentViewId = this.generateViewId();
    this.currentSource = source;
    this.mediaLoaded = false;
    this.countedForCurrentOpen = false;
  }

  notifyMediaLoaded(photoId: string): void {
    if (photoId !== this.currentPhotoId || this.countedForCurrentOpen) {
      return;
    }
    this.mediaLoaded = true;
    if (this.getVisibilityState() === 'visible' && this.timerId === null) {
      this.startTimer();
    }
  }

  switchVariant(photoId: string): void {
    if (photoId !== this.currentPhotoId) {
      this.openPhoto(photoId);
      return;
    }
    this.clearTimer();
    this.mediaLoaded = false;
  }

  handleVisibilityChange(): void {
    if (this.getVisibilityState() !== 'visible') {
      this.clearTimer();
      return;
    }
    if (
      this.currentPhotoId !== null &&
      this.mediaLoaded &&
      !this.countedForCurrentOpen &&
      this.timerId === null
    ) {
      this.startTimer();
    }
  }

  cancel(): void {
    this.clearTimer();
    this.currentPhotoId = null;
    this.currentViewId = null;
    this.mediaLoaded = false;
    this.countedForCurrentOpen = false;
  }

  isCounted(): boolean {
    return this.countedForCurrentOpen;
  }

  isPending(): boolean {
    return this.timerId !== null;
  }

  getPhotoId(): string | null {
    return this.currentPhotoId;
  }

  getViewId(): string | null {
    return this.currentViewId;
  }

  private startTimer(): void {
    const photoId = this.currentPhotoId;
    const viewId = this.currentViewId;
    const source = this.currentSource;
    if (!photoId || !viewId) {
      return;
    }
    this.timerId = setTimeout(() => {
      this.timerId = null;
      if (this.currentPhotoId !== photoId || this.countedForCurrentOpen) {
        return;
      }
      if (this.getVisibilityState() !== 'visible') {
        return;
      }
      this.countedForCurrentOpen = true;
      this.onTrigger({ photoId, viewId, source });
    }, this.dwellMs);
  }

  private clearTimer(): void {
    if (this.timerId !== null) {
      clearTimeout(this.timerId);
      this.timerId = null;
    }
  }
}

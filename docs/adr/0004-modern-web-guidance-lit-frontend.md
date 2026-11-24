# ADR 0004: Frontend Architecture with Lit Web Components & Modern Web Guidance

- **Status:** Accepted
- **Date:** 2026-11-17

## Context

The frontend needs fast gallery rendering, smooth thumbnail-to-lightbox transitions, accessible modals/popovers, and single-binary deployment inside the Go server while following `GoogleChrome/modern-web-guidance`.

## Decision

1. **TypeScript + Vite + Lit (Native Web Components):**
   - Build the UI in `web/` using Lit and native browser APIs championed by `GoogleChrome/modern-web-guidance`:
     - Native `<dialog>` sized full-viewport (`100dvw × 100dvh`) with uncropped `object-fit: contain` media scaling and floating HUD overlays (toolbar + details drawer) revealed on pointer movement (`2.2s` idle fade-out) for distraction-free viewing.
     - **View Transitions API** (`document.startViewTransition` with `prefers-reduced-motion` guard) for expanding thumbnails and navigating photos.
     - Native **Popover API** for filter/emoji popovers and responsive CSS Grid with uncropped `object-fit: contain` thumbnails and `loading="lazy"` / `decoding="async"`.
2. **1.5s Dwell Timer & Stable Browsing Queue:**
   - Start the 1.5s view timer only after `<img>` `load` or `<video>` `loadeddata` while `document.visibilityState === 'visible'`.
   - Update viewed photo badges (`👁 0` → `👁 1`) in-place during the active browsing session without removing cards from the open Gallery/Lightbox queue.
3. **Single-Binary Embedding (`web/embed.go`):**
   - Expose `web/dist` as an `fs.FS` via `web/embed.go` and pass it into `internal/adapter/httpapi`.

## Consequences

- Minimal bundle size, native browser accessibility, zero virtual-DOM friction with `<dialog>`/View Transitions, and a single self-contained binary.

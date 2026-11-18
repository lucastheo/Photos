# Photo View Tracker — Agent & Contributor Rulebook (`AGENTS.md`)

Read this file and [docs/SPECIFICATION.md](file:///home/theo/Projects/Photos/docs/SPECIFICATION.md) before writing or modifying any code in this repository.

---

## 1. Non-Negotiable Architectural Rules (Go Backend)

1. **Strict 3-Struct Separation Across Layers:**
   - **HTTP Request/Response DTOs (`internal/adapter/httpapi`):** Used **only** at the HTTP boundary (`json:"..."` tags live here). Converted explicitly to/from `domain` structs in `internal/adapter/httpapi/mapper.go`.
   - **Pure Domain Objects (`internal/domain`):** Pure Go structs with **zero** struct tags (`no json`, `no db`) and **zero** interfaces. Imports Go standard library (`$gostd`) only.
   - **Database Entities (`internal/adapter/sqlite`):** Used **only** inside `internal/adapter/sqlite` (`db:"..."` / SQL scan targets). Converted explicitly to/from `domain` structs in `internal/adapter/sqlite/mapper.go`.
2. **"Accept Interfaces, Return Structs" (`ireturn` & `depguard`):**
   - **Define interfaces only in the consuming package** (`internal/usecase` defines store/scanner interfaces; `internal/adapter/httpapi` defines use-case interfaces).
   - **Never define interfaces in producer packages or in `internal/domain`.**
   - **Every constructor (`New...`) and function must return a concrete struct** (e.g., `*PhotoRepository`, `*TrackViewUseCase`, `*PhotoHandler`), never an interface (enforced by `ireturn` in `.golangci.yml`).
   - Package import boundaries are enforced by `depguard`: `httpapi`, `sqlite`, `media`, and `usecase` never import each other directly—only `internal/domain`.

---

## 2. Core Domain Invariants

- **`LIBRARY_PATH` is 100% Read-Only:** Never modify, rename, delete, or write sidecar files into `LIBRARY_PATH`. `DB_PATH` (`./data/photos.db`) and `CACHE_PATH` (`./data/cache`) live outside `LIBRARY_PATH`.
- **1 Logical `Photo` → Many Physical `PhotoFile` Rows:**
  - `photos` holds `view_count`, `last_viewed_unix_ns`, `photo_views`, `photo_tags`, and EXIF metadata.
  - `photo_files` holds physical disk paths (`relative_path` NFC-normalized, `content_hash` SHA-256).
  - **Exact Duplicates** (same `SHA-256` in multiple folders) and **`RAW + JPEG` Still-Photo Pairs** (same folder + lowercase `base_name`, `media_type == 'photo'`, never stacking video with photo) attach to the same logical `Photo`.
  - **Default Stack Priority:** `CR3 (RAW) > HEIC > JPG/PNG/WebP`.
  - **In-Place Edits:** Same `relative_path` with a changed `SHA-256` updates the existing `photo_files` row in place, preserving `photo_id`, `view_count`, and emoji tags.
- **View Counting & History Synchronization:**
  - Automatic view count (`+1`) triggers only after **≥ 1.5s dwell time** in the expanded Lightbox after media load while `document.visibilityState === 'visible'` (idempotent via `view_id`).
  - Manual adjustments (`increment`, `decrement`, `reset`) keep `photos.view_count` and `photo_views` strictly synchronized (`view_count == COUNT(photo_views)`).
- **Emoji Tagging (`AND` Filter):**
  - Emojis are normalized (NFC, canonicalized without trailing `U+FE0F`) and validated in `internal/domain`.
  - Multi-emoji search (`emoji_tags=❤️,🏖️`) uses **`AND` semantics**, plus `untagged=true` for untagged photos.

---

## 3. Frontend Rules (`web/`)

- Stack: **TypeScript + Vite + Lit (Native Web Components)** embedded via `web/embed.go`.
- Follow **[GoogleChrome/modern-web-guidance](https://github.com/GoogleChrome/modern-web-guidance)**:
  - Use native `<dialog>` for the expanded Lightbox, **View Transitions API** (with `prefers-reduced-motion` fallback), native **Popover API**, and responsive CSS Grid.
  - Keep viewed cards in-place during the active Gallery/Lightbox browsing session (updating their badge `👁 0` → `👁 1` live without shifting the queue).

---

## 4. Quality Gates & Limits (Must Pass Before Completing Any Task)

- **File & Function Size (`make lint-max-file`):**
  - Max **1,000 lines per file** across `.go`, `.ts`, and `.css`.
  - Max **60 lines per function** in production code (`*_test.go` excluded for table-driven tests).
- **Linters & GoDoc (`make lint`):**
  - Every Go package must have `doc.go` and every exported symbol must have a GoDoc comment (`revive: exported, package-comments`).
- **Test Coverage (`make test-coverage`):**
  - Strictly **> 90.0%** statement/line coverage required overall and per layer (`domain`, `usecase`, `adapter/httpapi`, `adapter/sqlite`, `adapter/media`, and `web/`).
- **End-to-End Verification (`make test-e2e` & `make check`):**
  - Standalone black-box Playwright suite in `e2e/` must pass all core user journeys.

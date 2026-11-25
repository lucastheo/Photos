# Photo View Tracker — Feature Guide

Photo View Tracker is a single-user, self-hosted viewer for personal photo and video libraries. All media inside `LIBRARY_PATH` stays **100% read-only**—no files are modified, moved, or annotated with sidecars. View counts, timestamped view history, emoji tags, and extracted metadata live outside your media tree in a portable SQLite database (`DB_PATH`).

---

## 1. Automatic & Manual View Counting

- **Gallery Scrolling Never Counts:** Browsing thumbnails in the gallery grid never increments `view_count`.
- **1.5-Second Expanded Lightbox Dwell:** Clicking a photo or video opens the full-viewport `<dialog>` Lightbox (`/photo/:id`). A **1.5-second (`1500ms`) dwell timer** starts only after the media finishes loading (`<img>` `load` or `<video>` `loadeddata`) while `document.visibilityState === 'visible'`.
  - Navigating away before 1.5s, closing the Lightbox, or hiding/switching the browser tab cancels the timer so quick skimming never inflates counts.
  - Each view submission sends a client-generated `view_id` UUID to `POST /api/v1/photos/{id}/views` for idempotency so retries cannot double-count.
- **Slideshow Mode:** Toggle **▶ Slideshow** (or press `Space`) in the Lightbox toolbar with a selectable interval (`3s`, `4s`, `6s`, `10s`). Slideshow advances automatically across the current filtered queue and records views marked with `source = 'slideshow'`.
- **Manual Adjustments Synchronized with History:** Inside the Lightbox Details drawer (`View Counter` card), you can click `+1`, `-1`, or `Reset to 0`:
  - `+1` appends a new `photo_views` entry (`source = 'manual'`) and increments `view_count`.
  - `-1` removes the most recent `photo_views` entry, decrements `view_count` (clamped at `0`), and rolls back `last_viewed_at` to the previous timestamp (or clears it if `0`).
  - `Reset to 0` deletes all `photo_views` entries for that photo and resets `view_count` to `0`.
- **Stable Active Browsing Queue:** When a photo transitions from `0 → 1` views while you are filtering by `Never Seen (0)`, its badge updates live in-place (`👁 0` → `👁 1`) without disappearing or shifting cards under your cursor until you change filters or reload.

---

## 2. `RAW + JPEG` Stacking

- **Automatic Still-Photo Grouping:** When a camera writes both a RAW image and a standard image in the same folder with the same case-insensitive base filename (e.g., `2026/Vacation/Beach_Sunset.CR3` and `2026/Vacation/Beach_Sunset.jpg`), both physical files stack under **one logical `Photo`** (`stack_key = folder/photo/lower(base_name)`).
- **Videos Never Stack with Still Photos:** Video files (such as `.MOV` or `.MP4` companion clips) always remain separate `media_type = 'video'` items.
- **Default Variant Priority (`RAW > HEIC > Standard`):** Within a stack, RAW formats (e.g., `.CR3`, `.CR2`, `.DNG`, `.NEF`, `.ARW`, `.ORF`, `.RW2`) take priority over `.HEIC`/`.HEIF`, which take priority over standard images (`.JPG`, `.PNG`, `.WebP`, `.AVIF`, `.GIF`) for thumbnails, default previews, and EXIF extraction.
- **Lightbox Variant Switcher:** Stacked items show a format badge such as `CR3 + JPG` in the gallery and a variant toggle (`CR3` | `JPG`) in the Lightbox toolbar so you can inspect or download either variant. Switching variants does not trigger an extra view count.
- **Split-Stack Rule on Move:** If you move one file of a stacked pair to a different folder on disk and run Re-import, the remaining file keeps the existing `Photo` record (`view_count`, `photo_views`, `emoji_tags`), while the moved file splits into a new standalone `Photo` initialized with a copy of the parent photo's `view_count` and `emoji_tags`.

---

## 3. Exact Duplicate Tracking

- **Shared Logical Photo by `SHA-256`:** If the exact same file bytes exist in multiple folders (for example, `2026/Vacation/Mountain_Lake.jpg` and `2026/Backup/Mountain_Lake_Copy.jpg`), both `photo_files` rows link to the same logical `Photo`.
- **Unified Counters & Tags:** Viewing or emoji-tagging the photo updates the shared `view_count`, `photo_views` history, and `emoji_tags` across all copies.
- **Duplicate Files Inspector:** The `/library` view lists every exact `SHA-256` duplicate group (`> 1` active disk path) along with file paths and sizes in the **Duplicate Files Inspector** table.

---

## 4. Emoji Tagging & `AND` Filtering

- **Canonical Unicode Normalization:** Tags are normalized to Unicode NFC and stripped of trailing variation selectors (`U+FE0F`) so variants like `❤` and `❤️` never create duplicate tags, while plain ASCII text is rejected.
- **Quick Tagging:** Tag items in the Lightbox via the **🏷️ Tag** toolbar popover or the **Emoji Tags** drawer card (using 1-click chips `❤️`, `⭐`, `🔥`, `🏖️`, `😂`, `🎉`, `🗑️` or the custom emoji input).
- **Multi-Emoji `AND` Filter + `Untagged`:** The gallery filter bar lists all active emoji tags with photo counts. Selecting multiple emojis applies **`AND` intersection** (returning items that contain every selected emoji), and the `Untagged` chip matches items with zero tags.
- **Preserved Across Moves & Edits:** Because tags attach to the logical `Photo` ID, moving, renaming, or editing EXIF in-place preserves all emoji tags.

---

## 5. Search, Sorting & Interactive GPS Filtering

- **View Count Presets & Ranges:** Filter by `All`, `Never Seen (0)`, `Seen (1+)`, or custom `Min` and `Max` view bounds.
- **Media Type & Format Pills:** Filter by `All Media`, `Photos`, or `Videos`, and combine with format pills (`CR3`, `HEIC`, `JPG`, `PNG`, `WEBP`, `MP4`, `MOV`).
- **Path, Folder & Date Range:** Search filenames/paths (`q`), narrow by `Folder` (including nested subfolders), and filter by capture date range (`Start date` – `End date`).
- **Sorting & Seeded Random Discovery:** Sort by `Least Viewed First` (default), `Most Viewed First`, `Date Taken (Newest/Oldest)`, `Last Viewed (Recent/Oldest)`, or `Random Discovery`. Random Discovery uses a deterministic session `seed` so pagination never repeats items, with a **🎲 Reshuffle** button for a fresh order.
- **GPS Location & Interactive Map Bounds:**
  - Toggle `All Locations`, `📍 Has GPS`, or `No GPS`.
  - Click **GPS Bounds** to open the native popover containing an interactive Mercator map picker (drag to draw a bounding box, middle-drag / Pan mode / Shift+drag to pan, scroll or `+`/`−` to zoom, or type `Min/Max Lat/Lng` bounds).
  - Inside the Lightbox Details drawer, geotagged photos display decimal coordinates with **Filter Nearby Photos** (applies a `±0.05°` bounding box around that photo) and **Open Map** (opens OpenStreetMap).
- **URL State Persistence:** Active filters and sort options synchronize to query parameters in the browser URL.

---

## 6. Incremental 4-Step Re-import / Sync & Missing Files

Trigger **↻ Re-import** in the header or **Re-import / Sync Library** in `/library` (`POST /api/v1/library/sync`) whenever files are added, moved, edited, or removed on disk:

1. **Unmounted Drive Protection:** If `LIBRARY_PATH` is unreadable or contains `0` valid media files while the database already holds active records, sync aborts safely without marking your library as missing.
2. **Step 1 — Directory Walk & Fast Metadata Skip:** Walks `LIBRARY_PATH` (skipping hidden `.*` directories/files, `@eaDir`, and `0`-byte files) with NFC-normalized paths. Files whose `(relative_path, file_size, mod_time_unix_ns)` match an active database record are skipped with zero bytes hashed. Previously `missing` files found again are reactivated.
3. **Step 2 — Bounded Parallel `SHA-256` Hashing:** Only new or modified files are hashed in parallel (`SYNC_WORKERS`).
4. **Step 3 — Transactional Reconciliation:**
   - **In-Place Edit** (same `relative_path`, new `SHA-256`): Updates hash/size/mtime in place and regenerates previews while preserving `photo_id`, `view_count`, and `emoji_tags`.
   - **Moved / Renamed File** (new `relative_path`, known `SHA-256` matching an unseen record): Relinks the `photo_files` row via a collision-safe two-step transaction, preserving `view_count`, `photo_views`, and `emoji_tags`.
   - **Exact Duplicate / Stack / New Item:** Attaches duplicates and `RAW + JPEG` variants or creates new `Photo` records (`view_count = 0`), extracts EXIF metadata, and warms the preview cache.
5. **Step 4 — Missing Files & Safe Purge:** Unseen paths are marked `status = 'missing'` (a `Photo` becomes `missing` only when all its physical files are missing). Clicking **Purge Missing Records** in `/library` (`DELETE /api/v1/library/missing`) creates a timestamped SQLite backup before deleting missing rows. Sync completion also runs `PRAGMA wal_checkpoint(TRUNCATE)`.

---

## 7. Lightbox Experience, Media Formats & Orientation

- **Supported Formats:**
  - Standard Images: `.jpg`, `.jpeg`, `.png`, `.webp`, `.avif`, `.gif`
  - RAW Photos: `.cr3`, `.cr2`, `.dng`, `.nef`, `.arw`, `.orf`, `.rw2`
  - High-Efficiency Photos: `.heic`, `.heif`
  - Videos: `.mp4`, `.mov`, `.webm`, `.mkv`, `.m4v`
- **Two-Tier Preview Cache (`CACHE_PATH`):** Generates atomic `400px` JPEG grid thumbnails (`<sha256>_thumb.jpg`) and `2560px` web previews (`<sha256>_preview.jpg`) for RAW and HEIC files under bounded `PREVIEW_WORKERS`. Standard browser images and videos stream from disk with `ETag`, `Cache-Control`, and **HTTP Range (`206 Partial Content`)** seeking.
- **Format-Aware EXIF Auto-Orientation:** EXIF `Orientation` (`1..8`) is applied during thumbnail/preview generation for RAW and JPEG files and normalized into post-rotation `width` and `height`. `.HEIC`/`.HEIF` outputs from `heif-convert` already honor container rotation and skip duplicate EXIF rotation.
- **Distraction-Free HUD & Keyboard Shortcuts:** The floating toolbar and Details drawer fade out after `2.2s` of pointer inactivity (`cursor: none` on the stage) and reappear smoothly on pointer movement.
  - `←` / `→`: Previous / Next photo in the active gallery queue
  - `Space`: Play / Pause slideshow
  - `i`: Toggle Details drawer
  - `Esc`: Close Lightbox
  - **Zoom / Fit** button toggles full-size scrolling vs. uncropped viewport fit (`object-fit: contain`).

---

## 8. Single-User Authentication & Security Hardening

- **Optional Login Wall:** Set `AUTH_DISABLED=true` for frictionless local usage (`HOST=127.0.0.1`), or enable password authentication (`AUTH_DISABLED=false`) using `AUTH_PASSWORD` or a `SHA-256` hex digest in `AUTH_PASSWORD_HASH`.
- **Persistent Signed Sessions:** Successful login sets an `HttpOnly`, `SameSite=Lax` cookie (`pt_session`, 30-day TTL) signed with HMAC-SHA256 (`AUTH_SECRET`, or an auto-generated 32-byte secret persisted in SQLite `app_settings` across restarts).
- **Defense in Depth:**
  - Token-bucket rate limiting on `POST /api/v1/auth/login` (`429 Too Many Requests` after repeated failures).
  - Cross-origin protection (`http.CrossOriginProtection`) on mutating API calls (`POST`, `PATCH`, `DELETE`) even when `AUTH_DISABLED=true`.
  - Go `os.Root` directory confinement on all reads inside `LIBRARY_PATH` and `CACHE_PATH`, plus direct `exec.CommandContext` subprocess execution with a `15s` timeout.

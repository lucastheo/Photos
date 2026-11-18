# ADR 0002: SHA-256 Identity, Exact Duplicates, RAW+JPEG Stacking & 4-Step Sync

- **Status:** Accepted
- **Date:** 2026-11-17

## Context

Users reorganize folders, rename files, edit EXIF metadata in place, keep backup copies in multiple folders, and shoot Canon `RAW + JPEG` (`.CR3` + `.JPG`) pairs. Identifying photos purely by file path loses view history when files move; identifying purely by `UNIQUE(content_hash)` on a single table fails on duplicate files, in-place edits, and multi-file `RAW + JPEG` camera shots.

## Decision

1. **1 Logical `Photo` (`photos`) → Many Physical `PhotoFile` (`photo_files`) Model:**
   - `photos` (UUID/ULID primary key) owns `view_count`, `photo_views`, `photo_tags`, and EXIF metadata.
   - `photo_files` owns physical file records (`relative_path UNIQUE`, non-unique `content_hash` SHA-256).
2. **Exact Duplicates & `RAW + JPEG` Stacking:**
   - Files with identical `SHA-256` across folders attach to the same `Photo` (and are surfaced in `/library` Duplicates).
   - Still photos (`media_type == 'photo'`) sharing `(folder_path, lower(base_name))` stack under one `Photo` (`stack_key`), with **`CR3 (RAW) > HEIC > JPG`** as the default preview priority. Videos (`.MOV` Live Photos) never stack with still photos.
3. **Deterministic 4-Step Sync Pipeline:**
   - Abort if `LIBRARY_PATH` is unreadable/empty when active records exist.
   - Walk all paths first → hash changed/new files in a bounded I/O pool → reconcile in a single transaction (in-place edits update hash at the same path; moves update `relative_path` in two steps; duplicates attach to existing `Photo`) → mark remaining unseen rows `missing`.

## Consequences

- View counts and emoji tags survive renames, moves, and in-place EXIF edits.
- `LIBRARY_PATH` remains 100% read-only.

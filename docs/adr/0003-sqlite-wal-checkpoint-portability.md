# ADR 0003: Portable SQLite Configuration, DSN Pragmas & Pre-Migration Backups

- **Status:** Accepted
- **Date:** 2026-11-17

## Context

The SQLite database stores irreplaceable personal data (`view_count` history and emoji tags) and must be easy to back up or share across machines without CGO compilation issues, `SQLITE_BUSY` errors, or broken foreign keys.

## Decision

1. **Pure-Go Driver & Per-Connection DSN Pragmas:**
   - Use `modernc.org/sqlite` with pragmas passed in the DSN so every pooled connection inherits them:
     `?_pragma=journal_mode(WAL)&_pragma=synchronous(NORMAL)&_pragma=foreign_keys(1)&_pragma=busy_timeout(10000)`
2. **Paths Outside `LIBRARY_PATH` + Relative Media Paths:**
   - Default `DB_PATH` (`./data/photos.db`) and `CACHE_PATH` (`./data/cache`) outside `LIBRARY_PATH` so the photo library can be mounted read-only (`/photos:ro`), while storing all media paths inside SQLite relative to `LIBRARY_PATH` (normalized to Unicode NFC).
3. **WAL Auto-Checkpoint & Pre-Migration / Pre-Purge Backups:**
   - Run `PRAGMA wal_checkpoint(TRUNCATE)` after sync completion and on graceful shutdown (`SIGINT`/`SIGTERM`).
   - Automatically copy `photos.db` to a timestamped `.bak` file before running any pending schema migration or `DELETE /api/v1/library/missing`.

## Consequences

- Zero CGO dependencies, safe concurrent access, and automatic protection against accidental data loss during upgrades or purges.

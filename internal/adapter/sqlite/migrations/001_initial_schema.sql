CREATE TABLE IF NOT EXISTS photos (
    id                  TEXT PRIMARY KEY,
    stack_key           TEXT NOT NULL,
    base_name           TEXT NOT NULL,
    folder_path         TEXT NOT NULL,
    media_type          TEXT NOT NULL,
    width               INTEGER NOT NULL DEFAULT 0,
    height              INTEGER NOT NULL DEFAULT 0,
    orientation         INTEGER NOT NULL DEFAULT 1,
    duration_sec        REAL NOT NULL DEFAULT 0,
    taken_at_unix       INTEGER,
    camera_make         TEXT,
    camera_model        TEXT,
    lens_model          TEXT,
    iso                 INTEGER,
    f_number            REAL,
    exposure_time       TEXT,
    focal_length_mm     REAL,
    gps_latitude        REAL,
    gps_longitude       REAL,
    view_count          INTEGER NOT NULL DEFAULT 0 CHECK (view_count >= 0),
    last_viewed_unix_ns INTEGER,
    status              TEXT NOT NULL DEFAULT 'active',
    created_at_unix     INTEGER NOT NULL,
    updated_at_unix     INTEGER NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_photos_stack_key ON photos(stack_key);
CREATE INDEX IF NOT EXISTS idx_photos_status_views ON photos(status, view_count);
CREATE INDEX IF NOT EXISTS idx_photos_taken_at ON photos(taken_at_unix DESC);
CREATE INDEX IF NOT EXISTS idx_photos_folder ON photos(folder_path);
CREATE INDEX IF NOT EXISTS idx_photos_gps ON photos(gps_latitude, gps_longitude) WHERE gps_latitude IS NOT NULL;

CREATE TABLE IF NOT EXISTS photo_files (
    id               TEXT PRIMARY KEY,
    photo_id         TEXT NOT NULL REFERENCES photos(id) ON DELETE CASCADE,
    content_hash     TEXT NOT NULL,
    relative_path    TEXT NOT NULL UNIQUE,
    file_name        TEXT NOT NULL,
    folder_path      TEXT NOT NULL,
    format           TEXT NOT NULL,
    file_size        INTEGER NOT NULL,
    mod_time_unix_ns INTEGER NOT NULL,
    status           TEXT NOT NULL DEFAULT 'active',
    created_at_unix  INTEGER NOT NULL,
    updated_at_unix  INTEGER NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_photo_files_photo_id ON photo_files(photo_id);
CREATE INDEX IF NOT EXISTS idx_photo_files_hash ON photo_files(content_hash);
CREATE INDEX IF NOT EXISTS idx_photo_files_format ON photo_files(format);

CREATE TABLE IF NOT EXISTS photo_views (
    id                INTEGER PRIMARY KEY AUTOINCREMENT,
    view_id           TEXT UNIQUE,
    photo_id          TEXT NOT NULL REFERENCES photos(id) ON DELETE CASCADE,
    viewed_at_unix_ns INTEGER NOT NULL,
    source            TEXT NOT NULL DEFAULT 'dwell'
);

CREATE INDEX IF NOT EXISTS idx_photo_views_photo_id ON photo_views(photo_id, viewed_at_unix_ns DESC);

CREATE TABLE IF NOT EXISTS photo_tags (
    photo_id        TEXT NOT NULL REFERENCES photos(id) ON DELETE CASCADE,
    emoji           TEXT NOT NULL,
    created_at_unix INTEGER NOT NULL,
    PRIMARY KEY (photo_id, emoji)
);

CREATE INDEX IF NOT EXISTS idx_photo_tags_emoji ON photo_tags(emoji, photo_id);

CREATE TABLE IF NOT EXISTS sync_runs (
    id               TEXT PRIMARY KEY,
    started_at_unix  INTEGER NOT NULL,
    finished_at_unix INTEGER,
    status           TEXT NOT NULL,
    scanned_count    INTEGER NOT NULL DEFAULT 0,
    added_count      INTEGER NOT NULL DEFAULT 0,
    updated_count    INTEGER NOT NULL DEFAULT 0,
    moved_count      INTEGER NOT NULL DEFAULT 0,
    stacked_count    INTEGER NOT NULL DEFAULT 0,
    duplicate_count  INTEGER NOT NULL DEFAULT 0,
    unchanged_count  INTEGER NOT NULL DEFAULT 0,
    missing_count    INTEGER NOT NULL DEFAULT 0,
    error_message    TEXT
);

CREATE TABLE IF NOT EXISTS app_settings (
    key             TEXT PRIMARY KEY,
    value           TEXT NOT NULL,
    updated_at_unix INTEGER NOT NULL
);

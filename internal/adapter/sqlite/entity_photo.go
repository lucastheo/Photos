package sqlite

import "database/sql"

type photoEntity struct {
	ID               string          `db:"id"`
	StackKey         string          `db:"stack_key"`
	BaseName         string          `db:"base_name"`
	FolderPath       string          `db:"folder_path"`
	MediaType        string          `db:"media_type"`
	Width            int             `db:"width"`
	Height           int             `db:"height"`
	Orientation      int             `db:"orientation"`
	DurationSec      float64         `db:"duration_sec"`
	TakenAtUnix      sql.NullInt64   `db:"taken_at_unix"`
	CameraMake       sql.NullString  `db:"camera_make"`
	CameraModel      sql.NullString  `db:"camera_model"`
	LensModel        sql.NullString  `db:"lens_model"`
	ISO              sql.NullInt64   `db:"iso"`
	FNumber          sql.NullFloat64 `db:"f_number"`
	ExposureTime     sql.NullString  `db:"exposure_time"`
	FocalLengthMM    sql.NullFloat64 `db:"focal_length_mm"`
	GPSLatitude      sql.NullFloat64 `db:"gps_latitude"`
	GPSLongitude     sql.NullFloat64 `db:"gps_longitude"`
	ViewCount        int             `db:"view_count"`
	LastViewedUnixNs sql.NullInt64   `db:"last_viewed_unix_ns"`
	Status           string          `db:"status"`
	CreatedAtUnix    int64           `db:"created_at_unix"`
	UpdatedAtUnix    int64           `db:"updated_at_unix"`
}

type photoFileEntity struct {
	ID            string `db:"id"`
	PhotoID       string `db:"photo_id"`
	ContentHash   string `db:"content_hash"`
	RelativePath  string `db:"relative_path"`
	FileName      string `db:"file_name"`
	FolderPath    string `db:"folder_path"`
	Format        string `db:"format"`
	FileSize      int64  `db:"file_size"`
	ModTimeUnixNs int64  `db:"mod_time_unix_ns"`
	Status        string `db:"status"`
	CreatedAtUnix int64  `db:"created_at_unix"`
	UpdatedAtUnix int64  `db:"updated_at_unix"`
}

package sqlite

import "database/sql"

type syncRunEntity struct {
	ID             string         `db:"id"`
	StartedAtUnix  int64          `db:"started_at_unix"`
	FinishedAtUnix sql.NullInt64  `db:"finished_at_unix"`
	Status         string         `db:"status"`
	ScannedCount   int            `db:"scanned_count"`
	AddedCount     int            `db:"added_count"`
	UpdatedCount   int            `db:"updated_count"`
	MovedCount     int            `db:"moved_count"`
	StackedCount   int            `db:"stacked_count"`
	DuplicateCount int            `db:"duplicate_count"`
	UnchangedCount int            `db:"unchanged_count"`
	MissingCount   int            `db:"missing_count"`
	ErrorMessage   sql.NullString `db:"error_message"`
}

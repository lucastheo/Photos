package sqlite

import "database/sql"

type photoViewEntity struct {
	ID             int64          `db:"id"`
	ViewID         sql.NullString `db:"view_id"`
	PhotoID        string         `db:"photo_id"`
	ViewedAtUnixNs int64          `db:"viewed_at_unix_ns"`
	Source         string         `db:"source"`
}

package sqlite

type photoTagEntity struct {
	PhotoID       string `db:"photo_id"`
	Emoji         string `db:"emoji"`
	CreatedAtUnix int64  `db:"created_at_unix"`
}

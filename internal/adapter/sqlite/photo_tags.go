package sqlite

import (
	"context"
	"fmt"
	"time"

	"github.com/lucastheo/photos/internal/domain"
)

// AddTag normalizes rawEmoji and attaches it to photoID idempotently.
func (r *PhotoRepository) AddTag(ctx context.Context, photoID, rawEmoji string, now time.Time) error {
	emoji, err := domain.NormalizeEmojiTag(rawEmoji)
	if err != nil {
		return err
	}
	if err := ensurePhotoExists(ctx, r.db.QueryRowContext, photoID); err != nil {
		return err
	}
	if now.IsZero() {
		now = time.Now().UTC()
	}
	e := photoTagToEntity(photoID, emoji, now)
	if _, err := r.db.ExecContext(
		ctx,
		`INSERT OR IGNORE INTO photo_tags (photo_id, emoji, created_at_unix) VALUES (?, ?, ?)`,
		e.PhotoID, e.Emoji, e.CreatedAtUnix,
	); err != nil {
		return fmt.Errorf("add tag %q to photo %q: %w", e.Emoji, e.PhotoID, err)
	}
	return nil
}

// RemoveTag normalizes rawEmoji and deletes its association from photoID.
func (r *PhotoRepository) RemoveTag(ctx context.Context, photoID, rawEmoji string) error {
	emoji, err := domain.NormalizeEmojiTag(rawEmoji)
	if err != nil {
		return err
	}
	if err := ensurePhotoExists(ctx, r.db.QueryRowContext, photoID); err != nil {
		return err
	}
	if _, err := r.db.ExecContext(
		ctx,
		`DELETE FROM photo_tags WHERE photo_id = ? AND emoji = ?`,
		photoID, emoji,
	); err != nil {
		return fmt.Errorf("remove tag %q from photo %q: %w", emoji, photoID, err)
	}
	return nil
}

// ListTags returns every emoji tag present on active photos along with its count.
func (r *PhotoRepository) ListTags(ctx context.Context) ([]domain.EmojiTagCount, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT pt.emoji, COUNT(*) AS cnt
		FROM photo_tags pt
		INNER JOIN photos p ON p.id = pt.photo_id
		WHERE p.status = ?
		GROUP BY pt.emoji
		ORDER BY cnt DESC, pt.emoji ASC`,
		string(domain.FileStatusActive),
	)
	if err != nil {
		return nil, fmt.Errorf("list emoji tags: %w", err)
	}
	defer func() { _ = rows.Close() }()

	out := make([]domain.EmojiTagCount, 0)
	for rows.Next() {
		var item domain.EmojiTagCount
		if err := rows.Scan(&item.Emoji, &item.Count); err != nil {
			return nil, fmt.Errorf("scan emoji tag count: %w", err)
		}
		out = append(out, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate emoji tag counts: %w", err)
	}
	return out, nil
}

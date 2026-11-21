package domain

import "time"

// ViewSource identifies how a photo view event was triggered.
type ViewSource string

const (
	// ViewSourceDwell indicates an automatic view recorded after the lightbox dwell threshold.
	ViewSourceDwell ViewSource = "dwell"

	// ViewSourceManual indicates an explicit user adjustment from the controls drawer.
	ViewSourceManual ViewSource = "manual"

	// ViewSourceSlideshow indicates an automatic view recorded during slideshow playback.
	ViewSourceSlideshow ViewSource = "slideshow"
)

// IsValid reports whether s is a supported ViewSource value.
func (s ViewSource) IsValid() bool {
	return s == ViewSourceDwell || s == ViewSourceManual || s == ViewSourceSlideshow
}

// ViewAction represents a manual adjustment operation on a photo's view count.
type ViewAction string

const (
	// ViewActionIncrement increases the photo's view count by 1.
	ViewActionIncrement ViewAction = "increment"

	// ViewActionDecrement decreases the photo's view count by 1 (clamped at 0).
	ViewActionDecrement ViewAction = "decrement"

	// ViewActionReset resets the photo's view count to 0 and clears its view history.
	ViewActionReset ViewAction = "reset"
)

// IsValid reports whether a is a supported ViewAction value.
func (a ViewAction) IsValid() bool {
	return a == ViewActionIncrement || a == ViewActionDecrement || a == ViewActionReset
}

// ViewEvent records a single timestamped view of a logical photo.
type ViewEvent struct {
	// ID is the database-assigned sequential event identifier.
	ID int64
	// ViewID is an optional client-generated idempotency identifier.
	ViewID string
	// PhotoID is the ID of the viewed logical photo.
	PhotoID string
	// ViewedAt is the timestamp when the view occurred.
	ViewedAt time.Time
	// Source identifies whether the view came from dwell, manual, or slideshow mode.
	Source ViewSource
}

// ViewStats summarizes aggregate viewing progress across active photos.
type ViewStats struct {
	// TotalViews is the sum of view counts across active photos.
	TotalViews int64
	// UnseenPhotos is the count of active photos with view_count == 0.
	UnseenPhotos int64
	// SeenPhotos is the count of active photos with view_count >= 1.
	SeenPhotos int64
}

// EmojiTagCount pairs a canonical emoji tag with the number of active photos bearing it.
type EmojiTagCount struct {
	// Emoji is the canonical NFC-normalized emoji tag without trailing U+FE0F.
	Emoji string
	// Count is the number of active photos tagged with Emoji.
	Count int64
}

// AuthSession represents the authentication status of a client session.
type AuthSession struct {
	// Authenticated reports whether the session is authenticated or auth is disabled.
	Authenticated bool
	// AuthDisabled reports whether password authentication is disabled by configuration.
	AuthDisabled bool
}

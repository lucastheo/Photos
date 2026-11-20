package domain

import "errors"

var (
	// ErrPhotoNotFound indicates that the requested logical photo does not exist.
	ErrPhotoNotFound = errors.New("photo not found")

	// ErrFileNotFound indicates that the requested physical photo file does not exist.
	ErrFileNotFound = errors.New("photo file not found")

	// ErrInvalidEmojiTag indicates that a provided emoji tag failed normalization or validation.
	ErrInvalidEmojiTag = errors.New("invalid emoji tag")

	// ErrInvalidFilter indicates that one or more photo search filter parameters are invalid.
	ErrInvalidFilter = errors.New("invalid photo filter")

	// ErrInvalidViewAction indicates an unrecognized manual view adjustment action.
	ErrInvalidViewAction = errors.New("invalid view action")

	// ErrInvalidViewSource indicates an unrecognized view event source.
	ErrInvalidViewSource = errors.New("invalid view source")

	// ErrSyncInProgress indicates that a library sync is already running.
	ErrSyncInProgress = errors.New("library sync already in progress")

	// ErrSyncNotFound indicates that the requested sync run does not exist.
	ErrSyncNotFound = errors.New("sync run not found")

	// ErrSettingNotFound indicates that the requested application setting does not exist.
	ErrSettingNotFound = errors.New("setting not found")

	// ErrUnsupportedFormat indicates an unrecognized or unsupported media file format.
	ErrUnsupportedFormat = errors.New("unsupported media format")

	// ErrLibraryUnavailable indicates that the configured library directory is unreadable or empty.
	ErrLibraryUnavailable = errors.New("library path is unavailable or empty")
)

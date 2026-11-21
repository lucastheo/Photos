package httpapi

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"

	"github.com/lucastheo/photos/internal/domain"
)

// ProblemResponse represents a uniform structured JSON error response.
type ProblemResponse struct {
	Code      string `json:"code"`
	Message   string `json:"message"`
	RequestID string `json:"request_id,omitempty"`
}

// MapDomainError deterministically maps a domain or application error to an
// HTTP status code, stable machine error code, and safe user-facing message.
func MapDomainError(err error) (int, string, string) {
	switch {
	case errors.Is(err, domain.ErrPhotoNotFound):
		return http.StatusNotFound, "PHOTO_NOT_FOUND", "Photo with the requested ID does not exist"
	case errors.Is(err, domain.ErrFileNotFound):
		return http.StatusNotFound, "FILE_NOT_FOUND", "Requested media file does not exist"
	case errors.Is(err, domain.ErrSyncNotFound):
		return http.StatusNotFound, "SYNC_NOT_FOUND", "No library sync run has been recorded"
	case errors.Is(err, domain.ErrInvalidEmojiTag):
		return http.StatusBadRequest, "INVALID_EMOJI_TAG", "Invalid emoji tag"
	case errors.Is(err, domain.ErrInvalidFilter):
		return http.StatusBadRequest, "INVALID_FILTER", "Invalid search filter parameters"
	case errors.Is(err, domain.ErrInvalidViewAction):
		return http.StatusBadRequest, "INVALID_VIEW_ACTION", "Invalid view adjustment action"
	case errors.Is(err, domain.ErrInvalidViewSource):
		return http.StatusBadRequest, "INVALID_VIEW_SOURCE", "Invalid view source"
	case errors.Is(err, domain.ErrUnsupportedFormat):
		return http.StatusBadRequest, "UNSUPPORTED_FORMAT", "Unsupported media format"
	case errors.Is(err, domain.ErrInvalidInput):
		return http.StatusBadRequest, "INVALID_INPUT", "Invalid request payload or parameters"
	case errors.Is(err, domain.ErrInvalidCredentials):
		return http.StatusUnauthorized, "INVALID_CREDENTIALS", "Invalid login credentials"
	case errors.Is(err, domain.ErrUnauthorized):
		return http.StatusUnauthorized, "UNAUTHORIZED", "Authentication required"
	case errors.Is(err, domain.ErrSyncInProgress):
		return http.StatusConflict, "SYNC_IN_PROGRESS", "A library sync is already running"
	case errors.Is(err, domain.ErrRateLimited):
		return http.StatusTooManyRequests, "RATE_LIMITED", "Too many login attempts; please try again later"
	case errors.Is(err, domain.ErrLibraryUnavailable):
		return http.StatusServiceUnavailable, "LIBRARY_UNAVAILABLE", "Configured photo library path is unavailable or empty"
	default:
		return http.StatusInternalServerError, "INTERNAL_ERROR", "An unexpected internal error occurred"
	}
}

// WriteProblem writes a JSON ProblemResponse derived from err and logs 500
// errors with slog without leaking internal paths or SQL details.
func WriteProblem(w http.ResponseWriter, r *http.Request, logger *slog.Logger, err error) {
	status, code, msg := MapDomainError(err)
	reqID := RequestIDFromContext(r.Context())
	if status >= http.StatusInternalServerError && logger != nil {
		logger.ErrorContext(r.Context(), "internal server error", "request_id", reqID, "error", err)
	}
	WriteProblemWithStatus(w, r, status, code, msg)
}

// WriteProblemWithStatus writes an explicit ProblemResponse with status, code, and message.
func WriteProblemWithStatus(w http.ResponseWriter, r *http.Request, status int, code, msg string) {
	reqID := ""
	if r != nil {
		reqID = RequestIDFromContext(r.Context())
	}
	writeJSON(w, status, ProblemResponse{
		Code:      code,
		Message:   msg,
		RequestID: reqID,
	})
}

func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}

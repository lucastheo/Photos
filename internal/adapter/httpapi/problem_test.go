package httpapi_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/lucastheo/photos/internal/adapter/httpapi"
	"github.com/lucastheo/photos/internal/domain"
)

func TestMapDomainError(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name       string
		err        error
		wantStatus int
		wantCode   string
	}{
		{name: "photo not found", err: domain.ErrPhotoNotFound, wantStatus: http.StatusNotFound, wantCode: "PHOTO_NOT_FOUND"},
		{name: "file not found", err: domain.ErrFileNotFound, wantStatus: http.StatusNotFound, wantCode: "FILE_NOT_FOUND"},
		{name: "sync not found", err: domain.ErrSyncNotFound, wantStatus: http.StatusNotFound, wantCode: "SYNC_NOT_FOUND"},
		{name: "invalid emoji", err: domain.ErrInvalidEmojiTag, wantStatus: http.StatusBadRequest, wantCode: "INVALID_EMOJI_TAG"},
		{name: "invalid filter", err: domain.ErrInvalidFilter, wantStatus: http.StatusBadRequest, wantCode: "INVALID_FILTER"},
		{name: "invalid view action", err: domain.ErrInvalidViewAction, wantStatus: http.StatusBadRequest, wantCode: "INVALID_VIEW_ACTION"},
		{name: "invalid view source", err: domain.ErrInvalidViewSource, wantStatus: http.StatusBadRequest, wantCode: "INVALID_VIEW_SOURCE"},
		{name: "unsupported format", err: domain.ErrUnsupportedFormat, wantStatus: http.StatusBadRequest, wantCode: "UNSUPPORTED_FORMAT"},
		{name: "invalid input", err: domain.ErrInvalidInput, wantStatus: http.StatusBadRequest, wantCode: "INVALID_INPUT"},
		{name: "invalid credentials", err: domain.ErrInvalidCredentials, wantStatus: http.StatusUnauthorized, wantCode: "INVALID_CREDENTIALS"},
		{name: "unauthorized", err: domain.ErrUnauthorized, wantStatus: http.StatusUnauthorized, wantCode: "UNAUTHORIZED"},
		{name: "sync in progress", err: domain.ErrSyncInProgress, wantStatus: http.StatusConflict, wantCode: "SYNC_IN_PROGRESS"},
		{name: "rate limited", err: domain.ErrRateLimited, wantStatus: http.StatusTooManyRequests, wantCode: "RATE_LIMITED"},
		{name: "library unavailable", err: domain.ErrLibraryUnavailable, wantStatus: http.StatusServiceUnavailable, wantCode: "LIBRARY_UNAVAILABLE"},
		{name: "wrapped unknown error", err: fmt.Errorf("sql failed at /secret/db.sqlite: %w", errors.New("constraint")), wantStatus: http.StatusInternalServerError, wantCode: "INTERNAL_ERROR"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			status, code, msg := httpapi.MapDomainError(tc.err)
			if status != tc.wantStatus || code != tc.wantCode || msg == "" {
				t.Fatalf("MapDomainError = (%d, %q, %q), want (%d, %q)", status, code, msg, tc.wantStatus, tc.wantCode)
			}
		})
	}
}

func TestWriteProblemLogs500WithoutLeakingInternalDetails(t *testing.T) {
	t.Parallel()

	var logBuf bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&logBuf, nil))

	h := httpapi.RequestIDMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		httpapi.WriteProblem(w, r, logger, errors.New("sqlite error in /private/path/photos.db"))
	}))

	req := httptest.NewRequest(http.MethodGet, "/api/v1/photos", nil)
	req.Header.Set("X-Request-ID", "req-test-500")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", rec.Code)
	}
	var prob httpapi.ProblemResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &prob); err != nil {
		t.Fatalf("unmarshal problem: %v", err)
	}
	if prob.Code != "INTERNAL_ERROR" || prob.RequestID != "req-test-500" {
		t.Fatalf("unexpected problem: %+v", prob)
	}
	if strings.Contains(prob.Message, "/private/path") {
		t.Fatalf("response message leaked internal details: %q", prob.Message)
	}
	if !strings.Contains(logBuf.String(), "/private/path/photos.db") {
		t.Fatalf("expected internal error logged via slog, got: %s", logBuf.String())
	}
}

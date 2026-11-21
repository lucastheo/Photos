package httpapi

import (
	"context"
	"errors"
	"log/slog"
	"net/http"

	"github.com/lucastheo/photos/internal/domain"
)

const defaultDetailHistoryLimit = 50

// PhotoSearcher defines read-only photo search and lookup operations consumed by PhotoHandler.
type PhotoSearcher interface {
	Search(ctx context.Context, filter domain.PhotoFilter) (domain.PaginatedPhotos, error)
	GetByID(ctx context.Context, photoID string) (domain.Photo, error)
}

// ViewTracker defines view tracking operations consumed by PhotoHandler.
type ViewTracker interface {
	RecordView(ctx context.Context, photoID, viewID string, source domain.ViewSource) (domain.Photo, error)
	AdjustViewCount(ctx context.Context, photoID string, action domain.ViewAction) (domain.Photo, error)
	ListViewHistory(ctx context.Context, photoID string, limit int) ([]domain.ViewEvent, error)
}

// PhotoTagger defines emoji tagging operations consumed by PhotoHandler.
type PhotoTagger interface {
	AddTag(ctx context.Context, photoID, rawEmoji string) (domain.Photo, error)
	RemoveTag(ctx context.Context, photoID, rawEmoji string) (domain.Photo, error)
	ListTags(ctx context.Context) ([]domain.EmojiTagCount, error)
}

// PreviewGenerator generates missing thumbnails and previews on demand.
type PreviewGenerator interface {
	EnsureCache(ctx context.Context, relativePath, contentHash string, format domain.MediaFormat, orientation int) error
}

// PhotoHandler handles HTTP endpoints for searching photos, tracking views,
// managing emoji tags, and serving media thumbnails/previews/originals.
type PhotoHandler struct {
	searcher    PhotoSearcher
	tracker     ViewTracker
	tagger      PhotoTagger
	previews    PreviewGenerator
	libraryPath string
	cachePath   string
	logger      *slog.Logger
}

// NewPhotoHandler constructs a concrete PhotoHandler.
func NewPhotoHandler(
	searcher PhotoSearcher,
	tracker ViewTracker,
	tagger PhotoTagger,
	previews PreviewGenerator,
	libraryPath, cachePath string,
) *PhotoHandler {
	return &PhotoHandler{
		searcher:    searcher,
		tracker:     tracker,
		tagger:      tagger,
		previews:    previews,
		libraryPath: libraryPath,
		cachePath:   cachePath,
	}
}

// WithLogger attaches a structured logger to PhotoHandler.
func (h *PhotoHandler) WithLogger(logger *slog.Logger) *PhotoHandler {
	h.logger = logger
	return h
}

// List handles GET /api/v1/photos.
func (h *PhotoHandler) List(w http.ResponseWriter, r *http.Request) {
	if h.searcher == nil {
		WriteProblem(w, r, h.logger, errors.New("photo searcher unavailable"))
		return
	}
	filter, err := parsePhotoFilterFromQuery(r.URL.Query())
	if err != nil {
		WriteProblem(w, r, h.logger, err)
		return
	}
	res, err := h.searcher.Search(r.Context(), filter)
	if err != nil {
		WriteProblem(w, r, h.logger, err)
		return
	}
	writeJSON(w, http.StatusOK, toPaginatedPhotosResponse(res))
}

// Get handles GET /api/v1/photos/{id}.
func (h *PhotoHandler) Get(w http.ResponseWriter, r *http.Request) {
	if h.searcher == nil {
		WriteProblem(w, r, h.logger, errors.New("photo searcher unavailable"))
		return
	}
	photoID := r.PathValue("id")
	photo, err := h.searcher.GetByID(r.Context(), photoID)
	if err != nil {
		WriteProblem(w, r, h.logger, err)
		return
	}
	history, err := h.loadHistory(r.Context(), photo.ID)
	if err != nil {
		WriteProblem(w, r, h.logger, err)
		return
	}
	writeJSON(w, http.StatusOK, toPhotoResponse(photo, true, history))
}

// RecordView handles POST /api/v1/photos/{id}/views.
func (h *PhotoHandler) RecordView(w http.ResponseWriter, r *http.Request) {
	if h.tracker == nil {
		WriteProblem(w, r, h.logger, errors.New("view tracker unavailable"))
		return
	}
	var req RecordViewRequest
	if err := decodeJSONBody(w, r, &req, true); err != nil {
		WriteProblem(w, r, h.logger, err)
		return
	}
	photoID := r.PathValue("id")
	photo, err := h.tracker.RecordView(r.Context(), photoID, req.ViewID, domain.ViewSource(req.Source))
	if err != nil {
		WriteProblem(w, r, h.logger, err)
		return
	}
	history, err := h.loadHistory(r.Context(), photo.ID)
	if err != nil {
		WriteProblem(w, r, h.logger, err)
		return
	}
	writeJSON(w, http.StatusOK, toPhotoResponse(photo, true, history))
}

// AdjustViewCount handles PATCH /api/v1/photos/{id}/views.
func (h *PhotoHandler) AdjustViewCount(w http.ResponseWriter, r *http.Request) {
	if h.tracker == nil {
		WriteProblem(w, r, h.logger, errors.New("view tracker unavailable"))
		return
	}
	var req AdjustViewRequest
	if err := decodeJSONBody(w, r, &req, false); err != nil {
		WriteProblem(w, r, h.logger, err)
		return
	}
	photoID := r.PathValue("id")
	photo, err := h.tracker.AdjustViewCount(r.Context(), photoID, domain.ViewAction(req.Action))
	if err != nil {
		WriteProblem(w, r, h.logger, err)
		return
	}
	history, err := h.loadHistory(r.Context(), photo.ID)
	if err != nil {
		WriteProblem(w, r, h.logger, err)
		return
	}
	writeJSON(w, http.StatusOK, toPhotoResponse(photo, true, history))
}

// AddTag handles POST /api/v1/photos/{id}/tags.
func (h *PhotoHandler) AddTag(w http.ResponseWriter, r *http.Request) {
	if h.tagger == nil {
		WriteProblem(w, r, h.logger, errors.New("photo tagger unavailable"))
		return
	}
	var req AddTagRequest
	if err := decodeJSONBody(w, r, &req, false); err != nil {
		WriteProblem(w, r, h.logger, err)
		return
	}
	photo, err := h.tagger.AddTag(r.Context(), r.PathValue("id"), req.Emoji)
	if err != nil {
		WriteProblem(w, r, h.logger, err)
		return
	}
	writeJSON(w, http.StatusOK, toPhotoResponse(photo, false, nil))
}

// RemoveTag handles DELETE /api/v1/photos/{id}/tags/{emoji}.
func (h *PhotoHandler) RemoveTag(w http.ResponseWriter, r *http.Request) {
	if h.tagger == nil {
		WriteProblem(w, r, h.logger, errors.New("photo tagger unavailable"))
		return
	}
	photo, err := h.tagger.RemoveTag(r.Context(), r.PathValue("id"), r.PathValue("emoji"))
	if err != nil {
		WriteProblem(w, r, h.logger, err)
		return
	}
	writeJSON(w, http.StatusOK, toPhotoResponse(photo, false, nil))
}

// ListTags handles GET /api/v1/tags.
func (h *PhotoHandler) ListTags(w http.ResponseWriter, r *http.Request) {
	if h.tagger == nil {
		WriteProblem(w, r, h.logger, errors.New("photo tagger unavailable"))
		return
	}
	tags, err := h.tagger.ListTags(r.Context())
	if err != nil {
		WriteProblem(w, r, h.logger, err)
		return
	}
	writeJSON(w, http.StatusOK, toEmojiTagCountDTOs(tags))
}

func (h *PhotoHandler) loadHistory(ctx context.Context, photoID string) ([]domain.ViewEvent, error) {
	if h.tracker == nil {
		return nil, nil
	}
	return h.tracker.ListViewHistory(ctx, photoID, defaultDetailHistoryLimit)
}

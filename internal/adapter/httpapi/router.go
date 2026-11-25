package httpapi

import (
	"io/fs"
	"log/slog"
	"net/http"
	"path"
	"strings"
)

// RouterConfig provides handlers, auth validation, logging, and optional static
// SPA assets used to construct a concrete Router.
type RouterConfig struct {
	Auth          AuthValidator
	AuthHandler   *AuthHandler
	PhotoHandler  *PhotoHandler
	SyncHandler   *SyncHandler
	HealthHandler *HealthHandler
	StaticFS      fs.FS
	Logger        *slog.Logger
}

// Router routes Photo View Tracker REST API and optional SPA static requests
// through the configured security and observability middleware chain.
type Router struct {
	handler http.Handler
}

// NewRouter builds a concrete Router wiring all /api/v1/* endpoints and middleware.
func NewRouter(cfg RouterConfig) *Router {
	mux := http.NewServeMux()
	registerHealthAndAuthRoutes(mux, cfg)
	registerPhotoRoutes(mux, cfg)
	registerLibraryRoutes(mux, cfg)
	registerFallbackRoutes(mux, cfg.StaticFS)

	chain := AuthMiddleware(cfg.Auth)(mux)
	chain = CrossOriginMiddleware(cfg.Logger)(chain)
	chain = LoggingMiddleware(cfg.Logger)(chain)
	chain = RequestIDMiddleware(chain)
	return &Router{handler: chain}
}

// ServeHTTP dispatches HTTP requests through the configured middleware chain.
func (rt *Router) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	rt.handler.ServeHTTP(w, r)
}

func registerHealthAndAuthRoutes(mux *http.ServeMux, cfg RouterConfig) {
	if cfg.HealthHandler != nil {
		mux.HandleFunc("GET /api/v1/health", cfg.HealthHandler.Check)
	}
	if cfg.AuthHandler != nil {
		mux.HandleFunc("GET /api/v1/auth/session", cfg.AuthHandler.Session)
		mux.HandleFunc("POST /api/v1/auth/login", cfg.AuthHandler.Login)
		mux.HandleFunc("POST /api/v1/auth/logout", cfg.AuthHandler.Logout)
	}
}

func registerPhotoRoutes(mux *http.ServeMux, cfg RouterConfig) {
	if cfg.PhotoHandler == nil {
		return
	}
	mux.HandleFunc("GET /api/v1/photos", cfg.PhotoHandler.List)
	mux.HandleFunc("GET /api/v1/photos/{id}", cfg.PhotoHandler.Get)
	mux.HandleFunc("POST /api/v1/photos/{id}/views", cfg.PhotoHandler.RecordView)
	mux.HandleFunc("PATCH /api/v1/photos/{id}/views", cfg.PhotoHandler.AdjustViewCount)
	mux.HandleFunc("POST /api/v1/photos/{id}/tags", cfg.PhotoHandler.AddTag)
	mux.HandleFunc("DELETE /api/v1/photos/{id}/tags/{emoji}", cfg.PhotoHandler.RemoveTag)
	mux.HandleFunc("GET /api/v1/tags", cfg.PhotoHandler.ListTags)
	mux.HandleFunc("GET /api/v1/photos/{id}/thumbnail", cfg.PhotoHandler.Thumbnail)
	mux.HandleFunc("GET /api/v1/photos/{id}/preview", cfg.PhotoHandler.Preview)
	mux.HandleFunc("GET /api/v1/photos/{id}/original", cfg.PhotoHandler.Original)
}

func registerLibraryRoutes(mux *http.ServeMux, cfg RouterConfig) {
	if cfg.SyncHandler == nil {
		return
	}
	mux.HandleFunc("GET /api/v1/library/stats", cfg.SyncHandler.Stats)
	mux.HandleFunc("GET /api/v1/library/duplicates", cfg.SyncHandler.Duplicates)
	mux.HandleFunc("GET /api/v1/library/missing", cfg.SyncHandler.Missing)
	mux.HandleFunc("DELETE /api/v1/library/missing", cfg.SyncHandler.PurgeMissing)
	mux.HandleFunc("POST /api/v1/library/sync", cfg.SyncHandler.TriggerSync)
	mux.HandleFunc("GET /api/v1/library/sync/status", cfg.SyncHandler.SyncStatus)
}

func registerFallbackRoutes(mux *http.ServeMux, staticFS fs.FS) {
	mux.HandleFunc("/api/", func(w http.ResponseWriter, r *http.Request) {
		WriteProblemWithStatus(w, r, http.StatusNotFound, "NOT_FOUND", "API endpoint not found")
	})
	if staticFS == nil {
		mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
			WriteProblemWithStatus(w, r, http.StatusNotFound, "NOT_FOUND", "Resource not found")
		})
		return
	}
	fileServer := http.FileServerFS(staticFS)
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		serveSPAOrStatic(w, r, staticFS, fileServer)
	})
}

const (
	staticImmutableCacheHeader = "public, max-age=3600, immutable"
	staticHTMLCacheHeader      = "no-cache"
	staticDefaultCacheHeader   = "public, max-age=3600"
)

func serveSPAOrStatic(w http.ResponseWriter, r *http.Request, staticFS fs.FS, fileServer http.Handler) {
	cleanPath := strings.TrimPrefix(path.Clean("/"+r.URL.Path), "/")
	if cleanPath != "" && cleanPath != "index.html" {
		if info, err := fs.Stat(staticFS, cleanPath); err == nil && !info.IsDir() {
			w.Header().Set("Cache-Control", staticCacheControl(cleanPath))
			fileServer.ServeHTTP(w, r)
			return
		}
		if strings.Contains(path.Base(cleanPath), ".") {
			http.NotFound(w, r)
			return
		}
	}
	w.Header().Set("Cache-Control", staticHTMLCacheHeader)
	r2 := r.Clone(r.Context())
	r2.URL.Path = "/"
	fileServer.ServeHTTP(w, r2)
}

func staticCacheControl(cleanPath string) string {
	if strings.HasPrefix(cleanPath, "assets/") {
		return staticImmutableCacheHeader
	}
	if strings.HasSuffix(cleanPath, ".html") {
		return staticHTMLCacheHeader
	}
	return staticDefaultCacheHeader
}

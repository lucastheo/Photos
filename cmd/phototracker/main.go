package main

import (
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/lucastheo/photos/internal/adapter/httpapi"
	"github.com/lucastheo/photos/internal/adapter/media"
	"github.com/lucastheo/photos/internal/adapter/sqlite"
	"github.com/lucastheo/photos/internal/config"
	"github.com/lucastheo/photos/internal/usecase"
	"github.com/lucastheo/photos/web"
)

const shutdownTimeout = 10 * time.Second

type application struct {
	cfg         config.Config
	logger      *slog.Logger
	db          *sqlite.DB
	syncHandler *httpapi.SyncHandler
	router      *httpapi.Router
	cancelSync  context.CancelFunc
}

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if err := runMain(ctx); err != nil {
		slog.Error("phototracker exited with error", "error", err)
		os.Exit(1)
	}
}

func runMain(ctx context.Context) error {
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}
	staticFS, err := web.DistFS()
	if err != nil {
		return fmt.Errorf("load embedded frontend: %w", err)
	}
	logger := newLogger(cfg)
	app, err := buildApplication(ctx, cfg, logger, staticFS)
	if err != nil {
		return err
	}
	return app.serve(ctx)
}

func newLogger(cfg config.Config) *slog.Logger {
	opts := &slog.HandlerOptions{Level: parseSlogLevel(cfg.LogLevel)}
	if cfg.LogFormat == "json" {
		return slog.New(slog.NewJSONHandler(os.Stdout, opts))
	}
	return slog.New(slog.NewTextHandler(os.Stdout, opts))
}

func parseSlogLevel(raw string) slog.Level {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "debug":
		return slog.LevelDebug
	case "warn":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}

func buildApplication(
	ctx context.Context,
	cfg config.Config,
	logger *slog.Logger,
	staticFS fs.FS,
) (*application, error) {
	if err := os.MkdirAll(cfg.CachePath, 0o755); err != nil {
		return nil, fmt.Errorf("create cache dir: %w", err)
	}
	db, err := sqlite.Open(ctx, cfg.DBPath)
	if err != nil {
		return nil, fmt.Errorf("open database: %w", err)
	}
	if err := db.Migrate(ctx); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("migrate database: %w", err)
	}

	syncCtx, cancelSync := context.WithCancel(context.Background())
	app, err := wireDependencies(ctx, syncCtx, cancelSync, cfg, logger, db, staticFS)
	if err != nil {
		cancelSync()
		_ = db.Close()
		return nil, err
	}
	return app, nil
}

func wireDependencies(
	ctx, syncCtx context.Context,
	cancelSync context.CancelFunc,
	cfg config.Config,
	logger *slog.Logger,
	db *sqlite.DB,
	staticFS fs.FS,
) (*application, error) {
	photoRepo := sqlite.NewPhotoRepository(db.SQL())
	viewRepo := sqlite.NewViewRepository(db.SQL())
	syncRepo := sqlite.NewSyncRepository(db.SQL())

	scanner := media.NewScanner(cfg.LibraryPath, cfg.SyncWorkers)
	exifExt := media.NewEXIFExtractor(cfg.LibraryPath)
	previewExt := media.NewPreviewExtractor(cfg.LibraryPath, cfg.CachePath, cfg.PreviewWorkers)

	syncUC := usecase.NewSyncLibraryUseCase(photoRepo, scanner, previewExt).
		WithMetadataExtractor(exifExt).
		WithSyncRunStore(syncRepo).
		WithDatabaseMaintainer(db)
	if _, err := syncUC.RecoverStaleRuns(ctx); err != nil {
		return nil, fmt.Errorf("recover stale sync runs: %w", err)
	}

	authUC, err := initAuthUseCase(ctx, cfg, syncRepo, logger)
	if err != nil {
		return nil, err
	}

	searchUC := usecase.NewSearchPhotosUseCase(photoRepo)
	viewUC := usecase.NewTrackViewUseCase(viewRepo)
	tagUC := usecase.NewTagPhotoUseCase(photoRepo)

	syncH := httpapi.NewSyncHandler(syncUC, searchUC).WithBaseContext(syncCtx).WithLogger(logger)
	router := httpapi.NewRouter(httpapi.RouterConfig{
		Auth:          authUC,
		AuthHandler:   httpapi.NewAuthHandler(authUC, httpapi.NewLoginRateLimiter(5, time.Minute), cfg.CookieSecure).WithLogger(logger),
		PhotoHandler:  httpapi.NewPhotoHandler(searchUC, viewUC, tagUC, previewExt, cfg.LibraryPath, cfg.CachePath).WithLogger(logger),
		SyncHandler:   syncH,
		HealthHandler: httpapi.NewHealthHandler(db.SQL(), cfg.LibraryPath, cfg.CachePath),
		StaticFS:      staticFS,
		Logger:        logger,
	})
	return &application{cfg: cfg, logger: logger, db: db, syncHandler: syncH, router: router, cancelSync: cancelSync}, nil
}

func initAuthUseCase(ctx context.Context, cfg config.Config, syncRepo *sqlite.SyncRepository, logger *slog.Logger) (*usecase.AuthUseCase, error) {
	verifier := usecase.NewStaticPasswordVerifier(cfg.AuthPassword, cfg.AuthPasswordHash)
	if !cfg.AuthDisabled && !verifier.Configured() && logger != nil {
		logger.WarnContext(ctx, "authentication enabled without valid AUTH_PASSWORD or AUTH_PASSWORD_HASH; logins will be rejected")
	}
	authUC := usecase.NewAuthUseCase(cfg.AuthDisabled, verifier, decodeSecret(cfg.AuthSecret))
	if err := authUC.EnsureSecret(ctx, syncRepo); err != nil {
		return nil, fmt.Errorf("initialize session secret: %w", err)
	}
	return authUC, nil
}

func decodeSecret(raw string) []byte {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return nil
	}
	if decoded, err := hex.DecodeString(trimmed); err == nil && len(decoded) >= 32 {
		return decoded
	}
	return []byte(trimmed)
}

func (a *application) serve(ctx context.Context) error {
	ln, err := net.Listen("tcp", a.cfg.ListenAddr())
	if err != nil {
		_ = a.shutdown()
		return fmt.Errorf("listen %s: %w", a.cfg.ListenAddr(), err)
	}
	srv := &http.Server{
		Handler:           a.router,
		ReadHeaderTimeout: 10 * time.Second,
	}
	a.logger.Info("phototracker listening", "addr", ln.Addr().String(), "library_path", a.cfg.LibraryPath)

	errCh := make(chan error, 1)
	go func() {
		if serveErr := srv.Serve(ln); serveErr != nil && !errors.Is(serveErr, http.ErrServerClosed) {
			errCh <- serveErr
			return
		}
		errCh <- nil
	}()

	select {
	case serveErr := <-errCh:
		return errors.Join(serveErr, a.shutdown())
	case <-ctx.Done():
		drainCtx, cancelDrain := context.WithTimeout(context.Background(), shutdownTimeout)
		defer cancelDrain()
		drainErr := srv.Shutdown(drainCtx)
		serveErr := <-errCh
		return errors.Join(drainErr, serveErr, a.shutdown())
	}
}

func (a *application) shutdown() error {
	if a.cancelSync != nil {
		a.cancelSync()
	}
	if a.syncHandler != nil {
		a.syncHandler.Wait()
	}
	if a.db == nil {
		return nil
	}
	chkCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer cancel()
	chkErr := a.db.Checkpoint(chkCtx)
	closeErr := a.db.Close()
	return errors.Join(chkErr, closeErr)
}

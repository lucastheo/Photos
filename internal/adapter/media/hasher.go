package media

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"

	"golang.org/x/sync/errgroup"

	"github.com/lucastheo/photos/internal/domain"
)

const hashChunkSize = 64 * 1024

// Hasher computes streaming SHA-256 digests for media files confined to a library root.
type Hasher struct {
	libraryPath string
	workers     int
}

// NewHasher creates a Hasher confined to libraryPath with a bounded worker count.
// Non-positive workers default to runtime.NumCPU().
func NewHasher(libraryPath string, workers int) *Hasher {
	if workers <= 0 {
		workers = max(1, runtime.NumCPU())
	}
	return &Hasher{
		libraryPath: libraryPath,
		workers:     workers,
	}
}

// Workers returns the maximum number of concurrent hashing workers.
func (h *Hasher) Workers() int {
	return h.workers
}

// HashFile computes the lowercase SHA-256 hex digest of relativePath within the library root.
func (h *Hasher) HashFile(ctx context.Context, relativePath string) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	root, err := openLibraryRoot(h.libraryPath)
	if err != nil {
		return "", err
	}
	defer func() { _ = root.Close() }()

	return hashFileInRoot(ctx, root, relativePath)
}

// HashFiles computes SHA-256 digests for all files in parallel using a bounded worker pool
// and returns a cloned slice with ContentHash populated in the original deterministic order.
func (h *Hasher) HashFiles(ctx context.Context, files []domain.ScannedFile) ([]domain.ScannedFile, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if len(files) == 0 {
		return []domain.ScannedFile{}, nil
	}

	root, err := openLibraryRoot(h.libraryPath)
	if err != nil {
		return nil, err
	}
	defer func() { _ = root.Close() }()

	out := slices.Clone(files)
	g, gctx := errgroup.WithContext(ctx)
	g.SetLimit(h.workers)

	for i := range out {
		g.Go(func() error {
			sum, hashErr := hashFileInRoot(gctx, root, out[i].RelativePath)
			if hashErr != nil {
				return hashErr
			}
			out[i].ContentHash = sum
			return nil
		})
	}
	if err := g.Wait(); err != nil {
		return nil, err
	}
	return out, nil
}

func openLibraryRoot(libraryPath string) (*os.Root, error) {
	if libraryPath == "" {
		return nil, fmt.Errorf("%w: empty library path", domain.ErrLibraryUnavailable)
	}
	root, err := os.OpenRoot(libraryPath)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", domain.ErrLibraryUnavailable, err)
	}
	return root, nil
}

func localizeLibraryPath(relativePath string) (string, error) {
	norm := domain.NormalizeRelativePath(relativePath)
	if norm == "" {
		return "", errors.New("relative path cannot be empty")
	}
	local, err := filepath.Localize(norm)
	if err != nil {
		return "", fmt.Errorf("localize relative path %q: %w", relativePath, err)
	}
	return local, nil
}

func resolveSubprocessPath(libraryPath, local string) string {
	joined := filepath.Join(libraryPath, local)
	if strings.HasPrefix(joined, "-") {
		return "." + string(filepath.Separator) + joined
	}
	return joined
}

func openRegularInRoot(root *os.Root, relativePath string) (*os.File, string, error) {
	local, err := localizeLibraryPath(relativePath)
	if err != nil {
		return nil, "", err
	}
	f, err := root.Open(local)
	if err != nil {
		return nil, "", fmt.Errorf("open %q: %w", relativePath, err)
	}
	info, err := f.Stat()
	if err != nil {
		_ = f.Close()
		return nil, "", fmt.Errorf("stat %q: %w", relativePath, err)
	}
	if !info.Mode().IsRegular() {
		_ = f.Close()
		return nil, "", fmt.Errorf("%q is not a regular file", relativePath)
	}
	return f, local, nil
}

func hashFileInRoot(ctx context.Context, root *os.Root, relativePath string) (string, error) {
	f, _, err := openRegularInRoot(root, relativePath)
	if err != nil {
		return "", err
	}
	defer func() { _ = f.Close() }()

	return streamSHA256(ctx, f, relativePath)
}

func streamSHA256(ctx context.Context, r io.Reader, label string) (string, error) {
	sum := sha256.New()
	buf := make([]byte, hashChunkSize)

	for {
		if err := ctx.Err(); err != nil {
			return "", err
		}
		n, readErr := r.Read(buf)
		if n > 0 {
			_, _ = sum.Write(buf[:n])
		}
		if readErr == io.EOF {
			break
		}
		if readErr != nil {
			return "", fmt.Errorf("read %q: %w", label, readErr)
		}
	}
	return hex.EncodeToString(sum.Sum(nil)), nil
}

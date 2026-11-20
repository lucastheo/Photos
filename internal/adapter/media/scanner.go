package media

import (
	"cmp"
	"context"
	"fmt"
	"io/fs"
	"os"
	"path"
	"slices"
	"strings"

	"github.com/lucastheo/photos/internal/domain"
)

// Scanner walks a read-only media library directory using os.Root confinement
// and delegates parallel SHA-256 content hashing to a bounded Hasher.
type Scanner struct {
	libraryPath string
	hasher      *Hasher
}

// NewScanner creates a Scanner confined to libraryPath using workers for hashing.
func NewScanner(libraryPath string, workers int) *Scanner {
	return &Scanner{
		libraryPath: libraryPath,
		hasher:      NewHasher(libraryPath, workers),
	}
}

// FileMatchesMetadata reports whether a disk scan entry matches an indexed PhotoFile
// by normalized relative path, byte size, and UTC modification nanosecond timestamp.
func (s *Scanner) FileMatchesMetadata(scanned domain.ScannedFile, existing domain.PhotoFile) bool {
	if domain.NormalizeRelativePath(scanned.RelativePath) != domain.NormalizeRelativePath(existing.RelativePath) {
		return false
	}
	if scanned.FileSize != existing.FileSize {
		return false
	}
	return scanned.ModTime.UTC().UnixNano() == existing.ModTime.UTC().UnixNano()
}

// Walk traverses the library directory and returns supported non-empty regular media files
// sorted deterministically by NFC-normalized RelativePath ascending.
func (s *Scanner) Walk(ctx context.Context) ([]domain.ScannedFile, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	root, err := openLibraryRoot(s.libraryPath)
	if err != nil {
		return nil, err
	}
	defer func() { _ = root.Close() }()

	if err := ensureReadableDirRoot(root); err != nil {
		return nil, err
	}

	files := make([]domain.ScannedFile, 0, 64)
	walkErr := fs.WalkDir(root.FS(), ".", func(rel string, d fs.DirEntry, entryErr error) error {
		return s.collectWalkEntry(ctx, rel, d, entryErr, &files)
	})
	if walkErr != nil {
		return nil, walkErr
	}

	slices.SortFunc(files, func(a, b domain.ScannedFile) int {
		return cmp.Compare(a.RelativePath, b.RelativePath)
	})
	return files, nil
}

// HashFile delegates single-file SHA-256 hashing to the underlying Hasher.
func (s *Scanner) HashFile(ctx context.Context, relativePath string) (string, error) {
	return s.hasher.HashFile(ctx, relativePath)
}

// HashFiles delegates bounded parallel SHA-256 hashing to the underlying Hasher.
func (s *Scanner) HashFiles(ctx context.Context, files []domain.ScannedFile) ([]domain.ScannedFile, error) {
	return s.hasher.HashFiles(ctx, files)
}

func ensureReadableDirRoot(root *os.Root) error {
	info, err := root.Stat(".")
	if err != nil {
		return fmt.Errorf("%w: %w", domain.ErrLibraryUnavailable, err)
	}
	if !info.IsDir() {
		return fmt.Errorf("%w: library path is not a directory", domain.ErrLibraryUnavailable)
	}
	return nil
}

func (s *Scanner) collectWalkEntry(
	ctx context.Context,
	rel string,
	d fs.DirEntry,
	entryErr error,
	out *[]domain.ScannedFile,
) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if entryErr != nil {
		if rel == "." {
			return fmt.Errorf("%w: %w", domain.ErrLibraryUnavailable, entryErr)
		}
		return entryErr
	}
	if d.IsDir() {
		return filterDirectory(rel, d.Name())
	}
	scanned, ok, err := buildScannedFile(rel, d)
	if err != nil || !ok {
		return err
	}
	*out = append(*out, scanned)
	return nil
}

func filterDirectory(rel, name string) error {
	if rel == "." {
		return nil
	}
	if strings.HasPrefix(name, ".") || name == "@eaDir" {
		return fs.SkipDir
	}
	return nil
}

func buildScannedFile(rel string, d fs.DirEntry) (domain.ScannedFile, bool, error) {
	if !d.Type().IsRegular() {
		return domain.ScannedFile{}, false, nil
	}
	name := d.Name()
	if strings.HasPrefix(name, ".") {
		return domain.ScannedFile{}, false, nil
	}
	format, mediaType, ok := domain.FormatFromExtension(path.Ext(name))
	if !ok {
		return domain.ScannedFile{}, false, nil
	}
	info, err := d.Info()
	if err != nil {
		return domain.ScannedFile{}, false, fmt.Errorf("stat %q: %w", rel, err)
	}
	if !info.Mode().IsRegular() || info.Size() <= 0 {
		return domain.ScannedFile{}, false, nil
	}
	return makeScannedFile(rel, name, format, mediaType, info), true, nil
}

func makeScannedFile(
	rel, name string,
	format domain.MediaFormat,
	mediaType domain.MediaType,
	info fs.FileInfo,
) domain.ScannedFile {
	normRel := domain.NormalizeRelativePath(rel)
	normName := domain.NormalizeNFC(name)
	folder := domain.NormalizeRelativePath(path.Dir(normRel))
	baseName := strings.TrimSuffix(normName, path.Ext(normName))
	return domain.ScannedFile{
		RelativePath: normRel,
		FileName:     normName,
		FolderPath:   folder,
		BaseName:     baseName,
		Format:       format,
		MediaType:    mediaType,
		FileSize:     info.Size(),
		ModTime:      info.ModTime().UTC(),
	}
}

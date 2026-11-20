package usecase_test

import (
	"context"
	"maps"
	"path"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/lucastheo/photos/internal/domain"
)

type fakePhotoStore struct {
	mu     sync.Mutex
	photos map[string]domain.Photo
	files  map[string]domain.PhotoFile

	listAllErr        error
	getByIDErr        error
	getByStackKeyErr  error
	findByHashErr     error
	createPhotoErr    error
	attachFileErr     error
	updateFileErr     error
	updateFileAtCall  int
	updateFileCalls   int
	updateMetaErr     error
	moveBatchErr      error
	splitMovedErr     error
	markMissingErr    error
	purgeMissingErr   error
	updateMetaCalls   int
	markMissingCalled int
	splitCalls        int
	moveBatchCalls    int
}

func newFakePhotoStore() *fakePhotoStore {
	return &fakePhotoStore{
		photos: make(map[string]domain.Photo),
		files:  make(map[string]domain.PhotoFile),
	}
}

func (s *fakePhotoStore) seedPhoto(p domain.Photo) {
	s.mu.Lock()
	defer s.mu.Unlock()
	clone := clonePhoto(p)
	s.photos[clone.ID] = clone
	for _, f := range clone.Files {
		fc := f
		if fc.PhotoID == "" {
			fc.PhotoID = clone.ID
		}
		s.files[fc.ID] = fc
	}
}

func (s *fakePhotoStore) ListAllFiles(_ context.Context) ([]domain.PhotoFile, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.listAllErr != nil {
		return nil, s.listAllErr
	}
	out := make([]domain.PhotoFile, 0, len(s.files))
	for _, f := range s.files {
		out = append(out, f)
	}
	slices.SortFunc(out, func(a, b domain.PhotoFile) int {
		return strings.Compare(a.RelativePath, b.RelativePath)
	})
	return out, nil
}

func (s *fakePhotoStore) GetByID(_ context.Context, photoID string) (domain.Photo, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.getByIDErr != nil {
		return domain.Photo{}, s.getByIDErr
	}
	p, ok := s.photos[photoID]
	if !ok {
		return domain.Photo{}, domain.ErrPhotoNotFound
	}
	return s.hydrateLocked(p), nil
}

func (s *fakePhotoStore) GetByStackKey(_ context.Context, stackKey string) (domain.Photo, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.getByStackKeyErr != nil {
		return domain.Photo{}, s.getByStackKeyErr
	}
	var matches []domain.Photo
	for _, p := range s.photos {
		if p.StackKey == stackKey {
			matches = append(matches, s.hydrateLocked(p))
		}
	}
	if len(matches) == 0 {
		return domain.Photo{}, domain.ErrPhotoNotFound
	}
	slices.SortFunc(matches, func(a, b domain.Photo) int {
		if a.Status != b.Status {
			if a.Status == domain.FileStatusActive {
				return -1
			}
			return 1
		}
		return a.CreatedAt.Compare(b.CreatedAt)
	})
	return matches[0], nil
}

func (s *fakePhotoStore) FindFilesByContentHash(_ context.Context, contentHash string) ([]domain.PhotoFile, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.findByHashErr != nil {
		return nil, s.findByHashErr
	}
	var out []domain.PhotoFile
	for _, f := range s.files {
		if f.ContentHash == contentHash {
			out = append(out, f)
		}
	}
	slices.SortFunc(out, func(a, b domain.PhotoFile) int {
		if a.Status != b.Status {
			if a.Status == domain.FileStatusActive {
				return -1
			}
			return 1
		}
		return strings.Compare(a.RelativePath, b.RelativePath)
	})
	return out, nil
}

func (s *fakePhotoStore) CreatePhoto(_ context.Context, photo domain.Photo) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.createPhotoErr != nil {
		return s.createPhotoErr
	}
	clone := clonePhoto(photo)
	s.photos[clone.ID] = clone
	for _, f := range clone.Files {
		fc := f
		if fc.PhotoID == "" {
			fc.PhotoID = clone.ID
		}
		s.files[fc.ID] = fc
	}
	return nil
}

func (s *fakePhotoStore) AttachFile(_ context.Context, photoID string, file domain.PhotoFile) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.attachFileErr != nil {
		return s.attachFileErr
	}
	p, ok := s.photos[photoID]
	if !ok {
		return domain.ErrPhotoNotFound
	}
	fc := file
	fc.PhotoID = photoID
	s.files[fc.ID] = fc
	if fc.Status == domain.FileStatusActive {
		p.Status = domain.FileStatusActive
		s.photos[photoID] = p
	}
	return nil
}

func (s *fakePhotoStore) UpdateFileInPlace(_ context.Context, file domain.PhotoFile) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.updateFileCalls++
	if s.updateFileErr != nil && (s.updateFileAtCall == 0 || s.updateFileCalls == s.updateFileAtCall) {
		return s.updateFileErr
	}
	target, ok := s.findFileLocked(file)
	if !ok {
		return domain.ErrFileNotFound
	}
	target.ContentHash = file.ContentHash
	target.FileSize = file.FileSize
	target.ModTime = file.ModTime
	target.Status = file.Status
	target.UpdatedAt = file.UpdatedAt
	s.files[target.ID] = target
	s.recomputePhotoStatusLocked(target.PhotoID)
	return nil
}

func (s *fakePhotoStore) UpdatePhotoMetadata(_ context.Context, photo domain.Photo) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.updateMetaCalls++
	if s.updateMetaErr != nil {
		return s.updateMetaErr
	}
	existing, ok := s.photos[photo.ID]
	if !ok {
		return domain.ErrPhotoNotFound
	}
	existing.Width = photo.Width
	existing.Height = photo.Height
	existing.DurationSec = photo.DurationSec
	existing.TakenAt = photo.TakenAt
	existing.EXIF = photo.EXIF
	existing.UpdatedAt = photo.UpdatedAt
	s.photos[photo.ID] = existing
	return nil
}

func (s *fakePhotoStore) MoveFilesBatch(_ context.Context, moves []domain.FileMove, now time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.moveBatchCalls++
	if s.moveBatchErr != nil {
		return s.moveBatchErr
	}
	seenPhotos := make(map[string]bool)
	for _, m := range moves {
		f, ok := s.files[m.FileID]
		if !ok {
			return domain.ErrFileNotFound
		}
		f.RelativePath = domain.NormalizeRelativePath(m.RelativePath)
		f.FileName = m.FileName
		if f.FileName == "" {
			f.FileName = path.Base(f.RelativePath)
		}
		f.FolderPath = domain.NormalizeRelativePath(m.FolderPath)
		f.Status = domain.FileStatusActive
		f.UpdatedAt = now
		s.files[f.ID] = f
		seenPhotos[f.PhotoID] = true
	}
	for pid := range seenPhotos {
		s.refreshMovedPhotoLocked(pid, now)
	}
	return nil
}

func (s *fakePhotoStore) SplitMovedFile(
	_ context.Context,
	fileID string,
	newPhoto domain.Photo,
	move domain.FileMove,
	now time.Time,
) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.splitCalls++
	if s.splitMovedErr != nil {
		return s.splitMovedErr
	}
	f, ok := s.files[fileID]
	if !ok {
		return domain.ErrFileNotFound
	}
	oldPhotoID := f.PhotoID
	oldPhoto := s.hydrateLocked(s.photos[oldPhotoID])
	created := clonePhoto(newPhoto)
	if created.ViewCount == 0 {
		created.ViewCount = oldPhoto.ViewCount
	}
	if len(created.EmojiTags) == 0 {
		created.EmojiTags = slices.Clone(oldPhoto.EmojiTags)
	}
	created.Files = nil
	s.photos[created.ID] = created

	f.PhotoID = created.ID
	f.RelativePath = domain.NormalizeRelativePath(move.RelativePath)
	f.FileName = move.FileName
	f.FolderPath = domain.NormalizeRelativePath(move.FolderPath)
	f.Status = domain.FileStatusActive
	f.UpdatedAt = now
	s.files[f.ID] = f
	s.refreshMovedPhotoLocked(oldPhotoID, now)
	s.recomputePhotoStatusLocked(oldPhotoID)
	return nil
}

func (s *fakePhotoStore) MarkFilesMissing(_ context.Context, fileIDs []string, now time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.markMissingCalled++
	if s.markMissingErr != nil {
		return s.markMissingErr
	}
	affected := make(map[string]bool)
	for _, id := range fileIDs {
		f, ok := s.files[id]
		if !ok {
			continue
		}
		f.Status = domain.FileStatusMissing
		f.UpdatedAt = now
		s.files[id] = f
		affected[f.PhotoID] = true
	}
	for pid := range affected {
		s.recomputePhotoStatusLocked(pid)
	}
	return nil
}

func (s *fakePhotoStore) PurgeMissing(_ context.Context) (int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.purgeMissingErr != nil {
		return 0, s.purgeMissingErr
	}
	var deleted int64
	for id, f := range s.files {
		if f.Status == domain.FileStatusMissing {
			delete(s.files, id)
			deleted++
		}
	}
	for pid, p := range s.photos {
		if p.Status == domain.FileStatusMissing && len(s.filesForPhotoLocked(pid)) == 0 {
			delete(s.photos, pid)
			deleted++
		}
	}
	return deleted, nil
}

func (s *fakePhotoStore) findFileLocked(file domain.PhotoFile) (domain.PhotoFile, bool) {
	if file.ID != "" {
		f, ok := s.files[file.ID]
		return f, ok
	}
	norm := domain.NormalizeRelativePath(file.RelativePath)
	for _, f := range s.files {
		if domain.NormalizeRelativePath(f.RelativePath) == norm {
			return f, true
		}
	}
	return domain.PhotoFile{}, false
}

func (s *fakePhotoStore) filesForPhotoLocked(photoID string) []domain.PhotoFile {
	var out []domain.PhotoFile
	for _, f := range s.files {
		if f.PhotoID == photoID {
			out = append(out, f)
		}
	}
	return domain.SortPhotoFilesByPriority(out)
}

func (s *fakePhotoStore) hydrateLocked(p domain.Photo) domain.Photo {
	out := clonePhoto(p)
	out.Files = s.filesForPhotoLocked(p.ID)
	out.Formats = domain.UniqueSortedFormats(out.Files)
	return out
}

func (s *fakePhotoStore) recomputePhotoStatusLocked(photoID string) {
	p, ok := s.photos[photoID]
	if !ok {
		return
	}
	p.Status = domain.FileStatusMissing
	for _, f := range s.filesForPhotoLocked(photoID) {
		if f.Status == domain.FileStatusActive {
			p.Status = domain.FileStatusActive
			break
		}
	}
	s.photos[photoID] = p
}

func (s *fakePhotoStore) refreshMovedPhotoLocked(photoID string, now time.Time) {
	p, ok := s.photos[photoID]
	if !ok {
		return
	}
	files := s.filesForPhotoLocked(photoID)
	var active []domain.PhotoFile
	for _, f := range files {
		if f.Status == domain.FileStatusActive {
			active = append(active, f)
		}
	}
	if len(active) > 0 {
		p.Status = domain.FileStatusActive
		primary := active[0]
		p.FolderPath = primary.FolderPath
		p.BaseName = strings.TrimSuffix(primary.FileName, path.Ext(primary.FileName))
		p.StackKey = domain.ComputeStackKey(p.FolderPath, p.MediaType, p.BaseName, primary.Format)
		p.UpdatedAt = now
	}
	s.photos[photoID] = p
}

func clonePhoto(p domain.Photo) domain.Photo {
	out := p
	out.Formats = slices.Clone(p.Formats)
	out.Files = slices.Clone(p.Files)
	out.EmojiTags = slices.Clone(p.EmojiTags)
	return out
}

type fakeSyncRunStore struct {
	mu             sync.Mutex
	runs           map[string]domain.SyncReport
	order          []string
	createErr      error
	updateErr      error
	getLatestErr   error
	recoverErr     error
	recoverCalls   int
	lastReason     string
	lastRecoverNow time.Time
}

func newFakeSyncRunStore() *fakeSyncRunStore {
	return &fakeSyncRunStore{runs: make(map[string]domain.SyncReport)}
}

func (r *fakeSyncRunStore) CreateSyncRun(_ context.Context, report domain.SyncReport) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.createErr != nil {
		return r.createErr
	}
	r.runs[report.ID] = report
	r.order = append(r.order, report.ID)
	return nil
}

func (r *fakeSyncRunStore) UpdateSyncRun(_ context.Context, report domain.SyncReport) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.updateErr != nil {
		return r.updateErr
	}
	if _, ok := r.runs[report.ID]; !ok {
		return domain.ErrSyncNotFound
	}
	r.runs[report.ID] = report
	return nil
}

func (r *fakeSyncRunStore) GetLatestSyncRun(_ context.Context) (domain.SyncReport, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.getLatestErr != nil {
		return domain.SyncReport{}, r.getLatestErr
	}
	if len(r.order) == 0 {
		return domain.SyncReport{}, domain.ErrSyncNotFound
	}
	return r.runs[r.order[len(r.order)-1]], nil
}

func (r *fakeSyncRunStore) ListSyncRuns(_ context.Context, limit int) ([]domain.SyncReport, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	var out []domain.SyncReport
	for i := len(r.order) - 1; i >= 0 && len(out) < limit; i-- {
		out = append(out, r.runs[r.order[i]])
	}
	return out, nil
}

func (r *fakeSyncRunStore) RecoverStaleRuns(_ context.Context, reason string, now time.Time) (int64, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.recoverCalls++
	r.lastReason = reason
	r.lastRecoverNow = now
	if r.recoverErr != nil {
		return 0, r.recoverErr
	}
	var count int64
	for id, rep := range r.runs {
		if rep.Status == domain.SyncStatusRunning {
			rep.Status = domain.SyncStatusFailed
			rep.ErrorMessage = reason
			t := now
			rep.FinishedAt = &t
			r.runs[id] = rep
			count++
		}
	}
	return count, nil
}

type fakeMaintainer struct {
	mu              sync.Mutex
	backupSuffixes  []string
	checkpointCalls int
	backupErr       error
	checkpointErr   error
}

func (m *fakeMaintainer) Backup(_ context.Context, suffix string) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.backupSuffixes = append(m.backupSuffixes, suffix)
	if m.backupErr != nil {
		return "", m.backupErr
	}
	return "/tmp/photos.db.bak." + suffix, nil
}

func (m *fakeMaintainer) Checkpoint(_ context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.checkpointCalls++
	return m.checkpointErr
}

type fakeMediaScanner struct {
	mu          sync.Mutex
	walkFiles   []domain.ScannedFile
	walkErr     error
	hashErr     error
	hashesByRel map[string]string
	hashedInput []domain.ScannedFile
	hashCalls   int
	walkHook    func(ctx context.Context)
}

func newFakeMediaScanner(files []domain.ScannedFile, hashes map[string]string) *fakeMediaScanner {
	return &fakeMediaScanner{
		walkFiles:   slices.Clone(files),
		hashesByRel: maps.Clone(hashes),
	}
}

func (s *fakeMediaScanner) Walk(ctx context.Context) ([]domain.ScannedFile, error) {
	if s.walkHook != nil {
		s.walkHook(ctx)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.walkErr != nil {
		return nil, s.walkErr
	}
	return slices.Clone(s.walkFiles), nil
}

func (s *fakeMediaScanner) HashFiles(_ context.Context, files []domain.ScannedFile) ([]domain.ScannedFile, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.hashCalls++
	s.hashedInput = slices.Clone(files)
	if s.hashErr != nil {
		return nil, s.hashErr
	}
	out := make([]domain.ScannedFile, 0, len(files))
	for _, f := range files {
		item := f
		if h, ok := s.hashesByRel[domain.NormalizeRelativePath(f.RelativePath)]; ok {
			item.ContentHash = h
		} else if h, ok := s.hashesByRel[f.RelativePath]; ok {
			item.ContentHash = h
		}
		out = append(out, item)
	}
	return out, nil
}

type fakeMetadataExtractor struct {
	byPath map[string]domain.MediaMetadata
	errBy  map[string]error
	calls  int
}

func (e *fakeMetadataExtractor) Extract(_ context.Context, relativePath string, _ domain.MediaFormat) (domain.MediaMetadata, error) {
	e.calls++
	if err := e.errBy[relativePath]; err != nil {
		return domain.MediaMetadata{}, err
	}
	return e.byPath[relativePath], nil
}

type fakePreviewExtractor struct {
	mu          sync.Mutex
	ensured     []string
	orientation map[string]int
	errBy       map[string]error
}

func newFakePreviewExtractor() *fakePreviewExtractor {
	return &fakePreviewExtractor{
		orientation: make(map[string]int),
		errBy:       make(map[string]error),
	}
}

func (p *fakePreviewExtractor) EnsureCache(
	_ context.Context,
	relativePath, _ string,
	_ domain.MediaFormat,
	orientation int,
) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if err := p.errBy[relativePath]; err != nil {
		return err
	}
	p.ensured = append(p.ensured, relativePath)
	p.orientation[relativePath] = orientation
	return nil
}

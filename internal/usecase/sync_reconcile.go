package usecase

import (
	"cmp"
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"path"
	"slices"
	"strings"
	"time"

	"github.com/lucastheo/photos/internal/domain"
)

type syncDiff struct {
	existingByPath map[string]domain.PhotoFile
	toHash         []domain.ScannedFile
	unseen         []domain.PhotoFile
	unseenIDs      map[string]bool
	unseenByHash   map[string][]domain.PhotoFile
	consumedIDs    map[string]bool
}

type matchedMove struct {
	sf   domain.ScannedFile
	cand domain.PhotoFile
}

type splitState struct {
	unseenIDs       map[string]bool
	movedDestFolder map[string]string
	splitFileIDs    map[string]bool
	retainedPhoto   map[string]bool
}

func normalizeScannedFiles(raw []domain.ScannedFile) []domain.ScannedFile {
	out := make([]domain.ScannedFile, 0, len(raw))
	seen := make(map[string]struct{}, len(raw))
	for _, sf := range raw {
		norm := normalizeScannedFile(sf)
		if _, exists := seen[norm.RelativePath]; exists {
			continue
		}
		seen[norm.RelativePath] = struct{}{}
		out = append(out, norm)
	}
	return out
}

func normalizeScannedFile(sf domain.ScannedFile) domain.ScannedFile {
	out := sf
	out.RelativePath = domain.NormalizeRelativePath(sf.RelativePath)
	out.FileName = domain.NormalizeNFC(sf.FileName)
	if out.FileName == "" && out.RelativePath != "" {
		out.FileName = path.Base(out.RelativePath)
	}
	if strings.TrimSpace(sf.FolderPath) == "" && out.RelativePath != "" {
		dir := path.Dir(out.RelativePath)
		if dir != "." {
			out.FolderPath = dir
		}
	} else {
		out.FolderPath = domain.NormalizeRelativePath(sf.FolderPath)
	}
	out.BaseName = domain.NormalizeNFC(sf.BaseName)
	if out.BaseName == "" && out.FileName != "" {
		out.BaseName = strings.TrimSuffix(out.FileName, path.Ext(out.FileName))
	}
	if out.MediaType == "" && out.Format != "" {
		if mt, ok := domain.MediaTypeForFormat(out.Format); ok {
			out.MediaType = mt
		}
	}
	return out
}

func hasActiveFile(files []domain.PhotoFile) bool {
	for _, f := range files {
		if f.Status == domain.FileStatusActive {
			return true
		}
	}
	return false
}

func (u *SyncLibraryUseCase) computeFastDiff(
	ctx context.Context,
	scanned []domain.ScannedFile,
	existing []domain.PhotoFile,
	now time.Time,
	report *domain.SyncReport,
) (*syncDiff, error) {
	diff := &syncDiff{
		existingByPath: make(map[string]domain.PhotoFile, len(existing)),
		unseenIDs:      make(map[string]bool),
		unseenByHash:   make(map[string][]domain.PhotoFile),
		consumedIDs:    make(map[string]bool),
	}
	for _, f := range existing {
		diff.existingByPath[domain.NormalizeRelativePath(f.RelativePath)] = f
	}

	seenIDs := make(map[string]bool, len(existing))
	for _, sf := range scanned {
		dbFile, ok := diff.existingByPath[sf.RelativePath]
		if !ok {
			diff.toHash = append(diff.toHash, sf)
			continue
		}
		seenIDs[dbFile.ID] = true
		if !fastMetadataMatches(dbFile, sf) {
			diff.toHash = append(diff.toHash, sf)
			continue
		}
		if dbFile.Status == domain.FileStatusActive {
			report.UnchangedCount++
			continue
		}
		if err := u.reactivateFile(ctx, dbFile, now, report); err != nil {
			return nil, err
		}
	}

	collectUnseenCandidates(existing, seenIDs, diff)
	return diff, nil
}

func fastMetadataMatches(dbFile domain.PhotoFile, sf domain.ScannedFile) bool {
	return dbFile.FileSize == sf.FileSize && modTimeUnixNs(dbFile.ModTime) == modTimeUnixNs(sf.ModTime)
}

func modTimeUnixNs(t time.Time) int64 {
	if t.IsZero() {
		return 0
	}
	return t.UTC().UnixNano()
}

func (u *SyncLibraryUseCase) reactivateFile(
	ctx context.Context,
	dbFile domain.PhotoFile,
	now time.Time,
	report *domain.SyncReport,
) error {
	updated := dbFile
	updated.Status = domain.FileStatusActive
	updated.UpdatedAt = now
	if err := u.photos.UpdateFileInPlace(ctx, updated); err != nil {
		return fmt.Errorf("reactivate file %q: %w", dbFile.RelativePath, err)
	}
	report.UpdatedCount++
	return nil
}

func collectUnseenCandidates(existing []domain.PhotoFile, seenIDs map[string]bool, diff *syncDiff) {
	for _, f := range existing {
		if seenIDs[f.ID] {
			continue
		}
		diff.unseen = append(diff.unseen, f)
		diff.unseenIDs[f.ID] = true
		diff.unseenByHash[f.ContentHash] = append(diff.unseenByHash[f.ContentHash], f)
	}
	for hash, cands := range diff.unseenByHash {
		slices.SortFunc(cands, compareMoveCandidates)
		diff.unseenByHash[hash] = cands
	}
}

func compareMoveCandidates(a, b domain.PhotoFile) int {
	if r := cmp.Compare(candidateStatusRank(a.Status), candidateStatusRank(b.Status)); r != 0 {
		return r
	}
	if r := cmp.Compare(a.RelativePath, b.RelativePath); r != 0 {
		return r
	}
	return cmp.Compare(a.ID, b.ID)
}

func candidateStatusRank(s domain.FileStatus) int {
	if s == domain.FileStatusActive {
		return 0
	}
	return 1
}

func (u *SyncLibraryUseCase) reconcileHashed(
	ctx context.Context,
	rawHashed []domain.ScannedFile,
	diff *syncDiff,
	now time.Time,
	report *domain.SyncReport,
) error {
	hashed := make([]domain.ScannedFile, 0, len(rawHashed))
	for _, sf := range rawHashed {
		hashed = append(hashed, normalizeScannedFile(sf))
	}

	var newPaths []domain.ScannedFile
	for _, sf := range hashed {
		dbFile, exists := diff.existingByPath[sf.RelativePath]
		if !exists {
			newPaths = append(newPaths, sf)
			continue
		}
		if err := u.applyInPlaceEdit(ctx, dbFile, sf, now, report); err != nil {
			return err
		}
	}

	remaining, err := u.applyMovesAndSplits(ctx, newPaths, diff, now, report)
	if err != nil {
		return err
	}
	for _, sf := range remaining {
		if err := u.applyNewDuplicateOrStack(ctx, sf, now, report); err != nil {
			return err
		}
	}
	return nil
}

func (u *SyncLibraryUseCase) applyInPlaceEdit(
	ctx context.Context,
	dbFile domain.PhotoFile,
	sf domain.ScannedFile,
	now time.Time,
	report *domain.SyncReport,
) error {
	updated := dbFile
	updated.ContentHash = sf.ContentHash
	updated.FileSize = sf.FileSize
	updated.ModTime = sf.ModTime
	updated.Status = domain.FileStatusActive
	updated.UpdatedAt = now
	if err := u.photos.UpdateFileInPlace(ctx, updated); err != nil {
		return fmt.Errorf("update edited file %q: %w", sf.RelativePath, err)
	}

	parent, err := u.photos.GetByID(ctx, dbFile.PhotoID)
	if err != nil {
		return fmt.Errorf("load photo %q after edit: %w", dbFile.PhotoID, err)
	}
	meta, hasMeta, err := u.extractMetadata(ctx, sf)
	if err != nil {
		return err
	}
	orientation := parent.EXIF.Orientation
	if hasMeta {
		orientation = meta.EXIF.Orientation
	}
	if err := u.ensurePreviewCache(ctx, sf, orientation); err != nil {
		return err
	}
	if hasMeta && isPrimaryPhotoFile(parent, dbFile.ID) {
		if err := u.updateParentPhotoMetadata(ctx, parent, meta, sf.ModTime, now); err != nil {
			return err
		}
	}
	report.UpdatedCount++
	return nil
}

func isPrimaryPhotoFile(photo domain.Photo, fileID string) bool {
	primary, ok := domain.PrimaryFile(photo.Files)
	return ok && primary.ID == fileID
}

func (u *SyncLibraryUseCase) applyMovesAndSplits(
	ctx context.Context,
	newPaths []domain.ScannedFile,
	diff *syncDiff,
	now time.Time,
	report *domain.SyncReport,
) ([]domain.ScannedFile, error) {
	moves, remaining, state := matchMoveCandidates(newPaths, diff)
	var batchMoves []domain.FileMove
	var movedUpdates []domain.PhotoFile

	for _, m := range moves {
		parent, err := u.photos.GetByID(ctx, m.cand.PhotoID)
		if err != nil {
			return nil, fmt.Errorf("load parent photo %q for move: %w", m.cand.PhotoID, err)
		}
		if shouldSplitMove(parent, m, state) {
			if err := u.executeSplitMove(ctx, parent, m, state, now); err != nil {
				return nil, err
			}
			report.MovedCount++
			continue
		}
		batchMoves = append(batchMoves, buildFileMove(m))
		if !fastMetadataMatches(m.cand, m.sf) {
			movedUpdates = append(movedUpdates, buildMovedFileUpdate(m.cand, m.cand.PhotoID, m.sf, now))
		}
		report.MovedCount++
	}
	if err := u.flushBatchMoves(ctx, batchMoves, movedUpdates, now); err != nil {
		return nil, err
	}
	return remaining, nil
}

func matchMoveCandidates(
	newPaths []domain.ScannedFile,
	diff *syncDiff,
) ([]matchedMove, []domain.ScannedFile, *splitState) {
	var moves []matchedMove
	var remaining []domain.ScannedFile
	state := &splitState{
		unseenIDs:       diff.unseenIDs,
		movedDestFolder: make(map[string]string),
		splitFileIDs:    make(map[string]bool),
		retainedPhoto:   make(map[string]bool),
	}
	for _, sf := range newPaths {
		cand, ok := takeUnconsumedCandidate(diff.unseenByHash, diff.consumedIDs, sf.ContentHash)
		if !ok {
			remaining = append(remaining, sf)
			continue
		}
		moves = append(moves, matchedMove{sf: sf, cand: cand})
		state.movedDestFolder[cand.ID] = sf.FolderPath
	}
	return moves, remaining, state
}

func takeUnconsumedCandidate(
	byHash map[string][]domain.PhotoFile,
	consumed map[string]bool,
	hash string,
) (domain.PhotoFile, bool) {
	for _, cand := range byHash[hash] {
		if consumed[cand.ID] {
			continue
		}
		consumed[cand.ID] = true
		return cand, true
	}
	return domain.PhotoFile{}, false
}

func shouldSplitMove(parent domain.Photo, m matchedMove, state *splitState) bool {
	hasDifferentHashOtherFolder := false
	hasStationaryOther := false
	for _, other := range parent.Files {
		otherFolder, keep := effectiveOtherFolder(other, m.cand.ID, state)
		if !keep {
			continue
		}
		if other.ContentHash == m.cand.ContentHash {
			return false
		}
		if otherFolder != m.sf.FolderPath {
			hasDifferentHashOtherFolder = true
			if _, moving := state.movedDestFolder[other.ID]; !moving {
				hasStationaryOther = true
			}
		}
	}
	if !hasDifferentHashOtherFolder {
		return false
	}
	if hasStationaryOther || state.retainedPhoto[m.cand.PhotoID] {
		return true
	}
	state.retainedPhoto[m.cand.PhotoID] = true
	return false
}

func effectiveOtherFolder(other domain.PhotoFile, selfID string, state *splitState) (string, bool) {
	if other.ID == selfID || other.Status != domain.FileStatusActive || state.splitFileIDs[other.ID] {
		return "", false
	}
	if destFolder, moving := state.movedDestFolder[other.ID]; moving {
		return destFolder, true
	}
	if state.unseenIDs[other.ID] {
		return "", false
	}
	return domain.NormalizeRelativePath(other.FolderPath), true
}

func (u *SyncLibraryUseCase) executeSplitMove(
	ctx context.Context,
	parent domain.Photo,
	m matchedMove,
	state *splitState,
	now time.Time,
) error {
	newPhoto := domain.Photo{
		ID:           u.newID(),
		StackKey:     domain.ComputeStackKey(m.sf.FolderPath, m.sf.MediaType, m.sf.BaseName, m.sf.Format),
		BaseName:     m.sf.BaseName,
		FolderPath:   m.sf.FolderPath,
		MediaType:    m.sf.MediaType,
		Width:        parent.Width,
		Height:       parent.Height,
		DurationSec:  parent.DurationSec,
		TakenAt:      parent.TakenAt,
		EXIF:         parent.EXIF,
		ViewCount:    parent.ViewCount,
		EmojiTags:    slices.Clone(parent.EmojiTags),
		LastViewedAt: parent.LastViewedAt,
		Status:       domain.FileStatusActive,
		CreatedAt:    now,
		UpdatedAt:    now,
	}
	if err := u.photos.SplitMovedFile(ctx, m.cand.ID, newPhoto, buildFileMove(m), now); err != nil {
		return fmt.Errorf("split moved file %q: %w", m.cand.RelativePath, err)
	}
	state.splitFileIDs[m.cand.ID] = true
	if !fastMetadataMatches(m.cand, m.sf) {
		updated := buildMovedFileUpdate(m.cand, newPhoto.ID, m.sf, now)
		if err := u.photos.UpdateFileInPlace(ctx, updated); err != nil {
			return fmt.Errorf("update split file stats %q: %w", m.sf.RelativePath, err)
		}
	}
	return nil
}

func (u *SyncLibraryUseCase) flushBatchMoves(
	ctx context.Context,
	moves []domain.FileMove,
	updates []domain.PhotoFile,
	now time.Time,
) error {
	if len(moves) > 0 {
		if err := u.photos.MoveFilesBatch(ctx, moves, now); err != nil {
			return fmt.Errorf("move files batch: %w", err)
		}
	}
	for _, uf := range updates {
		if err := u.photos.UpdateFileInPlace(ctx, uf); err != nil {
			return fmt.Errorf("update moved file stats %q: %w", uf.RelativePath, err)
		}
	}
	return nil
}

func buildFileMove(m matchedMove) domain.FileMove {
	return domain.FileMove{
		FileID:       m.cand.ID,
		RelativePath: m.sf.RelativePath,
		FileName:     m.sf.FileName,
		FolderPath:   m.sf.FolderPath,
	}
}

func buildMovedFileUpdate(cand domain.PhotoFile, photoID string, sf domain.ScannedFile, now time.Time) domain.PhotoFile {
	updated := cand
	updated.PhotoID = photoID
	updated.RelativePath = sf.RelativePath
	updated.FileName = sf.FileName
	updated.FolderPath = sf.FolderPath
	updated.FileSize = sf.FileSize
	updated.ModTime = sf.ModTime
	updated.Status = domain.FileStatusActive
	updated.UpdatedAt = now
	return updated
}

func (u *SyncLibraryUseCase) applyNewDuplicateOrStack(
	ctx context.Context,
	sf domain.ScannedFile,
	now time.Time,
	report *domain.SyncReport,
) error {
	matches, err := u.photos.FindFilesByContentHash(ctx, sf.ContentHash)
	if err != nil {
		return fmt.Errorf("find files by hash %q: %w", sf.ContentHash, err)
	}
	for _, m := range matches {
		if m.Status == domain.FileStatusActive {
			return u.attachDuplicateFile(ctx, m.PhotoID, sf, now, report)
		}
	}

	if sf.MediaType == domain.MediaTypePhoto {
		stackKey := domain.ComputeStackKey(sf.FolderPath, sf.MediaType, sf.BaseName, sf.Format)
		existing, err := u.photos.GetByStackKey(ctx, stackKey)
		if err != nil && !errors.Is(err, domain.ErrPhotoNotFound) {
			return fmt.Errorf("lookup stack key %q: %w", stackKey, err)
		}
		if err == nil && existing.Status == domain.FileStatusActive {
			return u.attachStackedFile(ctx, existing, sf, now, report)
		}
	}
	return u.createNewPhoto(ctx, sf, now, report)
}

func (u *SyncLibraryUseCase) attachDuplicateFile(
	ctx context.Context,
	photoID string,
	sf domain.ScannedFile,
	now time.Time,
	report *domain.SyncReport,
) error {
	file := buildNewPhotoFile(u.newID(), photoID, sf, now)
	if err := u.photos.AttachFile(ctx, photoID, file); err != nil {
		return fmt.Errorf("attach duplicate file %q: %w", sf.RelativePath, err)
	}
	if err := u.ensurePreviewCache(ctx, sf, 0); err != nil {
		return err
	}
	report.DuplicateCount++
	return nil
}

func (u *SyncLibraryUseCase) attachStackedFile(
	ctx context.Context,
	parent domain.Photo,
	sf domain.ScannedFile,
	now time.Time,
	report *domain.SyncReport,
) error {
	file := buildNewPhotoFile(u.newID(), parent.ID, sf, now)
	if err := u.photos.AttachFile(ctx, parent.ID, file); err != nil {
		return fmt.Errorf("attach stacked file %q: %w", sf.RelativePath, err)
	}
	meta, hasMeta, err := u.extractMetadata(ctx, sf)
	if err != nil {
		return err
	}
	if err := u.ensurePreviewCache(ctx, sf, meta.EXIF.Orientation); err != nil {
		return err
	}
	if hasMeta && hasStrictlyHigherPriority(sf.Format, parent.Files) {
		if err := u.updateParentPhotoMetadata(ctx, parent, meta, sf.ModTime, now); err != nil {
			return err
		}
	}
	report.StackedCount++
	return nil
}

func hasStrictlyHigherPriority(format domain.MediaFormat, existing []domain.PhotoFile) bool {
	newPrio := domain.FormatPriority(format)
	for _, f := range existing {
		if f.Status == domain.FileStatusActive && domain.FormatPriority(f.Format) >= newPrio {
			return false
		}
	}
	return true
}

func (u *SyncLibraryUseCase) createNewPhoto(
	ctx context.Context,
	sf domain.ScannedFile,
	now time.Time,
	report *domain.SyncReport,
) error {
	meta, _, err := u.extractMetadata(ctx, sf)
	if err != nil {
		return err
	}
	if err := u.ensurePreviewCache(ctx, sf, meta.EXIF.Orientation); err != nil {
		return err
	}

	photoID := u.newID()
	takenAt := meta.TakenAt
	if takenAt == nil {
		t := sf.ModTime
		takenAt = &t
	}
	photo := domain.Photo{
		ID:          photoID,
		StackKey:    domain.ComputeStackKey(sf.FolderPath, sf.MediaType, sf.BaseName, sf.Format),
		BaseName:    sf.BaseName,
		FolderPath:  sf.FolderPath,
		MediaType:   sf.MediaType,
		Formats:     []domain.MediaFormat{sf.Format},
		Files:       []domain.PhotoFile{buildNewPhotoFile(u.newID(), photoID, sf, now)},
		Width:       meta.Width,
		Height:      meta.Height,
		DurationSec: meta.DurationSec,
		TakenAt:     takenAt,
		EXIF:        meta.EXIF,
		ViewCount:   0,
		Status:      domain.FileStatusActive,
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	if err := u.photos.CreatePhoto(ctx, photo); err != nil {
		return fmt.Errorf("create photo %q: %w", sf.RelativePath, err)
	}
	report.AddedCount++
	return nil
}

func buildNewPhotoFile(fileID, photoID string, sf domain.ScannedFile, now time.Time) domain.PhotoFile {
	return domain.PhotoFile{
		ID:           fileID,
		PhotoID:      photoID,
		ContentHash:  sf.ContentHash,
		RelativePath: sf.RelativePath,
		FileName:     sf.FileName,
		FolderPath:   sf.FolderPath,
		Format:       sf.Format,
		FileSize:     sf.FileSize,
		ModTime:      sf.ModTime,
		Status:       domain.FileStatusActive,
		CreatedAt:    now,
		UpdatedAt:    now,
	}
}

func (u *SyncLibraryUseCase) extractMetadata(ctx context.Context, sf domain.ScannedFile) (domain.MediaMetadata, bool, error) {
	if u.extractor == nil {
		return domain.MediaMetadata{}, false, nil
	}
	meta, err := u.extractor.Extract(ctx, sf.RelativePath, sf.Format)
	if err != nil {
		return domain.MediaMetadata{}, false, fmt.Errorf("extract metadata %q: %w", sf.RelativePath, err)
	}
	return meta, true, nil
}

func (u *SyncLibraryUseCase) ensurePreviewCache(ctx context.Context, sf domain.ScannedFile, orientation int) error {
	if u.previews == nil {
		return nil
	}
	if err := u.previews.EnsureCache(ctx, sf.RelativePath, sf.ContentHash, sf.Format, orientation); err != nil {
		return fmt.Errorf("ensure preview cache %q: %w", sf.RelativePath, err)
	}
	return nil
}

func (u *SyncLibraryUseCase) updateParentPhotoMetadata(
	ctx context.Context,
	parent domain.Photo,
	meta domain.MediaMetadata,
	fallbackModTime, now time.Time,
) error {
	updated := parent
	if meta.Width > 0 && meta.Height > 0 {
		updated.Width = meta.Width
		updated.Height = meta.Height
	}
	if meta.DurationSec > 0 {
		updated.DurationSec = meta.DurationSec
	}
	if meta.TakenAt != nil {
		updated.TakenAt = meta.TakenAt
	} else if updated.TakenAt == nil {
		t := fallbackModTime
		updated.TakenAt = &t
	}
	updated.EXIF = meta.EXIF
	updated.UpdatedAt = now
	if err := u.photos.UpdatePhotoMetadata(ctx, updated); err != nil {
		return fmt.Errorf("update photo metadata %q: %w", parent.ID, err)
	}
	return nil
}

func (u *SyncLibraryUseCase) markRemainingMissing(
	ctx context.Context,
	unseen []domain.PhotoFile,
	consumedIDs map[string]bool,
	now time.Time,
	report *domain.SyncReport,
) error {
	var missingIDs []string
	for _, f := range unseen {
		if consumedIDs[f.ID] || f.Status != domain.FileStatusActive {
			continue
		}
		missingIDs = append(missingIDs, f.ID)
	}
	if len(missingIDs) == 0 {
		return nil
	}
	if err := u.photos.MarkFilesMissing(ctx, missingIDs, now); err != nil {
		return fmt.Errorf("mark missing files: %w", err)
	}
	report.MissingCount = len(missingIDs)
	return nil
}

func newUUIDv4() string {
	var b [16]byte
	_, _ = rand.Read(b[:])
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	return fmt.Sprintf("%08x-%04x-%04x-%04x-%012x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}

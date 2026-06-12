package maintenance

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"filesyncengine/internal/block"
	"filesyncengine/internal/state"
)

type FileScrubIssueKind string

type FileScrubClassification string

type FileScrubVerifyMode string

const (
	FileScrubMissingFile      FileScrubIssueKind = "missing-file"
	FileScrubUnreadableFile   FileScrubIssueKind = "unreadable-file"
	FileScrubHashMismatch     FileScrubIssueKind = "hash-mismatch"
	FileScrubMetadataMismatch FileScrubIssueKind = "metadata-mismatch"
	FileScrubUnsafePath       FileScrubIssueKind = "unsafe-path"

	FileScrubLikelyLocalEdit     FileScrubClassification = "likely-local-edit"
	FileScrubSuspectedCorruption FileScrubClassification = "suspected-corruption"
	FileScrubNeedsUserReview     FileScrubClassification = "needs-user-review"

	FileScrubFullBlocks    FileScrubVerifyMode = "full-blocks"
	FileScrubSampledBlocks FileScrubVerifyMode = "sampled-blocks"
	FileScrubLightMetadata FileScrubVerifyMode = "light-metadata"
)

type FileScrubIssue struct {
	FolderID       string
	Path           string
	Kind           FileScrubIssueKind
	Classification FileScrubClassification
	ExpectedSize   int64
	ActualSize     int64
	ExpectedMTime  int64
	ActualMTime    int64
	Evidence       string
	Err            error
}

type FileScrubEvidenceKind string

type FileScrubEvidence struct {
	Kind    FileScrubEvidenceKind
	Message string
}

const (
	FileScrubEvidenceWatcherError  FileScrubEvidenceKind = "watcher-error"
	FileScrubEvidenceReadError     FileScrubEvidenceKind = "read-error"
	FileScrubEvidenceChecksumError FileScrubEvidenceKind = "checksum-error"
)

type FileScrubPendingWriteStore interface {
	PendingWrites(folderID string) ([]state.PendingWrite, error)
}

type FileScrubEvidenceStore interface {
	FileScrubEvidence(folderID string, path string) ([]FileScrubEvidence, error)
}

type FileScrubRetryEvidence struct {
	ConsecutiveMismatches int
	ExpectedFingerprint   string
	ActualFingerprint     string
}

type FileScrubRetryEvidenceStore interface {
	FileScrubRetryEvidence(folderID string, path string) (FileScrubRetryEvidence, error)
}

type FileScrubConsensusEvidence struct {
	TrustedExpectedMatches int
	TrustedActualMatches   int
}

type FileScrubConsensusEvidenceStore interface {
	FileScrubConsensusEvidence(folderID string, path string, expected block.Manifest, actual block.Manifest) (FileScrubConsensusEvidence, error)
}

type FileScrubDamageMarker interface {
	SaveManifest(folderID string, relativePath string, manifest block.Manifest) error
}

// FileScrubCrawler verifies real file bytes against stored manifests in small,
// resumable steps. It does not prune metadata, quarantine bytes, or repair files.
// When the store supports it, suspected corruption is marked on the manifest so
// those blocks stop being advertised as valid reuse sources while the original
// bytes remain untouched for later quarantine/repair policy.
type FileScrubCrawler struct {
	Store              ManifestStore
	FolderIDs          []string
	Roots              map[string]string
	VerifyMode         FileScrubVerifyMode
	SampleEveryNBlocks int
	Report             func(FileScrubIssue)
}

func (c FileScrubCrawler) Step(ctx context.Context, cursor Cursor) (StepResult, error) {
	if err := ctx.Err(); err != nil {
		return StepResult{}, err
	}
	records, err := c.records()
	if err != nil {
		return StepResult{}, err
	}
	idx := int(cursor.Position)
	if idx >= len(records) {
		return StepResult{Cursor: Cursor{}, Complete: true}, nil
	}
	record := records[idx]
	result := StepResult{
		Cursor:       Cursor{Position: uint64(idx + 1), FolderID: record.folderID, Path: record.path, Revision: record.revision},
		FilesScanned: 1,
		BytesScanned: record.manifest.Size,
		Complete:     idx+1 >= len(records),
	}
	issue, err := c.verify(record)
	if err != nil {
		return StepResult{}, err
	}
	if issue != nil {
		result.Reported = 1
		if c.Report != nil {
			c.Report(*issue)
		}
	}
	return result, nil
}

func (c FileScrubCrawler) records() ([]manifestRecord, error) {
	return ManifestCrawler{Store: c.Store, FolderIDs: c.FolderIDs}.records()
}

func (c FileScrubCrawler) verify(record manifestRecord) (*FileScrubIssue, error) {
	root := c.Roots[record.folderID]
	path, ok := containedPath(root, record.path)
	if !ok {
		return &FileScrubIssue{FolderID: record.folderID, Path: record.path, Kind: FileScrubUnsafePath}, nil
	}
	mode := c.verifyMode()
	if mode == FileScrubLightMetadata {
		return c.verifyLightMetadata(record, path)
	}
	if !isVerifiableManifest(record.manifest) {
		return nil, nil
	}
	if mode == FileScrubSampledBlocks {
		return c.verifySampledBlocks(record, path)
	}
	actual, err := block.BuildManifest(path, record.manifest.BlockSize)
	if err != nil {
		kind := FileScrubUnreadableFile
		if errors.Is(err, os.ErrNotExist) {
			kind = FileScrubMissingFile
		}
		return &FileScrubIssue{FolderID: record.folderID, Path: record.path, Kind: kind, Err: err}, nil
	}
	if !manifestBytesMatch(record.manifest, actual) {
		classification, evidence, err := c.classifyMismatch(record, actual)
		if err != nil {
			return nil, err
		}
		issue := &FileScrubIssue{
			FolderID:       record.folderID,
			Path:           record.path,
			Kind:           FileScrubHashMismatch,
			Classification: classification,
			ExpectedSize:   record.manifest.Size,
			ActualSize:     actual.Size,
			ExpectedMTime:  record.manifest.ModTimeUnixNano,
			ActualMTime:    actual.ModTimeUnixNano,
			Evidence:       evidence,
		}
		if classification == FileScrubSuspectedCorruption {
			if err := c.markDamaged(record); err != nil {
				return nil, err
			}
		}
		return issue, nil
	}
	return nil, nil
}

func (c FileScrubCrawler) verifyMode() FileScrubVerifyMode {
	if c.VerifyMode == "" {
		return FileScrubFullBlocks
	}
	return c.VerifyMode
}

func (c FileScrubCrawler) verifyLightMetadata(record manifestRecord, path string) (*FileScrubIssue, error) {
	info, err := os.Stat(path)
	if err != nil {
		kind := FileScrubUnreadableFile
		if errors.Is(err, os.ErrNotExist) {
			kind = FileScrubMissingFile
		}
		return &FileScrubIssue{FolderID: record.folderID, Path: record.path, Kind: kind, Err: err}, nil
	}
	actualMTime := info.ModTime().UnixNano()
	if info.Size() == record.manifest.Size && (record.manifest.ModTimeUnixNano == 0 || record.manifest.ModTimeUnixNano == actualMTime) {
		return nil, nil
	}
	return &FileScrubIssue{
		FolderID:       record.folderID,
		Path:           record.path,
		Kind:           FileScrubMetadataMismatch,
		Classification: classifyCheapMetadata(record.manifest, info.Size(), actualMTime),
		ExpectedSize:   record.manifest.Size,
		ActualSize:     info.Size(),
		ExpectedMTime:  record.manifest.ModTimeUnixNano,
		ActualMTime:    actualMTime,
		Evidence:       "light-metadata",
	}, nil
}

func (c FileScrubCrawler) verifySampledBlocks(record manifestRecord, path string) (*FileScrubIssue, error) {
	info, err := os.Stat(path)
	if err != nil {
		kind := FileScrubUnreadableFile
		if errors.Is(err, os.ErrNotExist) {
			kind = FileScrubMissingFile
		}
		return &FileScrubIssue{FolderID: record.folderID, Path: record.path, Kind: kind, Err: err}, nil
	}
	actualMTime := info.ModTime().UnixNano()
	if info.Size() != record.manifest.Size {
		return &FileScrubIssue{FolderID: record.folderID, Path: record.path, Kind: FileScrubMetadataMismatch, Classification: FileScrubLikelyLocalEdit, ExpectedSize: record.manifest.Size, ActualSize: info.Size(), ExpectedMTime: record.manifest.ModTimeUnixNano, ActualMTime: actualMTime, Evidence: "sampled-block-verification"}, nil
	}
	mismatched, err := sampledBlocksMismatch(path, record.manifest, c.sampleEveryNBlocks())
	if err != nil {
		return &FileScrubIssue{FolderID: record.folderID, Path: record.path, Kind: FileScrubUnreadableFile, Err: err}, nil
	}
	if !mismatched {
		return nil, nil
	}
	classification, _, err := c.classifyMismatch(record, block.Manifest{Size: info.Size(), ModTimeUnixNano: actualMTime})
	if err != nil {
		return nil, err
	}
	return &FileScrubIssue{FolderID: record.folderID, Path: record.path, Kind: FileScrubHashMismatch, Classification: classification, ExpectedSize: record.manifest.Size, ActualSize: info.Size(), ExpectedMTime: record.manifest.ModTimeUnixNano, ActualMTime: actualMTime, Evidence: "sampled-block-verification"}, nil
}

func (c FileScrubCrawler) sampleEveryNBlocks() int {
	if c.SampleEveryNBlocks <= 0 {
		return 10
	}
	return c.SampleEveryNBlocks
}

func sampledBlocksMismatch(path string, manifest block.Manifest, every int) (bool, error) {
	file, err := os.Open(path)
	if err != nil {
		return false, err
	}
	defer file.Close()
	blocks := append([]block.Block(nil), manifest.Blocks...)
	sort.Slice(blocks, func(i, j int) bool { return blocks[i].Index < blocks[j].Index })
	for i, expected := range blocks {
		if i != 0 && i != len(blocks)-1 && i%every != 0 {
			continue
		}
		buf := make([]byte, expected.Size)
		n, err := file.ReadAt(buf, expected.Offset)
		if err != nil && !(errors.Is(err, io.EOF) && n == len(buf)) {
			return false, err
		}
		sum := sha256.Sum256(buf)
		if !bytes.Equal(sum[:], expected.Hash) {
			return true, nil
		}
	}
	return false, nil
}

func (c FileScrubCrawler) markDamaged(record manifestRecord) error {
	if record.manifest.Damaged {
		return nil
	}
	store, ok := c.Store.(FileScrubDamageMarker)
	if !ok {
		return nil
	}
	manifest := record.manifest
	manifest.Damaged = true
	return store.SaveManifest(record.folderID, record.path, manifest)
}

func (c FileScrubCrawler) classifyMismatch(record manifestRecord, actual block.Manifest) (FileScrubClassification, string, error) {
	if c.hasUncommittedPendingWrite(record.folderID, record.path) {
		return FileScrubNeedsUserReview, "pending-write", nil
	}
	if evidence, ok, err := c.hasRecentReadEvidence(record.folderID, record.path); err != nil {
		return "", "", err
	} else if ok {
		return FileScrubNeedsUserReview, evidence, nil
	}
	classification := classifyMismatch(record.manifest, actual)
	if classification != FileScrubSuspectedCorruption {
		return classification, "", nil
	}
	if consensus, evidence, ok, err := c.classifyWithTrustedConsensus(record, actual); err != nil {
		return "", "", err
	} else if ok {
		return consensus, evidence, nil
	}
	return c.classifyWithRepeatedVerification(record, actual)
}

func (c FileScrubCrawler) classifyWithTrustedConsensus(record manifestRecord, actual block.Manifest) (FileScrubClassification, string, bool, error) {
	store, ok := c.Store.(FileScrubConsensusEvidenceStore)
	if !ok {
		return "", "", false, nil
	}
	evidence, err := store.FileScrubConsensusEvidence(record.folderID, record.path, record.manifest, actual)
	if err != nil {
		return "", "", false, err
	}
	if evidence.TrustedExpectedMatches > 0 && evidence.TrustedActualMatches == 0 {
		return FileScrubSuspectedCorruption, "trusted-peer-consensus", true, nil
	}
	if evidence.TrustedActualMatches > 0 && evidence.TrustedExpectedMatches == 0 {
		return FileScrubNeedsUserReview, "trusted-peer-actual-consensus", true, nil
	}
	return "", "", false, nil
}

func (c FileScrubCrawler) classifyWithRepeatedVerification(record manifestRecord, actual block.Manifest) (FileScrubClassification, string, error) {
	store, ok := c.Store.(FileScrubRetryEvidenceStore)
	if !ok {
		return FileScrubSuspectedCorruption, "", nil
	}
	evidence, err := store.FileScrubRetryEvidence(record.folderID, record.path)
	if err != nil {
		return "", "", err
	}
	expectedFingerprint := manifestFingerprint(record.manifest)
	actualFingerprint := manifestFingerprint(actual)
	if evidence.ExpectedFingerprint != "" && evidence.ExpectedFingerprint != expectedFingerprint {
		return FileScrubNeedsUserReview, "awaiting-repeated-verification", nil
	}
	if evidence.ActualFingerprint != "" && evidence.ActualFingerprint != actualFingerprint {
		return FileScrubNeedsUserReview, "awaiting-repeated-verification", nil
	}
	if evidence.ConsecutiveMismatches < 2 {
		return FileScrubNeedsUserReview, "awaiting-repeated-verification", nil
	}
	return FileScrubSuspectedCorruption, "repeated-verification", nil
}

func (c FileScrubCrawler) hasRecentReadEvidence(folderID string, path string) (string, bool, error) {
	store, ok := c.Store.(FileScrubEvidenceStore)
	if !ok {
		return "", false, nil
	}
	evidence, err := store.FileScrubEvidence(folderID, path)
	if err != nil {
		return "", false, err
	}
	for _, item := range evidence {
		switch item.Kind {
		case FileScrubEvidenceReadError, FileScrubEvidenceChecksumError:
			return "read-error-history", true, nil
		case FileScrubEvidenceWatcherError:
			return "watcher-error-history", true, nil
		}
	}
	return "", false, nil
}

func (c FileScrubCrawler) hasUncommittedPendingWrite(folderID string, path string) bool {
	store, ok := c.Store.(FileScrubPendingWriteStore)
	if !ok {
		return false
	}
	writes, err := store.PendingWrites(folderID)
	if err != nil {
		return false
	}
	for _, write := range writes {
		if write.Path == path && !write.Committed {
			return true
		}
	}
	return false
}

func containedPath(root string, rel string) (string, bool) {
	if root == "" || rel == "" || filepath.IsAbs(rel) {
		return "", false
	}
	cleanRel := filepath.Clean(filepath.FromSlash(rel))
	if cleanRel == "." || cleanRel == ".." || cleanRel == string(filepath.Separator) {
		return "", false
	}
	if hasParentSegment(cleanRel) {
		return "", false
	}
	rootClean, err := filepath.Abs(root)
	if err != nil {
		return "", false
	}
	full := filepath.Join(rootClean, cleanRel)
	relToRoot, err := filepath.Rel(rootClean, full)
	if err != nil || relToRoot == ".." || filepath.IsAbs(relToRoot) || hasParentSegment(relToRoot) {
		return "", false
	}
	return full, true
}

func hasParentSegment(path string) bool {
	for _, part := range strings.Split(filepath.ToSlash(path), "/") {
		if part == ".." {
			return true
		}
	}
	return false
}

func isVerifiableManifest(manifest block.Manifest) bool {
	if manifest.BlockSize <= 0 {
		return false
	}
	if manifest.HashState != "" && manifest.HashState != "complete" {
		return false
	}
	return manifest.Size == 0 || len(manifest.Blocks) > 0
}

func manifestBytesMatch(expected block.Manifest, actual block.Manifest) bool {
	if expected.Size != actual.Size || len(expected.Blocks) != len(actual.Blocks) {
		return false
	}
	expectedBlocks := append([]block.Block(nil), expected.Blocks...)
	actualBlocks := append([]block.Block(nil), actual.Blocks...)
	sort.Slice(expectedBlocks, func(i, j int) bool { return expectedBlocks[i].Index < expectedBlocks[j].Index })
	sort.Slice(actualBlocks, func(i, j int) bool { return actualBlocks[i].Index < actualBlocks[j].Index })
	for i := range expectedBlocks {
		want := expectedBlocks[i]
		got := actualBlocks[i]
		if want.Index != got.Index || want.Offset != got.Offset || want.Size != got.Size || !bytes.Equal(want.Hash, got.Hash) {
			return false
		}
	}
	return true
}

func manifestFingerprint(manifest block.Manifest) string {
	blocks := append([]block.Block(nil), manifest.Blocks...)
	sort.Slice(blocks, func(i, j int) bool { return blocks[i].Index < blocks[j].Index })
	h := sha256.New()
	_, _ = fmt.Fprintf(h, "size=%d;blockSize=%d;blocks=%d;", manifest.Size, manifest.BlockSize, len(blocks))
	for _, b := range blocks {
		_, _ = fmt.Fprintf(h, "%d:%d:%d:", b.Index, b.Offset, b.Size)
		_, _ = h.Write(b.Hash)
		_, _ = h.Write([]byte{';'})
	}
	return hex.EncodeToString(h.Sum(nil))
}

func classifyCheapMetadata(expected block.Manifest, actualSize int64, actualMTime int64) FileScrubClassification {
	if expected.Size != actualSize {
		return FileScrubLikelyLocalEdit
	}
	if expected.ModTimeUnixNano == 0 || actualMTime == 0 {
		return FileScrubNeedsUserReview
	}
	if expected.ModTimeUnixNano != actualMTime {
		return FileScrubLikelyLocalEdit
	}
	return FileScrubSuspectedCorruption
}

func classifyMismatch(expected block.Manifest, actual block.Manifest) FileScrubClassification {
	return classifyCheapMetadata(expected, actual.Size, actual.ModTimeUnixNano)
}

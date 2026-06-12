package maintenance

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"filesyncengine/internal/block"
	"filesyncengine/internal/state"
)

func TestFileScrubCrawlerClassifiesHashMismatchWithUnchangedMetadataAsSuspectedCorruption(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "data.bin")
	if err := os.WriteFile(path, []byte("good"), 0o644); err != nil {
		t.Fatalf("write initial file: %v", err)
	}
	manifest, err := block.BuildManifest(path, 4)
	if err != nil {
		t.Fatalf("build manifest: %v", err)
	}
	manifest.Path = "data.bin"
	if err := os.WriteFile(path, []byte("evil"), 0o644); err != nil {
		t.Fatalf("corrupt file: %v", err)
	}
	if err := os.Chtimes(path, timeFromUnixNano(manifest.ModTimeUnixNano), timeFromUnixNano(manifest.ModTimeUnixNano)); err != nil {
		t.Fatalf("restore mtime: %v", err)
	}
	var issues []FileScrubIssue
	crawler := FileScrubCrawler{
		Store:     fakeManifestStore{folders: map[string]map[string]block.Manifest{"docs": {"data.bin": manifest}}},
		FolderIDs: []string{"docs"},
		Roots:     map[string]string{"docs": root},
		Report: func(issue FileScrubIssue) {
			issues = append(issues, issue)
		},
	}

	result, err := RunOnce(context.Background(), RunOptions{
		Crawler:    crawler,
		Checkpoint: &memoryCheckpoint{},
		MaxFiles:   10,
	})
	if err != nil {
		t.Fatalf("RunOnce: %v", err)
	}
	if result.FilesScanned != 1 || result.Reported != 1 || result.Pruned != 0 || result.Quarantined != 0 || !result.Complete {
		t.Fatalf("result=%+v, want one suspected corruption reported without repair/quarantine", result)
	}
	if string(mustReadFile(t, path)) != "evil" {
		t.Fatalf("scrub crawler modified file contents; want report-only behavior")
	}
	if len(issues) != 1 || issues[0].FolderID != "docs" || issues[0].Path != "data.bin" || issues[0].Kind != FileScrubHashMismatch || issues[0].Classification != FileScrubSuspectedCorruption {
		t.Fatalf("issues=%+v, want docs/data.bin suspected corruption hash mismatch", issues)
	}
}

func TestFileScrubCrawlerMarksSuspectedCorruptionDamagedSoBlocksAreNotAdvertised(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "data.bin")
	if err := os.WriteFile(path, []byte("good"), 0o644); err != nil {
		t.Fatalf("write initial file: %v", err)
	}
	manifest, err := block.BuildManifest(path, 4)
	if err != nil {
		t.Fatalf("build manifest: %v", err)
	}
	manifest.Path = "data.bin"
	store := state.NewJSONStore(filepath.Join(t.TempDir(), "state.json"))
	if err := store.SaveManifest("docs", "data.bin", manifest); err != nil {
		t.Fatalf("SaveManifest: %v", err)
	}
	if err := os.WriteFile(path, []byte("evil"), 0o644); err != nil {
		t.Fatalf("corrupt file: %v", err)
	}
	if err := os.Chtimes(path, timeFromUnixNano(manifest.ModTimeUnixNano), timeFromUnixNano(manifest.ModTimeUnixNano)); err != nil {
		t.Fatalf("restore mtime: %v", err)
	}

	result, err := RunOnce(context.Background(), RunOptions{
		Crawler: FileScrubCrawler{
			Store:     store,
			FolderIDs: []string{"docs"},
			Roots:     map[string]string{"docs": root},
		},
		Checkpoint: &memoryCheckpoint{},
		MaxFiles:   10,
	})
	if err != nil {
		t.Fatalf("RunOnce: %v", err)
	}
	if result.Reported != 1 || result.Quarantined != 0 {
		t.Fatalf("result=%+v, want suspected corruption marked damaged without quarantine", result)
	}
	loaded, ok, err := store.LoadManifest("docs", "data.bin")
	if err != nil || !ok {
		t.Fatalf("LoadManifest: loaded=%+v ok=%v err=%v", loaded, ok, err)
	}
	if !loaded.Damaged {
		t.Fatalf("loaded manifest not marked damaged: %+v", loaded)
	}
	refs, err := store.FindBlocks(4, manifest.Blocks[0].Hash)
	if err != nil {
		t.Fatalf("FindBlocks: %v", err)
	}
	if len(refs) != 0 {
		t.Fatalf("refs=%+v, want damaged manifest blocks withheld from reuse/advertising", refs)
	}
	if string(mustReadFile(t, path)) != "evil" {
		t.Fatalf("scrub crawler modified suspected damaged bytes before quarantine policy")
	}
}

func TestFileScrubCrawlerDoesNotBumpRevisionWhenAlreadyMarkedDamaged(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "data.bin")
	if err := os.WriteFile(path, []byte("good"), 0o644); err != nil {
		t.Fatalf("write initial file: %v", err)
	}
	manifest, err := block.BuildManifest(path, 4)
	if err != nil {
		t.Fatalf("build manifest: %v", err)
	}
	manifest.Path = "data.bin"
	manifest.Damaged = true
	store := state.NewJSONStore(filepath.Join(t.TempDir(), "state.json"))
	if err := store.SaveManifest("docs", "data.bin", manifest); err != nil {
		t.Fatalf("SaveManifest: %v", err)
	}
	revisions, err := store.ListManifestRevisions("docs")
	if err != nil {
		t.Fatalf("ListManifestRevisions before: %v", err)
	}
	if err := os.WriteFile(path, []byte("evil"), 0o644); err != nil {
		t.Fatalf("corrupt file: %v", err)
	}
	if err := os.Chtimes(path, timeFromUnixNano(manifest.ModTimeUnixNano), timeFromUnixNano(manifest.ModTimeUnixNano)); err != nil {
		t.Fatalf("restore mtime: %v", err)
	}

	_, err = RunOnce(context.Background(), RunOptions{
		Crawler: FileScrubCrawler{
			Store:     store,
			FolderIDs: []string{"docs"},
			Roots:     map[string]string{"docs": root},
		},
		Checkpoint: &memoryCheckpoint{},
		MaxFiles:   10,
	})
	if err != nil {
		t.Fatalf("RunOnce: %v", err)
	}
	after, err := store.ListManifestRevisions("docs")
	if err != nil {
		t.Fatalf("ListManifestRevisions after: %v", err)
	}
	if after["data.bin"] != revisions["data.bin"] {
		t.Fatalf("revision advanced for already damaged manifest: before=%d after=%d", revisions["data.bin"], after["data.bin"])
	}
}

func TestFileScrubCrawlerClassifiesChangedMetadataAsLikelyLocalEdit(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "data.bin")
	if err := os.WriteFile(path, []byte("good"), 0o644); err != nil {
		t.Fatalf("write initial file: %v", err)
	}
	manifest, err := block.BuildManifest(path, 4)
	if err != nil {
		t.Fatalf("build manifest: %v", err)
	}
	manifest.Path = "data.bin"
	if err := os.WriteFile(path, []byte("intentional edit"), 0o644); err != nil {
		t.Fatalf("edit file: %v", err)
	}
	var issues []FileScrubIssue
	crawler := FileScrubCrawler{
		Store:     fakeManifestStore{folders: map[string]map[string]block.Manifest{"docs": {"data.bin": manifest}}},
		FolderIDs: []string{"docs"},
		Roots:     map[string]string{"docs": root},
		Report: func(issue FileScrubIssue) {
			issues = append(issues, issue)
		},
	}

	result, err := RunOnce(context.Background(), RunOptions{
		Crawler:    crawler,
		Checkpoint: &memoryCheckpoint{},
		MaxFiles:   10,
	})
	if err != nil {
		t.Fatalf("RunOnce: %v", err)
	}
	if result.FilesScanned != 1 || result.Reported != 1 || result.Pruned != 0 || result.Quarantined != 0 || !result.Complete {
		t.Fatalf("result=%+v, want one local-change mismatch reported without repair/quarantine", result)
	}
	if len(issues) != 1 || issues[0].Kind != FileScrubHashMismatch || issues[0].Classification != FileScrubLikelyLocalEdit {
		t.Fatalf("issues=%+v, want likely local edit classification", issues)
	}
}

func TestFileScrubCrawlerClassifiesMismatchWithPendingWriteAsNeedsUserReview(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "data.bin")
	if err := os.WriteFile(path, []byte("good"), 0o644); err != nil {
		t.Fatalf("write initial file: %v", err)
	}
	manifest, err := block.BuildManifest(path, 4)
	if err != nil {
		t.Fatalf("build manifest: %v", err)
	}
	manifest.Path = "data.bin"
	if err := os.WriteFile(path, []byte("swap"), 0o644); err != nil {
		t.Fatalf("write pending bytes: %v", err)
	}
	if err := os.Chtimes(path, timeFromUnixNano(manifest.ModTimeUnixNano), timeFromUnixNano(manifest.ModTimeUnixNano)); err != nil {
		t.Fatalf("restore mtime: %v", err)
	}
	var issues []FileScrubIssue
	crawler := FileScrubCrawler{
		Store: fakeFileScrubStore{
			fakeManifestStore: fakeManifestStore{folders: map[string]map[string]block.Manifest{"docs": {"data.bin": manifest}}},
			pendingWrites:     map[string][]state.PendingWrite{"docs": {{FolderID: "docs", Path: "data.bin", Manifest: manifest}}},
		},
		FolderIDs: []string{"docs"},
		Roots:     map[string]string{"docs": root},
		Report: func(issue FileScrubIssue) {
			issues = append(issues, issue)
		},
	}

	result, err := RunOnce(context.Background(), RunOptions{
		Crawler:    crawler,
		Checkpoint: &memoryCheckpoint{},
		MaxFiles:   10,
	})
	if err != nil {
		t.Fatalf("RunOnce: %v", err)
	}
	if result.FilesScanned != 1 || result.Reported != 1 || result.Pruned != 0 || result.Quarantined != 0 || !result.Complete {
		t.Fatalf("result=%+v, want one pending-write mismatch reported without repair/quarantine", result)
	}
	if len(issues) != 1 || issues[0].Kind != FileScrubHashMismatch || issues[0].Classification != FileScrubNeedsUserReview || issues[0].Evidence != "pending-write" {
		t.Fatalf("issues=%+v, want pending-write evidence and needs-user-review classification", issues)
	}
}

func TestFileScrubCrawlerClassifiesMismatchWithRecentReadErrorAsNeedsUserReview(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "data.bin")
	if err := os.WriteFile(path, []byte("good"), 0o644); err != nil {
		t.Fatalf("write initial file: %v", err)
	}
	manifest, err := block.BuildManifest(path, 4)
	if err != nil {
		t.Fatalf("build manifest: %v", err)
	}
	manifest.Path = "data.bin"
	if err := os.WriteFile(path, []byte("evil"), 0o644); err != nil {
		t.Fatalf("change file: %v", err)
	}
	if err := os.Chtimes(path, timeFromUnixNano(manifest.ModTimeUnixNano), timeFromUnixNano(manifest.ModTimeUnixNano)); err != nil {
		t.Fatalf("restore mtime: %v", err)
	}
	var issues []FileScrubIssue
	crawler := FileScrubCrawler{
		Store: fakeFileScrubStore{
			fakeManifestStore: fakeManifestStore{folders: map[string]map[string]block.Manifest{"docs": {"data.bin": manifest}}},
			evidence:          map[string]map[string][]FileScrubEvidence{"docs": {"data.bin": {{Kind: FileScrubEvidenceReadError, Message: "scanner read failed"}}}},
		},
		FolderIDs: []string{"docs"},
		Roots:     map[string]string{"docs": root},
		Report: func(issue FileScrubIssue) {
			issues = append(issues, issue)
		},
	}

	result, err := RunOnce(context.Background(), RunOptions{
		Crawler:    crawler,
		Checkpoint: &memoryCheckpoint{},
		MaxFiles:   10,
	})
	if err != nil {
		t.Fatalf("RunOnce: %v", err)
	}
	if result.FilesScanned != 1 || result.Reported != 1 || result.Pruned != 0 || result.Quarantined != 0 || !result.Complete {
		t.Fatalf("result=%+v, want one read-error-history mismatch reported without repair/quarantine", result)
	}
	if len(issues) != 1 || issues[0].Kind != FileScrubHashMismatch || issues[0].Classification != FileScrubNeedsUserReview || issues[0].Evidence != "read-error-history" {
		t.Fatalf("issues=%+v, want read-error-history evidence and needs-user-review classification", issues)
	}
}

func TestFileScrubCrawlerRequiresRepeatedVerificationBeforeSuspectedCorruption(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "data.bin")
	if err := os.WriteFile(path, []byte("good"), 0o644); err != nil {
		t.Fatalf("write initial file: %v", err)
	}
	manifest, err := block.BuildManifest(path, 4)
	if err != nil {
		t.Fatalf("build manifest: %v", err)
	}
	manifest.Path = "data.bin"
	if err := os.WriteFile(path, []byte("evil"), 0o644); err != nil {
		t.Fatalf("change file: %v", err)
	}
	if err := os.Chtimes(path, timeFromUnixNano(manifest.ModTimeUnixNano), timeFromUnixNano(manifest.ModTimeUnixNano)); err != nil {
		t.Fatalf("restore mtime: %v", err)
	}
	var issues []FileScrubIssue
	crawler := FileScrubCrawler{
		Store: fakeFileScrubStore{
			fakeManifestStore: fakeManifestStore{folders: map[string]map[string]block.Manifest{"docs": {"data.bin": manifest}}},
			retryEvidence:     map[string]map[string]FileScrubRetryEvidence{"docs": {"data.bin": {ConsecutiveMismatches: 1}}},
		},
		FolderIDs: []string{"docs"},
		Roots:     map[string]string{"docs": root},
		Report: func(issue FileScrubIssue) {
			issues = append(issues, issue)
		},
	}

	result, err := RunOnce(context.Background(), RunOptions{
		Crawler:    crawler,
		Checkpoint: &memoryCheckpoint{},
		MaxFiles:   10,
	})
	if err != nil {
		t.Fatalf("RunOnce: %v", err)
	}
	if result.FilesScanned != 1 || result.Reported != 1 || result.Pruned != 0 || result.Quarantined != 0 || !result.Complete {
		t.Fatalf("result=%+v, want mismatch reported without repair/quarantine", result)
	}
	if len(issues) != 1 || issues[0].Kind != FileScrubHashMismatch || issues[0].Classification != FileScrubNeedsUserReview || issues[0].Evidence != "awaiting-repeated-verification" {
		t.Fatalf("issues=%+v, want retry evidence to keep mismatch in needs-user-review", issues)
	}
}

func TestFileScrubCrawlerUsesRepeatedVerificationEvidenceForSuspectedCorruption(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "data.bin")
	if err := os.WriteFile(path, []byte("good"), 0o644); err != nil {
		t.Fatalf("write initial file: %v", err)
	}
	manifest, err := block.BuildManifest(path, 4)
	if err != nil {
		t.Fatalf("build manifest: %v", err)
	}
	manifest.Path = "data.bin"
	if err := os.WriteFile(path, []byte("evil"), 0o644); err != nil {
		t.Fatalf("change file: %v", err)
	}
	if err := os.Chtimes(path, timeFromUnixNano(manifest.ModTimeUnixNano), timeFromUnixNano(manifest.ModTimeUnixNano)); err != nil {
		t.Fatalf("restore mtime: %v", err)
	}
	var issues []FileScrubIssue
	crawler := FileScrubCrawler{
		Store: fakeFileScrubStore{
			fakeManifestStore: fakeManifestStore{folders: map[string]map[string]block.Manifest{"docs": {"data.bin": manifest}}},
			retryEvidence:     map[string]map[string]FileScrubRetryEvidence{"docs": {"data.bin": {ConsecutiveMismatches: 2}}},
		},
		FolderIDs: []string{"docs"},
		Roots:     map[string]string{"docs": root},
		Report: func(issue FileScrubIssue) {
			issues = append(issues, issue)
		},
	}

	result, err := RunOnce(context.Background(), RunOptions{
		Crawler:    crawler,
		Checkpoint: &memoryCheckpoint{},
		MaxFiles:   10,
	})
	if err != nil {
		t.Fatalf("RunOnce: %v", err)
	}
	if result.FilesScanned != 1 || result.Reported != 1 || result.Pruned != 0 || result.Quarantined != 0 || !result.Complete {
		t.Fatalf("result=%+v, want repeated mismatch reported without repair/quarantine", result)
	}
	if len(issues) != 1 || issues[0].Kind != FileScrubHashMismatch || issues[0].Classification != FileScrubSuspectedCorruption || issues[0].Evidence != "repeated-verification" {
		t.Fatalf("issues=%+v, want repeated verification evidence and suspected-corruption classification", issues)
	}
}

func TestFileScrubCrawlerUsesTrustedPeerConsensusForSuspectedCorruption(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "data.bin")
	if err := os.WriteFile(path, []byte("good"), 0o644); err != nil {
		t.Fatalf("write initial file: %v", err)
	}
	manifest, err := block.BuildManifest(path, 4)
	if err != nil {
		t.Fatalf("build manifest: %v", err)
	}
	manifest.Path = "data.bin"
	if err := os.WriteFile(path, []byte("evil"), 0o644); err != nil {
		t.Fatalf("change file: %v", err)
	}
	if err := os.Chtimes(path, timeFromUnixNano(manifest.ModTimeUnixNano), timeFromUnixNano(manifest.ModTimeUnixNano)); err != nil {
		t.Fatalf("restore mtime: %v", err)
	}
	var issues []FileScrubIssue
	crawler := FileScrubCrawler{
		Store: fakeFileScrubStore{
			fakeManifestStore: fakeManifestStore{folders: map[string]map[string]block.Manifest{"docs": {"data.bin": manifest}}},
			retryEvidence:     map[string]map[string]FileScrubRetryEvidence{"docs": {"data.bin": {ConsecutiveMismatches: 1}}},
			consensus:         map[string]map[string]FileScrubConsensusEvidence{"docs": {"data.bin": {TrustedExpectedMatches: 2}}},
		},
		FolderIDs: []string{"docs"},
		Roots:     map[string]string{"docs": root},
		Report: func(issue FileScrubIssue) {
			issues = append(issues, issue)
		},
	}

	result, err := RunOnce(context.Background(), RunOptions{
		Crawler:    crawler,
		Checkpoint: &memoryCheckpoint{},
		MaxFiles:   10,
	})
	if err != nil {
		t.Fatalf("RunOnce: %v", err)
	}
	if result.FilesScanned != 1 || result.Reported != 1 || result.Pruned != 0 || result.Quarantined != 0 || !result.Complete {
		t.Fatalf("result=%+v, want peer-consensus mismatch reported without repair/quarantine", result)
	}
	if len(issues) != 1 || issues[0].Kind != FileScrubHashMismatch || issues[0].Classification != FileScrubSuspectedCorruption || issues[0].Evidence != "trusted-peer-consensus" {
		t.Fatalf("issues=%+v, want trusted-peer-consensus evidence and suspected-corruption classification", issues)
	}
}

func TestFileScrubCrawlerUsesTrustedPeerConsensusForLocalDivergence(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "data.bin")
	if err := os.WriteFile(path, []byte("old"), 0o644); err != nil {
		t.Fatalf("write initial file: %v", err)
	}
	manifest, err := block.BuildManifest(path, 4)
	if err != nil {
		t.Fatalf("build manifest: %v", err)
	}
	manifest.Path = "data.bin"
	if err := os.WriteFile(path, []byte("new"), 0o644); err != nil {
		t.Fatalf("change file: %v", err)
	}
	if err := os.Chtimes(path, timeFromUnixNano(manifest.ModTimeUnixNano), timeFromUnixNano(manifest.ModTimeUnixNano)); err != nil {
		t.Fatalf("restore mtime: %v", err)
	}
	var issues []FileScrubIssue
	crawler := FileScrubCrawler{
		Store: fakeFileScrubStore{
			fakeManifestStore: fakeManifestStore{folders: map[string]map[string]block.Manifest{"docs": {"data.bin": manifest}}},
			retryEvidence:     map[string]map[string]FileScrubRetryEvidence{"docs": {"data.bin": {ConsecutiveMismatches: 2}}},
			consensus:         map[string]map[string]FileScrubConsensusEvidence{"docs": {"data.bin": {TrustedActualMatches: 2}}},
		},
		FolderIDs: []string{"docs"},
		Roots:     map[string]string{"docs": root},
		Report: func(issue FileScrubIssue) {
			issues = append(issues, issue)
		},
	}

	result, err := RunOnce(context.Background(), RunOptions{
		Crawler:    crawler,
		Checkpoint: &memoryCheckpoint{},
		MaxFiles:   10,
	})
	if err != nil {
		t.Fatalf("RunOnce: %v", err)
	}
	if result.FilesScanned != 1 || result.Reported != 1 || result.Pruned != 0 || result.Quarantined != 0 || !result.Complete {
		t.Fatalf("result=%+v, want peer-actual-consensus mismatch reported without repair/quarantine", result)
	}
	if len(issues) != 1 || issues[0].Kind != FileScrubHashMismatch || issues[0].Classification != FileScrubNeedsUserReview || issues[0].Evidence != "trusted-peer-actual-consensus" {
		t.Fatalf("issues=%+v, want trusted-peer-actual-consensus evidence and needs-user-review classification", issues)
	}
}

func TestFileScrubCrawlerClassifiesAmbiguousMismatchForUserReview(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "data.bin")
	if err := os.WriteFile(path, []byte("good"), 0o644); err != nil {
		t.Fatalf("write initial file: %v", err)
	}
	manifest, err := block.BuildManifest(path, 4)
	if err != nil {
		t.Fatalf("build manifest: %v", err)
	}
	manifest.Path = "data.bin"
	manifest.ModTimeUnixNano = 0
	if err := os.WriteFile(path, []byte("evil"), 0o644); err != nil {
		t.Fatalf("change file: %v", err)
	}
	var issues []FileScrubIssue
	crawler := FileScrubCrawler{
		Store:     fakeManifestStore{folders: map[string]map[string]block.Manifest{"docs": {"data.bin": manifest}}},
		FolderIDs: []string{"docs"},
		Roots:     map[string]string{"docs": root},
		Report: func(issue FileScrubIssue) {
			issues = append(issues, issue)
		},
	}

	result, err := RunOnce(context.Background(), RunOptions{
		Crawler:    crawler,
		Checkpoint: &memoryCheckpoint{},
		MaxFiles:   10,
	})
	if err != nil {
		t.Fatalf("RunOnce: %v", err)
	}
	if result.FilesScanned != 1 || result.Reported != 1 || result.Pruned != 0 || result.Quarantined != 0 || !result.Complete {
		t.Fatalf("result=%+v, want ambiguous mismatch reported without repair/quarantine", result)
	}
	if len(issues) != 1 || issues[0].Kind != FileScrubHashMismatch || issues[0].Classification != FileScrubNeedsUserReview {
		t.Fatalf("issues=%+v, want needs-user-review classification", issues)
	}
}

func TestFileScrubCrawlerReportsMissingFileWithoutPruningManifest(t *testing.T) {
	root := t.TempDir()
	manifest := block.Manifest{Path: "missing.bin", Size: 4, BlockSize: 4, HashState: "complete", Blocks: []block.Block{{Index: 0, Offset: 0, Size: 4, Hash: []byte{1, 2, 3}}}}
	store := fakeManifestStore{folders: map[string]map[string]block.Manifest{"docs": {"missing.bin": manifest}}}
	crawler := FileScrubCrawler{
		Store:     store,
		FolderIDs: []string{"docs"},
		Roots:     map[string]string{"docs": root},
	}

	result, err := RunOnce(context.Background(), RunOptions{
		Crawler:    crawler,
		Checkpoint: &memoryCheckpoint{},
		MaxFiles:   10,
	})
	if err != nil {
		t.Fatalf("RunOnce: %v", err)
	}
	if result.FilesScanned != 1 || result.Reported != 1 || result.Pruned != 0 || !result.Complete {
		t.Fatalf("result=%+v, want missing file reported without manifest pruning", result)
	}
	if _, ok := store.folders["docs"]["missing.bin"]; !ok {
		t.Fatalf("scrub crawler pruned missing file manifest; want report-only behavior")
	}
}

func TestFileScrubCrawlerSkipsUnverifiedManifestsWithoutBlockHashes(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "seed.bin"), []byte("seed"), 0o644); err != nil {
		t.Fatalf("write seed: %v", err)
	}
	manifest := block.Manifest{Path: "seed.bin", Size: 4, HashState: "unknown"}
	crawler := FileScrubCrawler{
		Store:     fakeManifestStore{folders: map[string]map[string]block.Manifest{"docs": {"seed.bin": manifest}}},
		FolderIDs: []string{"docs"},
		Roots:     map[string]string{"docs": root},
	}

	result, err := RunOnce(context.Background(), RunOptions{
		Crawler:    crawler,
		Checkpoint: &memoryCheckpoint{},
		MaxFiles:   10,
	})
	if err != nil {
		t.Fatalf("RunOnce: %v", err)
	}
	if result.FilesScanned != 1 || result.Reported != 0 || !result.Complete {
		t.Fatalf("result=%+v, want unverified manifest skipped without false corruption report", result)
	}
}

func TestFileScrubCrawlerLightMetadataModeReportsCheapMetadataMismatchWithoutHashing(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "data.bin")
	if err := os.WriteFile(path, []byte("good"), 0o644); err != nil {
		t.Fatalf("write initial file: %v", err)
	}
	manifest, err := block.BuildManifest(path, 4)
	if err != nil {
		t.Fatalf("build manifest: %v", err)
	}
	manifest.Path = "data.bin"
	if err := os.WriteFile(path, []byte("changed-size"), 0o644); err != nil {
		t.Fatalf("change file: %v", err)
	}
	var issues []FileScrubIssue
	crawler := FileScrubCrawler{
		Store:      fakeManifestStore{folders: map[string]map[string]block.Manifest{"docs": {"data.bin": manifest}}},
		FolderIDs:  []string{"docs"},
		Roots:      map[string]string{"docs": root},
		VerifyMode: FileScrubLightMetadata,
		Report: func(issue FileScrubIssue) {
			issues = append(issues, issue)
		},
	}

	result, err := RunOnce(context.Background(), RunOptions{
		Crawler:    crawler,
		Checkpoint: &memoryCheckpoint{},
		MaxFiles:   10,
	})
	if err != nil {
		t.Fatalf("RunOnce: %v", err)
	}
	if result.FilesScanned != 1 || result.Reported != 1 || !result.Complete {
		t.Fatalf("result=%+v, want one light metadata mismatch reported", result)
	}
	if len(issues) != 1 || issues[0].Kind != FileScrubMetadataMismatch || issues[0].Classification != FileScrubLikelyLocalEdit || issues[0].ExpectedSize != int64(len("good")) || issues[0].ActualSize != int64(len("changed-size")) {
		t.Fatalf("issues=%+v, want cheap metadata mismatch without block hash comparison", issues)
	}
}

func TestFileScrubCrawlerSampledBlockModeVerifiesDeterministicBlockSubset(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "data.bin")
	if err := os.WriteFile(path, []byte("aaaabbbbcccc"), 0o644); err != nil {
		t.Fatalf("write initial file: %v", err)
	}
	manifest, err := block.BuildManifest(path, 4)
	if err != nil {
		t.Fatalf("build manifest: %v", err)
	}
	manifest.Path = "data.bin"
	if err := os.WriteFile(path, []byte("aaaabbbbZZZZ"), 0o644); err != nil {
		t.Fatalf("change sampled block: %v", err)
	}
	if err := os.Chtimes(path, timeFromUnixNano(manifest.ModTimeUnixNano), timeFromUnixNano(manifest.ModTimeUnixNano)); err != nil {
		t.Fatalf("restore mtime: %v", err)
	}
	var issues []FileScrubIssue
	crawler := FileScrubCrawler{
		Store:              fakeManifestStore{folders: map[string]map[string]block.Manifest{"docs": {"data.bin": manifest}}},
		FolderIDs:          []string{"docs"},
		Roots:              map[string]string{"docs": root},
		VerifyMode:         FileScrubSampledBlocks,
		SampleEveryNBlocks: 2,
		Report: func(issue FileScrubIssue) {
			issues = append(issues, issue)
		},
	}

	result, err := RunOnce(context.Background(), RunOptions{
		Crawler:    crawler,
		Checkpoint: &memoryCheckpoint{},
		MaxFiles:   10,
	})
	if err != nil {
		t.Fatalf("RunOnce: %v", err)
	}
	if result.FilesScanned != 1 || result.Reported != 1 || !result.Complete {
		t.Fatalf("result=%+v, want one sampled block mismatch reported", result)
	}
	if len(issues) != 1 || issues[0].Kind != FileScrubHashMismatch || issues[0].Classification != FileScrubSuspectedCorruption || issues[0].Evidence != "sampled-block-verification" {
		t.Fatalf("issues=%+v, want sampled block mismatch evidence", issues)
	}
}

func mustReadFile(t *testing.T, path string) []byte {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return data
}

func timeFromUnixNano(nano int64) time.Time {
	return time.Unix(0, nano)
}

type fakeFileScrubStore struct {
	fakeManifestStore
	pendingWrites map[string][]state.PendingWrite
	evidence      map[string]map[string][]FileScrubEvidence
	retryEvidence map[string]map[string]FileScrubRetryEvidence
	consensus     map[string]map[string]FileScrubConsensusEvidence
}

func (f fakeFileScrubStore) PendingWrites(folderID string) ([]state.PendingWrite, error) {
	return append([]state.PendingWrite(nil), f.pendingWrites[folderID]...), nil
}

func (f fakeFileScrubStore) FileScrubEvidence(folderID string, path string) ([]FileScrubEvidence, error) {
	return append([]FileScrubEvidence(nil), f.evidence[folderID][path]...), nil
}

func (f fakeFileScrubStore) FileScrubRetryEvidence(folderID string, path string) (FileScrubRetryEvidence, error) {
	return f.retryEvidence[folderID][path], nil
}

func (f fakeFileScrubStore) FileScrubConsensusEvidence(folderID string, path string, expected block.Manifest, actual block.Manifest) (FileScrubConsensusEvidence, error) {
	return f.consensus[folderID][path], nil
}

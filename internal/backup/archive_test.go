package backup

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"testing"
	"time"

	"filesyncengine/internal/block"
	"filesyncengine/internal/state"
)

func TestProcessArchiveIntakeJobsArchivesVerifiedBlocksContentAddressed(t *testing.T) {
	root := t.TempDir()
	sourceRoot := filepath.Join(root, "source")
	archiveRoot := filepath.Join(root, "archive")
	if err := os.MkdirAll(filepath.Join(sourceRoot, "docs"), 0o755); err != nil {
		t.Fatalf("mkdir source: %v", err)
	}
	data := []byte("alpha-beta")
	if err := os.WriteFile(filepath.Join(sourceRoot, "docs", "report.txt"), data, 0o644); err != nil {
		t.Fatalf("write source: %v", err)
	}
	alphaHash := sha256.Sum256([]byte("alpha"))
	betaHash := sha256.Sum256([]byte("-beta"))
	store := state.NewJSONStore(filepath.Join(root, "state.json"))
	jobs := []state.ArchiveIntakeJob{
		{
			ID:         "job-alpha",
			SnapshotID: "snap-1",
			FolderID:   "docs",
			Path:       "docs/report.txt",
			Block:      block.Block{Index: 0, Offset: 0, Size: 5, Hash: alphaHash[:]},
			Status:     "pending",
			CreatedAt:  time.Now().UTC().Format(time.RFC3339),
		},
		{
			ID:         "job-beta",
			SnapshotID: "snap-1",
			FolderID:   "docs",
			Path:       "docs/report.txt",
			Block:      block.Block{Index: 1, Offset: 5, Size: 5, Hash: betaHash[:]},
			Status:     "pending",
			CreatedAt:  time.Now().UTC().Format(time.RFC3339),
		},
	}
	if err := store.SaveArchiveIntakeJobs("snap-1", jobs); err != nil {
		t.Fatalf("save jobs: %v", err)
	}

	result, err := ProcessArchiveIntakeJobs(sourceRoot, archiveRoot, store, "snap-1")
	if err != nil {
		t.Fatalf("process archive intake jobs: %v", err)
	}
	if result.Archived != 2 || result.Reused != 0 || result.Failed != 0 {
		t.Fatalf("unexpected archive result: %+v", result)
	}

	for _, tc := range []struct {
		name string
		hash [32]byte
		want []byte
	}{
		{name: "alpha", hash: alphaHash, want: []byte("alpha")},
		{name: "beta", hash: betaHash, want: []byte("-beta")},
	} {
		hexHash := hex.EncodeToString(tc.hash[:])
		archived, err := os.ReadFile(filepath.Join(archiveRoot, "blocks", hexHash[:2], hexHash))
		if err != nil {
			t.Fatalf("read archived %s block: %v", tc.name, err)
		}
		if !bytes.Equal(archived, tc.want) {
			t.Fatalf("archived %s block mismatch: got %q want %q", tc.name, archived, tc.want)
		}
	}
	updated, err := store.ListArchiveIntakeJobs("snap-1")
	if err != nil {
		t.Fatalf("list jobs: %v", err)
	}
	for _, job := range updated {
		if job.Status != "archived" {
			t.Fatalf("job %s status = %q, want archived", job.ID, job.Status)
		}
	}
}

func TestProcessArchiveIntakeJobsUsesRetainedBackupIntakeWhenLiveFileChanged(t *testing.T) {
	root := t.TempDir()
	sourceRoot := filepath.Join(root, "source")
	archiveRoot := filepath.Join(root, "archive")
	if err := os.MkdirAll(sourceRoot, 0o755); err != nil {
		t.Fatalf("mkdir source: %v", err)
	}
	if err := os.WriteFile(filepath.Join(sourceRoot, "report.txt"), []byte("new bytes"), 0o644); err != nil {
		t.Fatalf("write changed live source: %v", err)
	}
	retainedAt := time.Date(2026, 5, 24, 15, 30, 0, 0, time.UTC)
	retainedPath, err := RetainBackupIntakeFile(sourceRoot, "report.txt", []byte("old bytes"), retainedAt)
	if err != nil {
		t.Fatalf("retain backup intake file: %v", err)
	}
	if retainedPath == filepath.Join(sourceRoot, "report.txt") {
		t.Fatalf("retained backup copy must not overwrite live file")
	}
	oldHash := sha256.Sum256([]byte("old bytes"))
	store := state.NewJSONStore(filepath.Join(root, "state.json"))
	jobs := []state.ArchiveIntakeJob{{
		ID:         "job-old",
		SnapshotID: "snap-1",
		FolderID:   "docs",
		Path:       "report.txt",
		Block:      block.Block{Index: 0, Offset: 0, Size: 9, Hash: oldHash[:]},
		Status:     "pending",
		CreatedAt:  time.Now().UTC().Format(time.RFC3339),
	}}
	if err := store.SaveArchiveIntakeJobs("snap-1", jobs); err != nil {
		t.Fatalf("save jobs: %v", err)
	}

	result, err := ProcessArchiveIntakeJobs(sourceRoot, archiveRoot, store, "snap-1")
	if err != nil {
		t.Fatalf("process archive intake jobs from retained copy: %v", err)
	}
	if result.Archived != 1 || result.Failed != 0 {
		t.Fatalf("unexpected archive result: %+v", result)
	}
	hexHash := hex.EncodeToString(oldHash[:])
	archived, err := os.ReadFile(filepath.Join(archiveRoot, "blocks", hexHash[:2], hexHash))
	if err != nil {
		t.Fatalf("read archived retained block: %v", err)
	}
	if !bytes.Equal(archived, []byte("old bytes")) {
		t.Fatalf("archived retained block = %q, want old bytes", archived)
	}
}

func TestProcessArchiveIntakeJobsRejectsHashMismatchWithoutArchiveArtifact(t *testing.T) {
	root := t.TempDir()
	sourceRoot := filepath.Join(root, "source")
	archiveRoot := filepath.Join(root, "archive")
	if err := os.MkdirAll(sourceRoot, 0o755); err != nil {
		t.Fatalf("mkdir source: %v", err)
	}
	if err := os.WriteFile(filepath.Join(sourceRoot, "file.txt"), []byte("actual"), 0o644); err != nil {
		t.Fatalf("write source: %v", err)
	}
	wrongHash := sha256.Sum256([]byte("expected"))
	store := state.NewJSONStore(filepath.Join(root, "state.json"))
	jobs := []state.ArchiveIntakeJob{{
		ID:         "job-bad",
		SnapshotID: "snap-1",
		FolderID:   "docs",
		Path:       "file.txt",
		Block:      block.Block{Index: 0, Offset: 0, Size: 6, Hash: wrongHash[:]},
		Status:     "pending",
		CreatedAt:  time.Now().UTC().Format(time.RFC3339),
	}}
	if err := store.SaveArchiveIntakeJobs("snap-1", jobs); err != nil {
		t.Fatalf("save jobs: %v", err)
	}

	result, err := ProcessArchiveIntakeJobs(sourceRoot, archiveRoot, store, "snap-1")
	if err == nil {
		t.Fatalf("expected hash mismatch error")
	}
	if result.Archived != 0 || result.Failed != 1 {
		t.Fatalf("unexpected failed archive result: %+v", result)
	}
	hexHash := hex.EncodeToString(wrongHash[:])
	if _, statErr := os.Stat(filepath.Join(archiveRoot, "blocks", hexHash[:2], hexHash)); !os.IsNotExist(statErr) {
		t.Fatalf("hash mismatch should not leave archive artifact, stat err=%v", statErr)
	}
}

func TestRunArchiveIntakeOnceResumesPendingJobsWithBudgetAndRetryDelay(t *testing.T) {
	root := t.TempDir()
	sourceRoot := filepath.Join(root, "source")
	archiveRoot := filepath.Join(root, "archive")
	if err := os.MkdirAll(sourceRoot, 0o755); err != nil {
		t.Fatalf("mkdir source: %v", err)
	}
	if err := os.WriteFile(filepath.Join(sourceRoot, "one.txt"), []byte("first"), 0o644); err != nil {
		t.Fatalf("write source one: %v", err)
	}
	if err := os.WriteFile(filepath.Join(sourceRoot, "two.txt"), []byte("second"), 0o644); err != nil {
		t.Fatalf("write source two: %v", err)
	}
	firstHash := sha256.Sum256([]byte("first"))
	secondHash := sha256.Sum256([]byte("second"))
	now := time.Date(2026, 5, 24, 12, 0, 0, 0, time.UTC)
	store := state.NewJSONStore(filepath.Join(root, "state.json"))
	jobs := []state.ArchiveIntakeJob{
		{ID: "job-1", SnapshotID: "snap-1", FolderID: "docs", Path: "one.txt", Block: block.Block{Index: 0, Offset: 0, Size: 5, Hash: firstHash[:]}, Status: "pending", CreatedAt: now.Format(time.RFC3339)},
		{ID: "job-2", SnapshotID: "snap-1", FolderID: "docs", Path: "two.txt", Block: block.Block{Index: 0, Offset: 0, Size: 6, Hash: secondHash[:]}, Status: "pending", CreatedAt: now.Format(time.RFC3339)},
	}
	if err := store.SaveArchiveIntakeJobs("snap-1", jobs); err != nil {
		t.Fatalf("save jobs: %v", err)
	}

	first, err := RunArchiveIntakeOnce(ArchiveIntakeWorkerOptions{
		ArchiveRoot: archiveRoot,
		SourceRoots: map[string]string{"docs": sourceRoot},
		Store:       store,
		Now:         now,
		MaxJobs:     1,
		RetryDelay:  time.Hour,
	})
	if err != nil {
		t.Fatalf("first run archive intake worker: %v", err)
	}
	if first.Processed != 1 || first.Archived != 1 || first.Remaining != 1 {
		t.Fatalf("unexpected first worker result: %+v", first)
	}

	reopened := state.NewJSONStore(filepath.Join(root, "state.json"))
	second, err := RunArchiveIntakeOnce(ArchiveIntakeWorkerOptions{
		ArchiveRoot: archiveRoot,
		SourceRoots: map[string]string{"docs": sourceRoot},
		Store:       reopened,
		Now:         now.Add(time.Minute),
		MaxJobs:     10,
		RetryDelay:  time.Hour,
	})
	if err != nil {
		t.Fatalf("second run archive intake worker: %v", err)
	}
	if second.Processed != 1 || second.Archived != 1 || second.Remaining != 0 {
		t.Fatalf("unexpected second worker result: %+v", second)
	}

	badStore := state.NewJSONStore(filepath.Join(root, "bad-state.json"))
	badJobs := []state.ArchiveIntakeJob{{ID: "bad", SnapshotID: "snap-bad", FolderID: "docs", Path: "missing.txt", Block: block.Block{Index: 0, Offset: 0, Size: 7, Hash: firstHash[:]}, Status: "pending", CreatedAt: now.Format(time.RFC3339)}}
	if err := badStore.SaveArchiveIntakeJobs("snap-bad", badJobs); err != nil {
		t.Fatalf("save bad jobs: %v", err)
	}
	failed, err := RunArchiveIntakeOnce(ArchiveIntakeWorkerOptions{ArchiveRoot: archiveRoot, SourceRoots: map[string]string{"docs": sourceRoot}, Store: badStore, Now: now, MaxJobs: 10, RetryDelay: time.Hour})
	if err == nil {
		t.Fatalf("expected missing source to fail")
	}
	if failed.Failed != 1 || failed.Remaining != 1 {
		t.Fatalf("unexpected failed worker result: %+v", failed)
	}
	retryBlocked, err := RunArchiveIntakeOnce(ArchiveIntakeWorkerOptions{ArchiveRoot: archiveRoot, SourceRoots: map[string]string{"docs": sourceRoot}, Store: badStore, Now: now.Add(time.Minute), MaxJobs: 10, RetryDelay: time.Hour})
	if err != nil {
		t.Fatalf("retry-blocked worker run should skip failed job, got: %v", err)
	}
	if retryBlocked.Processed != 0 || retryBlocked.Remaining != 1 {
		t.Fatalf("retry delay should preserve failed job without processing: %+v", retryBlocked)
	}
}

func TestArchiveProtectionStatusSeparatesVerifiedBlocksFromJobState(t *testing.T) {
	root := t.TempDir()
	sourceRoot := filepath.Join(root, "source")
	archiveRoot := filepath.Join(root, "archive")
	if err := os.MkdirAll(sourceRoot, 0o755); err != nil {
		t.Fatalf("mkdir source: %v", err)
	}
	if err := os.WriteFile(filepath.Join(sourceRoot, "file.txt"), []byte("alpha-beta"), 0o644); err != nil {
		t.Fatalf("write source: %v", err)
	}
	alphaHash := sha256.Sum256([]byte("alpha"))
	betaHash := sha256.Sum256([]byte("-beta"))
	missingHash := sha256.Sum256([]byte("missing"))
	failedHash := sha256.Sum256([]byte("failed"))
	store := state.NewJSONStore(filepath.Join(root, "state.json"))
	jobs := []state.ArchiveIntakeJob{
		{ID: "archived-present", SnapshotID: "snap-1", FolderID: "docs", Path: "file.txt", Block: block.Block{Index: 0, Offset: 0, Size: 5, Hash: alphaHash[:]}, Status: ArchiveJobStatusArchived, CreatedAt: time.Now().UTC().Format(time.RFC3339)},
		{ID: "archived-missing", SnapshotID: "snap-1", FolderID: "docs", Path: "file.txt", Block: block.Block{Index: 1, Offset: 5, Size: 5, Hash: betaHash[:]}, Status: ArchiveJobStatusArchived, CreatedAt: time.Now().UTC().Format(time.RFC3339)},
		{ID: "pending", SnapshotID: "snap-1", FolderID: "docs", Path: "missing.txt", Block: block.Block{Index: 0, Offset: 0, Size: 7, Hash: missingHash[:]}, Status: ArchiveJobStatusPending, CreatedAt: time.Now().UTC().Format(time.RFC3339)},
		{ID: "failed", SnapshotID: "snap-2", FolderID: "docs", Path: "failed.txt", Block: block.Block{Index: 0, Offset: 0, Size: 6, Hash: failedHash[:]}, Status: ArchiveJobStatusFailed, CreatedAt: time.Now().UTC().Format(time.RFC3339)},
	}
	if err := store.SaveArchiveIntakeJobs("snap-1", jobs[:3]); err != nil {
		t.Fatalf("save snap-1 jobs: %v", err)
	}
	if err := store.SaveArchiveIntakeJobs("snap-2", jobs[3:]); err != nil {
		t.Fatalf("save snap-2 jobs: %v", err)
	}
	if _, err := archiveOneBlock(sourceRoot, archiveRoot, "file.txt", jobs[0].Block); err != nil {
		t.Fatalf("seed archive block: %v", err)
	}

	status, err := ComputeArchiveProtectionStatus(ArchiveProtectionStatusOptions{ArchiveRoot: archiveRoot, Store: store})
	if err != nil {
		t.Fatalf("compute protection status: %v", err)
	}
	if status.TotalBlocks != 4 || status.ProtectedBlocks != 1 || status.PendingBlocks != 1 || status.FailedBlocks != 1 || status.MissingArchiveBlocks != 1 {
		t.Fatalf("unexpected status: %+v", status)
	}
	if status.Snapshots["snap-1"].ProtectedBlocks != 1 || status.Snapshots["snap-1"].MissingArchiveBlocks != 1 || status.Snapshots["snap-1"].PendingBlocks != 1 {
		t.Fatalf("snap-1 status should keep archive verification distinct from job state: %+v", status.Snapshots["snap-1"])
	}
	if status.Snapshots["snap-2"].FailedBlocks != 1 {
		t.Fatalf("snap-2 failed status missing: %+v", status.Snapshots["snap-2"])
	}
}

func TestScrubBackupArchiveReportsDegradedJobsAndOrphanBlocks(t *testing.T) {
	root := t.TempDir()
	archiveRoot := filepath.Join(root, "archive")
	store := state.NewJSONStore(filepath.Join(root, "state.json"))
	createdAt := time.Date(2026, 5, 25, 12, 0, 0, 0, time.UTC).Format(time.RFC3339)
	goodHash := sha256.Sum256([]byte("good"))
	corruptHash := sha256.Sum256([]byte("expected"))
	missingHash := sha256.Sum256([]byte("missing"))
	pendingHash := sha256.Sum256([]byte("pending"))
	orphanHash := sha256.Sum256([]byte("orphan"))
	jobs := []state.ArchiveIntakeJob{
		{ID: "good", SnapshotID: "snap", FolderID: "docs", Path: "good.txt", Block: block.Block{Index: 0, Offset: 0, Size: 4, Hash: goodHash[:]}, Status: ArchiveJobStatusArchived, CreatedAt: createdAt},
		{ID: "corrupt", SnapshotID: "snap", FolderID: "docs", Path: "corrupt.txt", Block: block.Block{Index: 0, Offset: 0, Size: 8, Hash: corruptHash[:]}, Status: ArchiveJobStatusArchived, CreatedAt: createdAt},
		{ID: "missing", SnapshotID: "snap", FolderID: "docs", Path: "missing.txt", Block: block.Block{Index: 0, Offset: 0, Size: 7, Hash: missingHash[:]}, Status: ArchiveJobStatusArchived, CreatedAt: createdAt},
		{ID: "pending", SnapshotID: "snap", FolderID: "docs", Path: "pending.txt", Block: block.Block{Index: 0, Offset: 0, Size: 7, Hash: pendingHash[:]}, Status: ArchiveJobStatusPending, CreatedAt: createdAt},
	}
	if err := store.SaveArchiveIntakeJobs("snap", jobs); err != nil {
		t.Fatalf("save jobs: %v", err)
	}
	if err := writeArchiveBlockAtomic(mustArchiveBlockPath(t, archiveRoot, jobs[0].Block), []byte("good")); err != nil {
		t.Fatalf("seed good archive block: %v", err)
	}
	if err := writeArchiveBlockAtomic(mustArchiveBlockPath(t, archiveRoot, jobs[1].Block), []byte("wrong")); err != nil {
		t.Fatalf("seed corrupt archive block: %v", err)
	}
	orphanPath := filepath.Join(archiveRoot, "blocks", hex.EncodeToString(orphanHash[:])[:2], hex.EncodeToString(orphanHash[:]))
	if err := writeArchiveBlockAtomic(orphanPath, []byte("orphan")); err != nil {
		t.Fatalf("seed orphan archive block: %v", err)
	}

	result, err := ScrubBackupArchive(BackupArchiveScrubOptions{ArchiveRoot: archiveRoot, Store: store})
	if err != nil {
		t.Fatalf("scrub backup archive: %v", err)
	}
	if result.CheckedJobs != 4 || result.ProtectedBlocks != 1 || result.MissingBlocks != 1 || result.CorruptBlocks != 1 || result.IncompleteJobs != 1 || result.OrphanBlocks != 1 {
		t.Fatalf("unexpected scrub summary: %+v", result)
	}
	wantKinds := map[string]bool{"archive_block_corrupt": false, "archive_block_missing": false, "archive_intake_incomplete": false, "archive_block_orphaned": false}
	for _, issue := range result.Issues {
		if _, ok := wantKinds[issue.Kind]; ok {
			wantKinds[issue.Kind] = true
		}
	}
	for kind, seen := range wantKinds {
		if !seen {
			t.Fatalf("missing scrub issue kind %s in %+v", kind, result.Issues)
		}
	}
}

func TestScrubBackupCheckpointsReportsMissingCorruptAndDegradedSnapshots(t *testing.T) {
	root := t.TempDir()
	archiveRoot := filepath.Join(root, "archive")
	checkpointRoot := filepath.Join(root, "checkpoints")
	store := state.NewJSONStore(filepath.Join(root, "state.json"))
	createdAt := time.Date(2026, 5, 25, 12, 0, 0, 0, time.UTC).Format(time.RFC3339)
	markers := []state.SnapshotMarker{
		{ID: "snap-good", FolderID: "docs", Cursor: 1, StateHash: "hash-good", CreatedAt: createdAt},
		{ID: "snap-missing-checkpoint", FolderID: "docs", Cursor: 2, StateHash: "hash-missing", CreatedAt: createdAt},
		{ID: "snap-corrupt-checkpoint", FolderID: "docs", Cursor: 3, StateHash: "hash-corrupt", CreatedAt: createdAt},
		{ID: "snap-missing-archive", FolderID: "docs", Cursor: 4, StateHash: "hash-archive", CreatedAt: createdAt},
	}
	for _, marker := range markers {
		if err := store.SaveSnapshotMarker(marker); err != nil {
			t.Fatalf("save marker %s: %v", marker.ID, err)
		}
	}
	goodHash := sha256.Sum256([]byte("good"))
	missingHash := sha256.Sum256([]byte("missing"))
	jobs := []state.ArchiveIntakeJob{
		{ID: "good", SnapshotID: "snap-good", FolderID: "docs", Path: "good.txt", Block: block.Block{Index: 0, Offset: 0, Size: 4, Hash: goodHash[:]}, Status: ArchiveJobStatusArchived, CreatedAt: createdAt},
		{ID: "missing", SnapshotID: "snap-missing-archive", FolderID: "docs", Path: "missing.txt", Block: block.Block{Index: 0, Offset: 0, Size: 7, Hash: missingHash[:]}, Status: ArchiveJobStatusArchived, CreatedAt: createdAt},
	}
	if err := store.SaveArchiveIntakeJobs("snap-good", jobs[:1]); err != nil {
		t.Fatalf("save good job: %v", err)
	}
	if err := store.SaveArchiveIntakeJobs("snap-missing-archive", jobs[1:]); err != nil {
		t.Fatalf("save missing archive job: %v", err)
	}
	if err := writeArchiveBlockAtomic(mustArchiveBlockPath(t, archiveRoot, jobs[0].Block), []byte("good")); err != nil {
		t.Fatalf("seed good archive block: %v", err)
	}
	for _, snapshotID := range []string{"snap-good", "snap-corrupt-checkpoint", "snap-missing-archive"} {
		path := filepath.Join(checkpointRoot, "docs", snapshotID+".json")
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatalf("mkdir checkpoint dir: %v", err)
		}
		payload := []byte(`{"snapshot":"` + snapshotID + `"}`)
		if snapshotID == "snap-corrupt-checkpoint" {
			payload = []byte(`{"snapshot":`)
		}
		if err := os.WriteFile(path, payload, 0o644); err != nil {
			t.Fatalf("write checkpoint %s: %v", snapshotID, err)
		}
	}

	result, err := ScrubBackupCheckpoints(BackupCheckpointScrubOptions{ArchiveRoot: archiveRoot, CheckpointRoot: checkpointRoot, Store: store})
	if err != nil {
		t.Fatalf("scrub backup checkpoints: %v", err)
	}
	if result.CheckedSnapshots != 4 || result.AvailableCheckpoints != 2 || result.MissingCheckpoints != 1 || result.CorruptCheckpoints != 1 || result.DegradedSnapshots != 3 {
		t.Fatalf("unexpected checkpoint scrub summary: %+v", result)
	}
	wantKinds := map[string]bool{"checkpoint_missing": false, "checkpoint_corrupt": false, "snapshot_degraded": false}
	for _, issue := range result.Issues {
		if _, ok := wantKinds[issue.Kind]; ok {
			wantKinds[issue.Kind] = true
		}
	}
	for kind, seen := range wantKinds {
		if !seen {
			t.Fatalf("missing checkpoint scrub issue kind %s in %+v", kind, result.Issues)
		}
	}
}

func TestExecuteBackupCheckpointRepairRestoresVerifiedPeerCheckpoint(t *testing.T) {
	root := t.TempDir()
	checkpointRoot := filepath.Join(root, "checkpoints")
	peerCheckpointRoot := filepath.Join(root, "peer-checkpoints")
	store := state.NewJSONStore(filepath.Join(root, "state.json"))
	createdAt := time.Date(2026, 5, 25, 12, 0, 0, 0, time.UTC).Format(time.RFC3339)
	marker := state.SnapshotMarker{ID: "snap", FolderID: "docs", Cursor: 7, StateHash: "hash", CreatedAt: createdAt}
	if err := store.SaveSnapshotMarker(marker); err != nil {
		t.Fatalf("save marker: %v", err)
	}
	peerPath := filepath.Join(peerCheckpointRoot, "docs", "snap.json")
	if err := os.MkdirAll(filepath.Dir(peerPath), 0o755); err != nil {
		t.Fatalf("mkdir peer checkpoint: %v", err)
	}
	peerPayload := []byte(`{"snapshotMarkers":{"snap":{"id":"snap","folderId":"docs","cursor":7,"stateHash":"hash","createdAt":"` + createdAt + `"}},"folders":{"docs":{}}}`)
	if err := os.WriteFile(peerPath, peerPayload, 0o600); err != nil {
		t.Fatalf("write peer checkpoint: %v", err)
	}
	localPath := filepath.Join(checkpointRoot, "docs", "snap.json")
	if err := os.MkdirAll(filepath.Dir(localPath), 0o755); err != nil {
		t.Fatalf("mkdir local checkpoint: %v", err)
	}
	if err := os.WriteFile(localPath, []byte(`{"snapshotMarkers":`), 0o600); err != nil {
		t.Fatalf("write corrupt local checkpoint: %v", err)
	}

	result, err := ExecuteBackupCheckpointRepair(BackupCheckpointRepairOptions{CheckpointRoot: checkpointRoot, PeerCheckpointRoots: map[string]string{"peer-a": peerCheckpointRoot}, Store: store})
	if err != nil {
		t.Fatalf("execute checkpoint repair: %v", err)
	}
	if result.RepairedCheckpoints != 1 || result.UnresolvedCheckpoints != 0 || len(result.Actions) != 1 || result.Actions[0].SourcePeerID != "peer-a" {
		t.Fatalf("unexpected checkpoint repair result: %+v", result)
	}
	repaired, err := os.ReadFile(localPath)
	if err != nil {
		t.Fatalf("read repaired local checkpoint: %v", err)
	}
	if !bytes.Equal(repaired, peerPayload) {
		t.Fatalf("repaired checkpoint payload mismatch: %s", repaired)
	}

	badPeerPath := filepath.Join(peerCheckpointRoot, "docs", "wrong.json")
	if err := os.WriteFile(badPeerPath, []byte(`{"snapshotMarkers":{"wrong":{"id":"wrong","folderId":"docs","cursor":7,"stateHash":"hash"}}}`), 0o600); err != nil {
		t.Fatalf("write wrong peer checkpoint: %v", err)
	}
	if err := store.SaveSnapshotMarker(state.SnapshotMarker{ID: "wrong", FolderID: "docs", Cursor: 7, StateHash: "different-hash", CreatedAt: createdAt}); err != nil {
		t.Fatalf("save wrong marker: %v", err)
	}
	second, err := ExecuteBackupCheckpointRepair(BackupCheckpointRepairOptions{CheckpointRoot: checkpointRoot, PeerCheckpointRoots: map[string]string{"peer-a": peerCheckpointRoot}, Store: store})
	if err != nil {
		t.Fatalf("second checkpoint repair: %v", err)
	}
	if second.RepairedCheckpoints != 0 || second.UnresolvedCheckpoints != 1 || second.Unresolved[0].SnapshotID != "wrong" {
		t.Fatalf("mismatched peer checkpoint should stay unresolved: %+v", second)
	}
}

func TestPlanBackupArchiveRepairFindsSafeLiveRetainedAndPeerSources(t *testing.T) {
	root := t.TempDir()
	sourceRoot := filepath.Join(root, "source")
	archiveRoot := filepath.Join(root, "archive")
	peerArchiveRoot := filepath.Join(root, "peer-archive")
	store := state.NewJSONStore(filepath.Join(root, "state.json"))
	createdAt := time.Date(2026, 5, 25, 12, 0, 0, 0, time.UTC).Format(time.RFC3339)
	liveHash := sha256.Sum256([]byte("live"))
	retainedHash := sha256.Sum256([]byte("old"))
	peerHash := sha256.Sum256([]byte("peer"))
	unresolvedHash := sha256.Sum256([]byte("missing"))
	jobs := []state.ArchiveIntakeJob{
		{ID: "live", SnapshotID: "snap", FolderID: "docs", Path: "live.txt", Block: block.Block{Index: 0, Offset: 0, Size: 4, Hash: liveHash[:]}, Status: ArchiveJobStatusArchived, CreatedAt: createdAt},
		{ID: "retained", SnapshotID: "snap", FolderID: "docs", Path: "retained.txt", Block: block.Block{Index: 0, Offset: 0, Size: 3, Hash: retainedHash[:]}, Status: ArchiveJobStatusArchived, CreatedAt: createdAt},
		{ID: "peer", SnapshotID: "snap", FolderID: "docs", Path: "peer.txt", Block: block.Block{Index: 0, Offset: 0, Size: 4, Hash: peerHash[:]}, Status: ArchiveJobStatusArchived, CreatedAt: createdAt},
		{ID: "unresolved", SnapshotID: "snap", FolderID: "docs", Path: "missing.txt", Block: block.Block{Index: 0, Offset: 0, Size: 7, Hash: unresolvedHash[:]}, Status: ArchiveJobStatusArchived, CreatedAt: createdAt},
	}
	if err := store.SaveArchiveIntakeJobs("snap", jobs); err != nil {
		t.Fatalf("save jobs: %v", err)
	}
	if err := os.MkdirAll(sourceRoot, 0o755); err != nil {
		t.Fatalf("mkdir source: %v", err)
	}
	if err := os.WriteFile(filepath.Join(sourceRoot, "live.txt"), []byte("live"), 0o644); err != nil {
		t.Fatalf("write live source: %v", err)
	}
	if _, err := RetainBackupIntakeFile(sourceRoot, "retained.txt", []byte("old"), time.Date(2026, 5, 25, 13, 0, 0, 0, time.UTC)); err != nil {
		t.Fatalf("retain backup intake: %v", err)
	}
	if err := writeArchiveBlockAtomic(mustArchiveBlockPath(t, peerArchiveRoot, jobs[2].Block), []byte("peer")); err != nil {
		t.Fatalf("seed peer archive: %v", err)
	}

	plan, err := PlanBackupArchiveRepair(BackupArchiveRepairPlanOptions{ArchiveRoot: archiveRoot, SourceRoots: map[string]string{"docs": sourceRoot}, PeerArchiveRoots: map[string]string{"peer-a": peerArchiveRoot}, Store: store})
	if err != nil {
		t.Fatalf("plan backup archive repair: %v", err)
	}
	if plan.RepairableBlocks != 3 || plan.UnresolvedBlocks != 1 {
		t.Fatalf("unexpected repair summary: %+v", plan)
	}
	wantSources := map[string]bool{"live_file": false, "retained_backup_intake": false, "peer_archive": false}
	for _, action := range plan.Actions {
		wantSources[action.SourceKind] = true
		if action.Kind != "restore_archive_block" || action.Hash == "" || action.SourcePath == "" {
			t.Fatalf("unexpected repair action: %+v", action)
		}
	}
	for source, seen := range wantSources {
		if !seen {
			t.Fatalf("missing repair source %s in %+v", source, plan.Actions)
		}
	}
	if len(plan.Unresolved) != 1 || plan.Unresolved[0].Kind != "archive_block_unrepairable" || plan.Unresolved[0].JobID != "unresolved" {
		t.Fatalf("unexpected unresolved issues: %+v", plan.Unresolved)
	}
}

func TestExecuteBackupArchiveRepairRestoresVerifiedBlocksAndUpdatesJobs(t *testing.T) {
	root := t.TempDir()
	sourceRoot := filepath.Join(root, "source")
	archiveRoot := filepath.Join(root, "archive")
	peerArchiveRoot := filepath.Join(root, "peer-archive")
	store := state.NewJSONStore(filepath.Join(root, "state.json"))
	now := time.Date(2026, 5, 25, 14, 0, 0, 0, time.UTC)
	liveHash := sha256.Sum256([]byte("live"))
	peerHash := sha256.Sum256([]byte("peer"))
	collidingHash := sha256.Sum256([]byte("other"))
	collidingJobs := []state.ArchiveIntakeJob{
		{ID: "peer", SnapshotID: "other-snap", FolderID: "docs", Path: "other.txt", Block: block.Block{Index: 0, Offset: 0, Size: 5, Hash: collidingHash[:]}, Status: ArchiveJobStatusFailed, CreatedAt: now.Add(-time.Hour).Format(time.RFC3339), LastError: "unrelated"},
	}
	jobs := []state.ArchiveIntakeJob{
		{ID: "live", SnapshotID: "snap", FolderID: "docs", Path: "live.txt", Block: block.Block{Index: 0, Offset: 0, Size: 4, Hash: liveHash[:]}, Status: ArchiveJobStatusArchived, CreatedAt: now.Add(-time.Hour).Format(time.RFC3339)},
		{ID: "peer", SnapshotID: "snap", FolderID: "docs", Path: "peer.txt", Block: block.Block{Index: 0, Offset: 0, Size: 4, Hash: peerHash[:]}, Status: ArchiveJobStatusFailed, CreatedAt: now.Add(-time.Hour).Format(time.RFC3339), LastError: "missing"},
	}
	if err := store.SaveArchiveIntakeJobs("other-snap", collidingJobs); err != nil {
		t.Fatalf("save colliding jobs: %v", err)
	}
	if err := store.SaveArchiveIntakeJobs("snap", jobs); err != nil {
		t.Fatalf("save jobs: %v", err)
	}
	if err := os.MkdirAll(sourceRoot, 0o755); err != nil {
		t.Fatalf("mkdir source: %v", err)
	}
	if err := os.WriteFile(filepath.Join(sourceRoot, "live.txt"), []byte("live"), 0o644); err != nil {
		t.Fatalf("write live source: %v", err)
	}
	if err := writeArchiveBlockAtomic(mustArchiveBlockPath(t, peerArchiveRoot, jobs[1].Block), []byte("peer")); err != nil {
		t.Fatalf("seed peer archive: %v", err)
	}

	result, err := ExecuteBackupArchiveRepair(BackupArchiveRepairOptions{ArchiveRoot: archiveRoot, SourceRoots: map[string]string{"docs": sourceRoot}, PeerArchiveRoots: map[string]string{"peer-a": peerArchiveRoot}, Store: store, Now: now})
	if err != nil {
		t.Fatalf("execute backup archive repair: %v", err)
	}
	if result.RepairedBlocks != 2 || result.UnresolvedBlocks != 1 {
		t.Fatalf("unexpected repair result: %+v", result)
	}
	for _, job := range jobs {
		path := mustArchiveBlockPath(t, archiveRoot, job.Block)
		ok, err := verifyExistingArchiveBlock(path, job.Block)
		if err != nil || !ok {
			t.Fatalf("archive block %s was not restored: ok=%v err=%v", job.ID, ok, err)
		}
	}
	stored, err := store.ListArchiveIntakeJobs("snap")
	if err != nil {
		t.Fatalf("list jobs after repair: %v", err)
	}
	for _, job := range stored {
		if job.Status != ArchiveJobStatusArchived || job.ArchivedAt != now.Format(time.RFC3339) || job.LastError != "" {
			t.Fatalf("job was not marked archived cleanly: %+v", job)
		}
	}
	otherStored, err := store.ListArchiveIntakeJobs("other-snap")
	if err != nil {
		t.Fatalf("list colliding jobs after repair: %v", err)
	}
	if len(otherStored) != 1 || otherStored[0].Status != ArchiveJobStatusFailed || otherStored[0].LastError != "unrelated" || otherStored[0].ArchivedAt != "" {
		t.Fatalf("colliding job ID in another snapshot was changed: %+v", otherStored)
	}
}

func TestExecuteBackupArchiveRepairPersistsJobProgress(t *testing.T) {
	root := t.TempDir()
	sourceRoot := filepath.Join(root, "source")
	archiveRoot := filepath.Join(root, "archive")
	store := state.NewJSONStore(filepath.Join(root, "state.json"))
	now := time.Date(2026, 5, 25, 15, 0, 0, 0, time.UTC)
	repairedHash := sha256.Sum256([]byte("repair-me"))
	missingHash := sha256.Sum256([]byte("missing"))
	jobs := []state.ArchiveIntakeJob{
		{ID: "repairable", SnapshotID: "snap", FolderID: "docs", Path: "repairable.txt", Block: block.Block{Index: 0, Offset: 0, Size: 9, Hash: repairedHash[:]}, Status: ArchiveJobStatusFailed, CreatedAt: now.Add(-time.Hour).Format(time.RFC3339), LastError: "missing"},
		{ID: "unresolved", SnapshotID: "snap", FolderID: "docs", Path: "missing.txt", Block: block.Block{Index: 0, Offset: 0, Size: 7, Hash: missingHash[:]}, Status: ArchiveJobStatusFailed, CreatedAt: now.Add(-time.Hour).Format(time.RFC3339), LastError: "missing"},
	}
	if err := store.SaveArchiveIntakeJobs("snap", jobs); err != nil {
		t.Fatalf("save jobs: %v", err)
	}
	if err := os.MkdirAll(sourceRoot, 0o755); err != nil {
		t.Fatalf("mkdir source: %v", err)
	}
	if err := os.WriteFile(filepath.Join(sourceRoot, "repairable.txt"), []byte("repair-me"), 0o644); err != nil {
		t.Fatalf("write repair source: %v", err)
	}

	result, err := ExecuteBackupArchiveRepair(BackupArchiveRepairOptions{ArchiveRoot: archiveRoot, SourceRoots: map[string]string{"docs": sourceRoot}, Store: store, JobID: "repair-job-1", Now: now})
	if err != nil {
		t.Fatalf("execute backup archive repair: %v", err)
	}
	if result.RepairedBlocks != 1 || result.UnresolvedBlocks != 1 {
		t.Fatalf("unexpected repair result: %+v", result)
	}
	job, ok, err := store.LoadBackupRepairJob("repair-job-1")
	if err != nil {
		t.Fatalf("load durable repair job: %v", err)
	}
	if !ok {
		t.Fatalf("durable repair job was not persisted")
	}
	if job.Status != "completed" || job.TotalBlocks != 2 || job.RepairedBlocks != 1 || job.UnresolvedBlocks != 1 || job.RemainingBlocks != 0 {
		t.Fatalf("unexpected durable repair job progress: %+v", job)
	}
	if len(job.Blocks) != 2 || job.Blocks[0].Status != "repaired" || job.Blocks[1].Status != "unresolved" {
		t.Fatalf("durable repair job should checkpoint per-block completion states: %+v", job.Blocks)
	}
}

func TestSnapshotAvailabilityStatusSeparatesMetadataArchiveAndCheckpointPresence(t *testing.T) {
	root := t.TempDir()
	archiveRoot := filepath.Join(root, "archive")
	checkpointRoot := filepath.Join(root, "checkpoints")
	store := state.NewJSONStore(filepath.Join(root, "state.json"))
	createdAt := time.Date(2026, 5, 25, 12, 0, 0, 0, time.UTC).Format(time.RFC3339)
	markers := []state.SnapshotMarker{
		{ID: "snap-metadata-only", FolderID: "docs", Cursor: 10, StateHash: "hash-meta", CreatedAt: createdAt},
		{ID: "snap-archived", FolderID: "docs", Cursor: 11, StateHash: "hash-archived", CreatedAt: createdAt},
	}
	for _, marker := range markers {
		if err := store.SaveSnapshotMarker(marker); err != nil {
			t.Fatalf("save marker %s: %v", marker.ID, err)
		}
	}
	payloadHash := sha256.Sum256([]byte("payload"))
	job := state.ArchiveIntakeJob{ID: "job", SnapshotID: "snap-archived", FolderID: "docs", Path: "file.txt", Block: block.Block{Index: 0, Offset: 0, Size: 7, Hash: payloadHash[:]}, Status: ArchiveJobStatusArchived, CreatedAt: createdAt}
	if err := store.SaveArchiveIntakeJobs("snap-archived", []state.ArchiveIntakeJob{job}); err != nil {
		t.Fatalf("save archive job: %v", err)
	}
	if err := writeArchiveBlockAtomic(mustArchiveBlockPath(t, archiveRoot, job.Block), []byte("payload")); err != nil {
		t.Fatalf("seed archive block: %v", err)
	}
	checkpointPath := filepath.Join(checkpointRoot, "docs", "snap-metadata-only.json")
	if err := os.MkdirAll(filepath.Dir(checkpointPath), 0o755); err != nil {
		t.Fatalf("mkdir checkpoint dir: %v", err)
	}
	if err := os.WriteFile(checkpointPath, []byte(`{"snapshot":"metadata-only"}`), 0o644); err != nil {
		t.Fatalf("write checkpoint: %v", err)
	}

	status, err := ComputeSnapshotAvailabilityStatus(SnapshotAvailabilityOptions{ArchiveRoot: archiveRoot, CheckpointRoot: checkpointRoot, Store: store})
	if err != nil {
		t.Fatalf("compute snapshot availability: %v", err)
	}
	if status.TotalSnapshots != 2 || status.MetadataSnapshots != 2 || status.ArchiveProtectedSnapshots != 1 || status.DBCheckpointSnapshots != 1 {
		t.Fatalf("unexpected aggregate availability: %+v", status)
	}
	metadataOnly := status.Snapshots["snap-metadata-only"]
	if !metadataOnly.MetadataPresent || !metadataOnly.DBCheckpointAvailable || metadataOnly.Archive.TotalBlocks != 0 || metadataOnly.ArchiveFullyProtected {
		t.Fatalf("metadata-only snapshot should not be confused with archive protection: %+v", metadataOnly)
	}
	archived := status.Snapshots["snap-archived"]
	if !archived.MetadataPresent || archived.DBCheckpointAvailable || !archived.ArchiveFullyProtected || archived.Archive.ProtectedBlocks != 1 {
		t.Fatalf("archived snapshot should advertise archive protection separately from checkpoint presence: %+v", archived)
	}
}

func TestPlanSnapshotRestoreDryRunSelectsFilesAndRequiresVerifiedArchiveBlocks(t *testing.T) {
	root := t.TempDir()
	archiveRoot := filepath.Join(root, "archive")
	store := state.NewJSONStore(filepath.Join(root, "state.json"))
	alphaHash := sha256.Sum256([]byte("alpha"))
	betaHash := sha256.Sum256([]byte("beta"))
	if err := store.SaveManifest("docs", "dir/alpha.txt", block.Manifest{Path: "dir/alpha.txt", Size: 5, BlockSize: 4096, Blocks: []block.Block{{Index: 0, Offset: 0, Size: 5, Hash: alphaHash[:]}}}); err != nil {
		t.Fatalf("save alpha manifest: %v", err)
	}
	if err := store.SaveManifest("docs", "dir/beta.txt", block.Manifest{Path: "dir/beta.txt", Size: 4, BlockSize: 4096, Blocks: []block.Block{{Index: 0, Offset: 0, Size: 4, Hash: betaHash[:]}}}); err != nil {
		t.Fatalf("save beta manifest: %v", err)
	}
	summary, err := store.FolderSummary("docs")
	if err != nil {
		t.Fatalf("folder summary: %v", err)
	}
	marker := state.SnapshotMarker{ID: "snap", FolderID: "docs", Cursor: summary.Cursor, StateHash: summary.StateHash, CreatedAt: time.Now().UTC().Format(time.RFC3339)}
	if err := store.SaveSnapshotMarker(marker); err != nil {
		t.Fatalf("save snapshot marker: %v", err)
	}
	if err := writeArchiveBlockAtomic(mustArchiveBlockPath(t, archiveRoot, block.Block{Index: 0, Offset: 0, Size: 5, Hash: alphaHash[:]}), []byte("alpha")); err != nil {
		t.Fatalf("seed alpha archive block: %v", err)
	}

	plan, err := PlanSnapshotRestore(RestorePlanOptions{Store: store, ArchiveRoot: archiveRoot, SnapshotID: "snap", Paths: []string{"dir/alpha.txt"}, DestinationRoot: filepath.Join(root, "restore"), DryRun: true})
	if err != nil {
		t.Fatalf("plan snapshot restore: %v", err)
	}
	if plan.SnapshotID != "snap" || plan.FolderID != "docs" || plan.TotalFiles != 1 || plan.TotalBytes != 5 || !plan.DryRun {
		t.Fatalf("unexpected restore plan summary: %+v", plan)
	}
	if len(plan.Files) != 1 || plan.Files[0].Path != "dir/alpha.txt" || plan.Files[0].DestinationPath != filepath.Join(root, "restore", "dir", "alpha.txt") || !plan.Files[0].ArchiveAvailable || len(plan.Files[0].MissingBlocks) != 0 {
		t.Fatalf("unexpected selected file plan: %+v", plan.Files)
	}

	missingPlan, err := PlanSnapshotRestore(RestorePlanOptions{Store: store, ArchiveRoot: archiveRoot, SnapshotID: "snap", Paths: []string{"dir/beta.txt"}, DestinationRoot: filepath.Join(root, "restore"), DryRun: true})
	if err != nil {
		t.Fatalf("plan missing-block snapshot restore: %v", err)
	}
	if len(missingPlan.Files) != 1 || missingPlan.Files[0].ArchiveAvailable || len(missingPlan.Files[0].MissingBlocks) != 1 || missingPlan.MissingBlocks != 1 {
		t.Fatalf("missing archive block should keep restore dry-run from pretending protected: %+v", missingPlan)
	}
}

func TestPlanSnapshotRestoreRejectsUnsafeAlternateDestination(t *testing.T) {
	root := t.TempDir()
	archiveRoot := filepath.Join(root, "archive")
	store := state.NewJSONStore(filepath.Join(root, "state.json"))
	alphaHash := sha256.Sum256([]byte("alpha"))
	manifest := block.Manifest{Path: "dir/file.txt", Size: 5, BlockSize: 4096, Blocks: []block.Block{{Index: 0, Offset: 0, Size: 5, Hash: alphaHash[:]}}}
	if err := store.SaveManifest("docs", "dir/file.txt", manifest); err != nil {
		t.Fatalf("save manifest: %v", err)
	}
	summary, err := store.FolderSummary("docs")
	if err != nil {
		t.Fatalf("folder summary: %v", err)
	}
	if err := store.SaveSnapshotMarker(state.SnapshotMarker{ID: "snap", FolderID: "docs", Cursor: summary.Cursor, StateHash: summary.StateHash, CreatedAt: time.Now().UTC().Format(time.RFC3339)}); err != nil {
		t.Fatalf("save snapshot marker: %v", err)
	}
	if err := writeArchiveBlockAtomic(mustArchiveBlockPath(t, archiveRoot, manifest.Blocks[0]), []byte("alpha")); err != nil {
		t.Fatalf("seed alpha archive block: %v", err)
	}

	if _, err := PlanSnapshotRestore(RestorePlanOptions{Store: store, ArchiveRoot: archiveRoot, SnapshotID: "snap", Paths: []string{"dir/file.txt"}, DestinationRoot: filepath.Join(root, "restore"), AlternatePath: "../escape.txt", DryRun: true}); err == nil {
		t.Fatalf("unsafe alternate restore destination should be rejected")
	}
}

func TestExecuteSnapshotRestoreAssemblesVerifiedArchiveBlocks(t *testing.T) {
	root := t.TempDir()
	archiveRoot := filepath.Join(root, "archive")
	destinationRoot := filepath.Join(root, "restore")
	store := state.NewJSONStore(filepath.Join(root, "state.json"))
	alphaHash := sha256.Sum256([]byte("alpha"))
	betaHash := sha256.Sum256([]byte("beta"))
	manifest := block.Manifest{Path: "dir/file.txt", Size: 9, BlockSize: 5, Blocks: []block.Block{{Index: 0, Offset: 0, Size: 5, Hash: alphaHash[:]}, {Index: 1, Offset: 5, Size: 4, Hash: betaHash[:]}}}
	if err := store.SaveManifest("docs", "dir/file.txt", manifest); err != nil {
		t.Fatalf("save manifest: %v", err)
	}
	summary, err := store.FolderSummary("docs")
	if err != nil {
		t.Fatalf("folder summary: %v", err)
	}
	if err := store.SaveSnapshotMarker(state.SnapshotMarker{ID: "snap", FolderID: "docs", Cursor: summary.Cursor, StateHash: summary.StateHash, CreatedAt: time.Now().UTC().Format(time.RFC3339)}); err != nil {
		t.Fatalf("save snapshot marker: %v", err)
	}
	if err := writeArchiveBlockAtomic(mustArchiveBlockPath(t, archiveRoot, manifest.Blocks[0]), []byte("alpha")); err != nil {
		t.Fatalf("seed alpha archive block: %v", err)
	}
	if err := writeArchiveBlockAtomic(mustArchiveBlockPath(t, archiveRoot, manifest.Blocks[1]), []byte("beta")); err != nil {
		t.Fatalf("seed beta archive block: %v", err)
	}

	result, err := ExecuteSnapshotRestore(RestorePlanOptions{Store: store, ArchiveRoot: archiveRoot, SnapshotID: "snap", Paths: []string{"dir/file.txt"}, DestinationRoot: destinationRoot, DryRun: false})
	if err != nil {
		t.Fatalf("execute snapshot restore: %v", err)
	}
	if result.RestoredFiles != 1 || result.RestoredBytes != 9 || result.SkippedFiles != 0 {
		t.Fatalf("unexpected restore result: %+v", result)
	}
	restored, err := os.ReadFile(filepath.Join(destinationRoot, "dir", "file.txt"))
	if err != nil {
		t.Fatalf("read restored file: %v", err)
	}
	if string(restored) != "alphabeta" {
		t.Fatalf("restored content mismatch: %q", string(restored))
	}
}

func TestExecuteSnapshotRestoreSkipsAlreadyVerifiedDestinationFiles(t *testing.T) {
	root := t.TempDir()
	archiveRoot := filepath.Join(root, "archive")
	destinationRoot := filepath.Join(root, "restore")
	store := state.NewJSONStore(filepath.Join(root, "state.json"))
	alphaHash := sha256.Sum256([]byte("alpha"))
	betaHash := sha256.Sum256([]byte("beta"))
	alphaManifest := block.Manifest{Path: "alpha.txt", Size: 5, BlockSize: 5, Blocks: []block.Block{{Index: 0, Offset: 0, Size: 5, Hash: alphaHash[:]}}}
	betaManifest := block.Manifest{Path: "beta.txt", Size: 4, BlockSize: 4, Blocks: []block.Block{{Index: 0, Offset: 0, Size: 4, Hash: betaHash[:]}}}
	if err := store.SaveManifest("docs", "alpha.txt", alphaManifest); err != nil {
		t.Fatalf("save alpha manifest: %v", err)
	}
	if err := store.SaveManifest("docs", "beta.txt", betaManifest); err != nil {
		t.Fatalf("save beta manifest: %v", err)
	}
	summary, err := store.FolderSummary("docs")
	if err != nil {
		t.Fatalf("folder summary: %v", err)
	}
	if err := store.SaveSnapshotMarker(state.SnapshotMarker{ID: "snap", FolderID: "docs", Cursor: summary.Cursor, StateHash: summary.StateHash, CreatedAt: time.Now().UTC().Format(time.RFC3339)}); err != nil {
		t.Fatalf("save snapshot marker: %v", err)
	}
	if err := writeArchiveBlockAtomic(mustArchiveBlockPath(t, archiveRoot, alphaManifest.Blocks[0]), []byte("alpha")); err != nil {
		t.Fatalf("seed alpha archive block: %v", err)
	}
	if err := writeArchiveBlockAtomic(mustArchiveBlockPath(t, archiveRoot, betaManifest.Blocks[0]), []byte("beta")); err != nil {
		t.Fatalf("seed beta archive block: %v", err)
	}
	if err := os.MkdirAll(destinationRoot, 0o755); err != nil {
		t.Fatalf("mkdir destination: %v", err)
	}
	alphaDestination := filepath.Join(destinationRoot, "alpha.txt")
	if err := os.WriteFile(alphaDestination, []byte("alpha"), 0o644); err != nil {
		t.Fatalf("seed already restored destination: %v", err)
	}
	before, err := os.Stat(alphaDestination)
	if err != nil {
		t.Fatalf("stat already restored destination: %v", err)
	}

	result, err := ExecuteSnapshotRestore(RestorePlanOptions{Store: store, ArchiveRoot: archiveRoot, SnapshotID: "snap", DestinationRoot: destinationRoot, JobID: "restore-job-1", Now: time.Date(2026, 5, 25, 12, 0, 0, 0, time.UTC)})
	if err != nil {
		t.Fatalf("execute resumable snapshot restore: %v", err)
	}
	if result.TotalFiles != 2 || result.RestoredFiles != 1 || result.SkippedFiles != 1 || result.RemainingFiles != 0 || result.RestoredBytes != 4 {
		t.Fatalf("restore should report resumable totals while skipping already verified file and restoring only missing file: %+v", result)
	}
	job, ok, err := store.LoadBackupRestoreJob("restore-job-1")
	if err != nil {
		t.Fatalf("load durable restore job: %v", err)
	}
	if !ok {
		t.Fatalf("durable restore job was not persisted")
	}
	if job.Status != "completed" || job.TotalFiles != 2 || job.RestoredFiles != 1 || job.SkippedFiles != 1 || job.RemainingFiles != 0 {
		t.Fatalf("unexpected durable restore job progress: %+v", job)
	}
	if len(job.Files) != 2 || job.Files[0].Status != "skipped" || job.Files[1].Status != "restored" {
		t.Fatalf("durable restore job should checkpoint per-file completion states: %+v", job.Files)
	}
	after, err := os.Stat(alphaDestination)
	if err != nil {
		t.Fatalf("stat skipped destination: %v", err)
	}
	if !after.ModTime().Equal(before.ModTime()) {
		t.Fatalf("verified destination file should not be rewritten during resumable restore")
	}
	restoredBeta, err := os.ReadFile(filepath.Join(destinationRoot, "beta.txt"))
	if err != nil {
		t.Fatalf("read restored beta: %v", err)
	}
	if string(restoredBeta) != "beta" {
		t.Fatalf("restored beta mismatch: %q", restoredBeta)
	}
}

func TestAuthorizeSnapshotDatabaseReversionRequiresExplicitGates(t *testing.T) {
	root := t.TempDir()
	checkpointRoot := filepath.Join(root, "checkpoints")
	store := state.NewJSONStore(filepath.Join(root, "state.json"))
	marker := state.SnapshotMarker{ID: "snap", FolderID: "docs", Cursor: 7, StateHash: "hash", CreatedAt: time.Now().UTC().Format(time.RFC3339)}
	if err := store.SaveSnapshotMarker(marker); err != nil {
		t.Fatalf("save snapshot marker: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(checkpointRoot, "docs"), 0o755); err != nil {
		t.Fatalf("mkdir checkpoint folder: %v", err)
	}
	if err := os.WriteFile(filepath.Join(checkpointRoot, "docs", "snap.json"), []byte(`{"snapshot":"snap"}`), 0o600); err != nil {
		t.Fatalf("write checkpoint: %v", err)
	}

	if _, err := AuthorizeSnapshotDatabaseReversion(DatabaseReversionOptions{Store: store, CheckpointRoot: checkpointRoot, SnapshotID: "snap", ConfirmSnapshotID: "snap"}); err == nil {
		t.Fatalf("database reversion without explicit authorization should fail")
	}
	if _, err := AuthorizeSnapshotDatabaseReversion(DatabaseReversionOptions{Store: store, CheckpointRoot: checkpointRoot, SnapshotID: "snap", AllowDatabaseReversion: true, ConfirmSnapshotID: "wrong"}); err == nil {
		t.Fatalf("database reversion without exact snapshot confirmation should fail")
	}

	plan, err := AuthorizeSnapshotDatabaseReversion(DatabaseReversionOptions{Store: store, CheckpointRoot: checkpointRoot, SnapshotID: "snap", AllowDatabaseReversion: true, ConfirmSnapshotID: "snap"})
	if err != nil {
		t.Fatalf("authorize database reversion: %v", err)
	}
	if !plan.Authorized || plan.SnapshotID != "snap" || plan.FolderID != "docs" || plan.CheckpointPath != filepath.Join(checkpointRoot, "docs", "snap.json") {
		t.Fatalf("unexpected database reversion authorization plan: %+v", plan)
	}
}

func TestExecuteSnapshotRestoreRefusesMissingArchiveBlocksWithoutPartialWrite(t *testing.T) {
	root := t.TempDir()
	archiveRoot := filepath.Join(root, "archive")
	destinationRoot := filepath.Join(root, "restore")
	store := state.NewJSONStore(filepath.Join(root, "state.json"))
	missingHash := sha256.Sum256([]byte("missing"))
	if err := store.SaveManifest("docs", "file.txt", block.Manifest{Path: "file.txt", Size: 7, BlockSize: 4096, Blocks: []block.Block{{Index: 0, Offset: 0, Size: 7, Hash: missingHash[:]}}}); err != nil {
		t.Fatalf("save manifest: %v", err)
	}
	summary, err := store.FolderSummary("docs")
	if err != nil {
		t.Fatalf("folder summary: %v", err)
	}
	if err := store.SaveSnapshotMarker(state.SnapshotMarker{ID: "snap", FolderID: "docs", Cursor: summary.Cursor, StateHash: summary.StateHash, CreatedAt: time.Now().UTC().Format(time.RFC3339)}); err != nil {
		t.Fatalf("save snapshot marker: %v", err)
	}

	if _, err := ExecuteSnapshotRestore(RestorePlanOptions{Store: store, ArchiveRoot: archiveRoot, SnapshotID: "snap", DestinationRoot: destinationRoot, DryRun: false}); err == nil {
		t.Fatalf("execute restore should fail when archive blocks are missing")
	}
	if _, err := os.Stat(filepath.Join(destinationRoot, "file.txt")); !os.IsNotExist(err) {
		t.Fatalf("restore should not leave a partial destination file, stat err=%v", err)
	}
}

func TestPruneBackupIntakeFilesDeletesOnlyOldProtectedRetainedBytes(t *testing.T) {
	root := t.TempDir()
	sourceRoot := filepath.Join(root, "source")
	archiveRoot := filepath.Join(root, "archive")
	if err := os.MkdirAll(sourceRoot, 0o755); err != nil {
		t.Fatalf("mkdir source: %v", err)
	}
	now := time.Date(2026, 5, 25, 12, 0, 0, 0, time.UTC)
	oldAt := now.Add(-48 * time.Hour)
	youngAt := now.Add(-time.Hour)
	protectedHash := sha256.Sum256([]byte("old protected"))
	pendingHash := sha256.Sum256([]byte("old pending"))
	youngHash := sha256.Sum256([]byte("young protected"))
	protectedPath, err := RetainBackupIntakeFile(sourceRoot, "protected.txt", []byte("old protected"), oldAt)
	if err != nil {
		t.Fatalf("retain protected: %v", err)
	}
	pendingPath, err := RetainBackupIntakeFile(sourceRoot, "pending.txt", []byte("old pending"), oldAt)
	if err != nil {
		t.Fatalf("retain pending: %v", err)
	}
	youngPath, err := RetainBackupIntakeFile(sourceRoot, "young.txt", []byte("young protected"), youngAt)
	if err != nil {
		t.Fatalf("retain young: %v", err)
	}
	store := state.NewJSONStore(filepath.Join(root, "state.json"))
	jobs := []state.ArchiveIntakeJob{
		{ID: "protected", SnapshotID: "snap", FolderID: "docs", Path: "protected.txt", Block: block.Block{Index: 0, Offset: 0, Size: len("old protected"), Hash: protectedHash[:]}, Status: ArchiveJobStatusArchived, CreatedAt: oldAt.Format(time.RFC3339)},
		{ID: "pending", SnapshotID: "snap", FolderID: "docs", Path: "pending.txt", Block: block.Block{Index: 0, Offset: 0, Size: len("old pending"), Hash: pendingHash[:]}, Status: ArchiveJobStatusPending, CreatedAt: oldAt.Format(time.RFC3339)},
		{ID: "young", SnapshotID: "snap", FolderID: "docs", Path: "young.txt", Block: block.Block{Index: 0, Offset: 0, Size: len("young protected"), Hash: youngHash[:]}, Status: ArchiveJobStatusArchived, CreatedAt: oldAt.Format(time.RFC3339)},
	}
	if err := store.SaveArchiveIntakeJobs("snap", jobs); err != nil {
		t.Fatalf("save jobs: %v", err)
	}
	for _, job := range []state.ArchiveIntakeJob{jobs[0], jobs[2]} {
		if err := writeArchiveBlockAtomic(mustArchiveBlockPath(t, archiveRoot, job.Block), []byte(blockPayloadForPath(job.Path))); err != nil {
			t.Fatalf("write archived block for %s: %v", job.Path, err)
		}
	}

	result, err := PruneBackupIntakeFiles(BackupIntakePruneOptions{SourceRoot: sourceRoot, ArchiveRoot: archiveRoot, Store: store, Now: now, MinAge: 24 * time.Hour})
	if err != nil {
		t.Fatalf("prune backup intake files: %v", err)
	}
	if result.DeletedFiles != 1 || result.KeptFiles != 2 {
		t.Fatalf("unexpected prune result: %+v", result)
	}
	if _, err := os.Stat(protectedPath); !os.IsNotExist(err) {
		t.Fatalf("old protected retained file should be deleted, stat err=%v", err)
	}
	for _, path := range []string{pendingPath, youngPath} {
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("retained file %s should remain: %v", path, err)
		}
	}
}

func blockPayloadForPath(path string) string {
	switch path {
	case "protected.txt":
		return "old protected"
	case "young.txt":
		return "young protected"
	default:
		return ""
	}
}

func mustArchiveBlockPath(t *testing.T, archiveRoot string, b block.Block) string {
	t.Helper()
	path, err := archiveBlockPath(archiveRoot, b)
	if err != nil {
		t.Fatalf("archive block path: %v", err)
	}
	return path
}

func TestPlanSnapshotRetentionDeprecatesBeforeDeleteAndKeepsReferencedBlocks(t *testing.T) {
	root := t.TempDir()
	store := state.NewJSONStore(filepath.Join(root, "state.json"))
	alphaHash := sha256.Sum256([]byte("alpha"))
	oldOnlyHash := sha256.Sum256([]byte("old-only"))
	betaHash := sha256.Sum256([]byte("beta"))
	alphaManifest := block.Manifest{Path: "alpha.txt", Size: 5, BlockSize: 5, Blocks: []block.Block{{Index: 0, Offset: 0, Size: 5, Hash: alphaHash[:]}}}
	oldOnlyManifest := block.Manifest{Path: "old-only.txt", Size: 8, BlockSize: 8, Blocks: []block.Block{{Index: 0, Offset: 0, Size: 8, Hash: oldOnlyHash[:]}}}
	betaManifest := block.Manifest{Path: "beta.txt", Size: 4, BlockSize: 4, Blocks: []block.Block{{Index: 0, Offset: 0, Size: 4, Hash: betaHash[:]}}}
	if err := store.SaveManifest("docs", "alpha.txt", alphaManifest); err != nil {
		t.Fatalf("save alpha manifest: %v", err)
	}
	if err := store.SaveManifest("docs", "old-only.txt", oldOnlyManifest); err != nil {
		t.Fatalf("save old-only manifest: %v", err)
	}
	oldSummary, err := store.FolderSummary("docs")
	if err != nil {
		t.Fatalf("old folder summary: %v", err)
	}
	oldMarker := state.SnapshotMarker{ID: "snap-old", FolderID: "docs", Cursor: oldSummary.Cursor, StateHash: oldSummary.StateHash, CreatedAt: "2026-05-24T10:00:00Z"}
	if err := store.SaveSnapshotMarker(oldMarker); err != nil {
		t.Fatalf("save old marker: %v", err)
	}
	if err := store.DeleteManifest("docs", "old-only.txt"); err != nil {
		t.Fatalf("delete old-only manifest: %v", err)
	}
	if err := store.SaveManifest("docs", "beta.txt", betaManifest); err != nil {
		t.Fatalf("save beta manifest: %v", err)
	}
	newSummary, err := store.FolderSummary("docs")
	if err != nil {
		t.Fatalf("new folder summary: %v", err)
	}
	if err := store.SaveSnapshotMarker(state.SnapshotMarker{ID: "snap-new", FolderID: "docs", Cursor: newSummary.Cursor, StateHash: newSummary.StateHash, CreatedAt: "2026-05-24T11:00:00Z"}); err != nil {
		t.Fatalf("save new marker: %v", err)
	}

	first, err := PlanSnapshotRetention(SnapshotRetentionOptions{Store: store, KeepLast: 1})
	if err != nil {
		t.Fatalf("plan first retention pass: %v", err)
	}
	if len(first.DeprecateSnapshots) != 1 || first.DeprecateSnapshots[0] != "snap-old" {
		t.Fatalf("first pass should only request deprecation of old snapshot: %+v", first)
	}
	if len(first.DeleteSnapshots) != 0 || len(first.ArchiveBlocksEligibleForSweep) != 0 {
		t.Fatalf("first pass must not delete or sweep before deprecation: %+v", first)
	}

	oldMarker.Deprecated = true
	if err := store.SaveSnapshotMarker(oldMarker); err != nil {
		t.Fatalf("mark old marker deprecated: %v", err)
	}
	second, err := PlanSnapshotRetention(SnapshotRetentionOptions{Store: store, KeepLast: 1})
	if err != nil {
		t.Fatalf("plan second retention pass: %v", err)
	}
	if len(second.DeleteSnapshots) != 1 || second.DeleteSnapshots[0] != "snap-old" {
		t.Fatalf("deprecated old snapshot should be eligible for deletion: %+v", second)
	}
	if len(second.Promotions) != 1 || second.Promotions[0].FromSnapshotID != "snap-old" || second.Promotions[0].ToSnapshotID != "snap-new" || second.Promotions[0].Path != "alpha.txt" {
		t.Fatalf("inherited alpha entry should be promoted to next retained snapshot before delete: %+v", second.Promotions)
	}
	if got, want := len(second.ArchiveBlocksEligibleForSweep), 1; got != want {
		t.Fatalf("only unreferenced old-only archive block should be sweep-eligible, got %d: %+v", got, second.ArchiveBlocksEligibleForSweep)
	}
	if second.ArchiveBlocksEligibleForSweep[0].Hash != hex.EncodeToString(oldOnlyHash[:]) {
		t.Fatalf("sweep candidate hash = %s, want old-only hash", second.ArchiveBlocksEligibleForSweep[0].Hash)
	}
}

func TestExecuteSnapshotRetentionSweepsOnlyUnreferencedArchiveBlocks(t *testing.T) {
	root := t.TempDir()
	archiveRoot := filepath.Join(root, "archive")
	store := state.NewJSONStore(filepath.Join(root, "state.json"))
	sharedHash := sha256.Sum256([]byte("shared"))
	oldOnlyHash := sha256.Sum256([]byte("old-only"))
	newHash := sha256.Sum256([]byte("new"))
	sharedBlock := block.Block{Index: 0, Offset: 0, Size: 6, Hash: sharedHash[:]}
	oldOnlyBlock := block.Block{Index: 0, Offset: 0, Size: 8, Hash: oldOnlyHash[:]}
	newBlock := block.Block{Index: 0, Offset: 0, Size: 3, Hash: newHash[:]}
	sharedManifest := block.Manifest{Path: "shared.txt", Size: 6, BlockSize: 6, Blocks: []block.Block{sharedBlock}}
	oldOnlyManifest := block.Manifest{Path: "old-only.txt", Size: 8, BlockSize: 8, Blocks: []block.Block{oldOnlyBlock}}
	newManifest := block.Manifest{Path: "new.txt", Size: 3, BlockSize: 3, Blocks: []block.Block{newBlock}}
	if err := store.SaveManifest("docs", "shared.txt", sharedManifest); err != nil {
		t.Fatalf("save shared manifest: %v", err)
	}
	if err := store.SaveManifest("docs", "old-only.txt", oldOnlyManifest); err != nil {
		t.Fatalf("save old-only manifest: %v", err)
	}
	oldSummary, err := store.FolderSummary("docs")
	if err != nil {
		t.Fatalf("old folder summary: %v", err)
	}
	oldMarker := state.SnapshotMarker{ID: "snap-old", FolderID: "docs", Cursor: oldSummary.Cursor, StateHash: oldSummary.StateHash, CreatedAt: "2026-05-24T10:00:00Z", Deprecated: true}
	if err := store.SaveSnapshotMarker(oldMarker); err != nil {
		t.Fatalf("save old marker: %v", err)
	}
	if err := writeArchiveBlockAtomic(mustArchiveBlockPath(t, archiveRoot, sharedBlock), []byte("shared")); err != nil {
		t.Fatalf("write shared archive block: %v", err)
	}
	if err := writeArchiveBlockAtomic(mustArchiveBlockPath(t, archiveRoot, oldOnlyBlock), []byte("old-only")); err != nil {
		t.Fatalf("write old-only archive block: %v", err)
	}
	if err := store.DeleteManifest("docs", "old-only.txt"); err != nil {
		t.Fatalf("delete old-only manifest: %v", err)
	}
	if err := store.SaveManifest("docs", "new.txt", newManifest); err != nil {
		t.Fatalf("save new manifest: %v", err)
	}
	newSummary, err := store.FolderSummary("docs")
	if err != nil {
		t.Fatalf("new folder summary: %v", err)
	}
	if err := store.SaveSnapshotMarker(state.SnapshotMarker{ID: "snap-new", FolderID: "docs", Cursor: newSummary.Cursor, StateHash: newSummary.StateHash, CreatedAt: "2026-05-24T11:00:00Z"}); err != nil {
		t.Fatalf("save new marker: %v", err)
	}

	result, err := ExecuteSnapshotRetention(SnapshotRetentionOptions{Store: store, KeepLast: 1, ArchiveRoot: archiveRoot})
	if err != nil {
		t.Fatalf("execute retention: %v", err)
	}
	if got, want := len(result.ArchiveBlocksEligibleForSweep), 1; got != want {
		t.Fatalf("swept blocks = %d, want %d: %+v", got, want, result.ArchiveBlocksEligibleForSweep)
	}
	if _, err := os.Stat(mustArchiveBlockPath(t, archiveRoot, oldOnlyBlock)); !os.IsNotExist(err) {
		t.Fatalf("old-only archive block should be removed, stat err=%v", err)
	}
	if _, err := os.Stat(mustArchiveBlockPath(t, archiveRoot, sharedBlock)); err != nil {
		t.Fatalf("shared archive block should remain referenced: %v", err)
	}
}

func TestExecuteSnapshotRetentionPersistsOperationJobStatus(t *testing.T) {
	root := t.TempDir()
	store := state.NewJSONStore(filepath.Join(root, "state.json"))
	alphaHash := sha256.Sum256([]byte("alpha"))
	alphaManifest := block.Manifest{Path: "alpha.txt", Size: 5, BlockSize: 5, Blocks: []block.Block{{Index: 0, Offset: 0, Size: 5, Hash: alphaHash[:]}}}
	if err := store.SaveManifest("docs", "alpha.txt", alphaManifest); err != nil {
		t.Fatalf("save alpha manifest: %v", err)
	}
	oldSummary, err := store.FolderSummary("docs")
	if err != nil {
		t.Fatalf("old folder summary: %v", err)
	}
	if err := store.SaveSnapshotMarker(state.SnapshotMarker{ID: "snap-old", FolderID: "docs", Cursor: oldSummary.Cursor, StateHash: oldSummary.StateHash, CreatedAt: "2026-05-24T10:00:00Z", Deprecated: true}); err != nil {
		t.Fatalf("save old marker: %v", err)
	}
	if err := store.SaveManifest("docs", "beta.txt", block.Manifest{Path: "beta.txt", Size: 0}); err != nil {
		t.Fatalf("save beta manifest: %v", err)
	}
	newSummary, err := store.FolderSummary("docs")
	if err != nil {
		t.Fatalf("new folder summary: %v", err)
	}
	if err := store.SaveSnapshotMarker(state.SnapshotMarker{ID: "snap-new", FolderID: "docs", Cursor: newSummary.Cursor, StateHash: newSummary.StateHash, CreatedAt: "2026-05-24T11:00:00Z"}); err != nil {
		t.Fatalf("save new marker: %v", err)
	}
	now := time.Date(2026, 5, 25, 12, 0, 0, 0, time.UTC)

	result, err := ExecuteSnapshotRetention(SnapshotRetentionOptions{Store: store, KeepLast: 1, JobID: "retention-job-1", Now: now})
	if err != nil {
		t.Fatalf("execute retention: %v", err)
	}
	if result.JobID != "retention-job-1" {
		t.Fatalf("result job id = %q", result.JobID)
	}
	job, ok, err := store.LoadBackupRetentionJob("retention-job-1")
	if err != nil || !ok {
		t.Fatalf("load retention job ok=%v err=%v", ok, err)
	}
	if job.Status != "completed" || job.KeepLast != 1 || job.DeprecatedSnapshots != 0 || job.DeletedSnapshots != 1 || job.PromotedManifests != 1 || job.RemainingOperations != 0 {
		t.Fatalf("unexpected retention job: %+v", job)
	}
	if job.StartedAt != now.Format(time.RFC3339) || job.CompletedAt != now.Format(time.RFC3339) || job.UpdatedAt != now.Format(time.RFC3339) {
		t.Fatalf("unexpected job timestamps: %+v", job)
	}
}

func TestExecuteSnapshotRetentionPromotesBeforeDeletingDeprecatedSnapshot(t *testing.T) {
	root := t.TempDir()
	store := state.NewJSONStore(filepath.Join(root, "state.json"))
	alphaHash := sha256.Sum256([]byte("alpha"))
	betaHash := sha256.Sum256([]byte("beta"))
	alphaManifest := block.Manifest{Path: "alpha.txt", Size: 5, BlockSize: 5, Blocks: []block.Block{{Index: 0, Offset: 0, Size: 5, Hash: alphaHash[:]}}}
	betaManifest := block.Manifest{Path: "beta.txt", Size: 4, BlockSize: 4, Blocks: []block.Block{{Index: 0, Offset: 0, Size: 4, Hash: betaHash[:]}}}
	if err := store.SaveManifest("docs", "alpha.txt", alphaManifest); err != nil {
		t.Fatalf("save alpha manifest: %v", err)
	}
	oldSummary, err := store.FolderSummary("docs")
	if err != nil {
		t.Fatalf("old folder summary: %v", err)
	}
	oldMarker := state.SnapshotMarker{ID: "snap-old", FolderID: "docs", Cursor: oldSummary.Cursor, StateHash: oldSummary.StateHash, CreatedAt: "2026-05-24T10:00:00Z", Deprecated: true}
	if err := store.SaveSnapshotMarker(oldMarker); err != nil {
		t.Fatalf("save old marker: %v", err)
	}
	if err := store.SaveManifest("docs", "beta.txt", betaManifest); err != nil {
		t.Fatalf("save beta manifest: %v", err)
	}
	newSummary, err := store.FolderSummary("docs")
	if err != nil {
		t.Fatalf("new folder summary: %v", err)
	}
	newMarker := state.SnapshotMarker{ID: "snap-new", FolderID: "docs", Cursor: newSummary.Cursor, StateHash: newSummary.StateHash, CreatedAt: "2026-05-24T11:00:00Z"}
	if err := store.SaveSnapshotMarker(newMarker); err != nil {
		t.Fatalf("save new marker: %v", err)
	}

	result, err := ExecuteSnapshotRetention(SnapshotRetentionOptions{Store: store, KeepLast: 1})
	if err != nil {
		t.Fatalf("execute retention: %v", err)
	}
	if got, want := len(result.Promotions), 1; got != want {
		t.Fatalf("promotions = %d, want %d: %+v", got, want, result)
	}
	if _, ok, err := store.LoadSnapshotMarker("snap-old"); err != nil || ok {
		t.Fatalf("old marker should be deleted after promotion: ok=%v err=%v", ok, err)
	}
	if _, ok, err := store.LoadSnapshotMarker("snap-new"); err != nil || !ok {
		t.Fatalf("new marker should remain: ok=%v err=%v", ok, err)
	}
	manifests, err := store.SnapshotManifests("snap-new")
	if err != nil {
		t.Fatalf("snapshot manifests after execute: %v", err)
	}
	if _, ok := manifests["alpha.txt"]; !ok {
		t.Fatalf("promoted inherited alpha manifest should remain available in next snapshot: %+v", manifests)
	}
	if _, ok := manifests["beta.txt"]; !ok {
		t.Fatalf("new beta manifest should remain available in next snapshot: %+v", manifests)
	}
}

func TestRunArchiveIntakeWorkerPublishesProgressUntilContextStops(t *testing.T) {
	root := t.TempDir()
	sourceRoot := filepath.Join(root, "source")
	archiveRoot := filepath.Join(root, "archive")
	if err := os.MkdirAll(sourceRoot, 0o755); err != nil {
		t.Fatalf("mkdir source: %v", err)
	}
	if err := os.WriteFile(filepath.Join(sourceRoot, "file.txt"), []byte("payload"), 0o644); err != nil {
		t.Fatalf("write source: %v", err)
	}
	hash := sha256.Sum256([]byte("payload"))
	store := state.NewJSONStore(filepath.Join(root, "state.json"))
	job := state.ArchiveIntakeJob{ID: "job", SnapshotID: "snap", FolderID: "docs", Path: "file.txt", Block: block.Block{Index: 0, Offset: 0, Size: 7, Hash: hash[:]}, Status: "pending", CreatedAt: time.Now().UTC().Format(time.RFC3339)}
	if err := store.SaveArchiveIntakeJobs("snap", []state.ArchiveIntakeJob{job}); err != nil {
		t.Fatalf("save job: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	progress := make(chan ArchiveIntakeWorkerResult, 1)
	errCh := make(chan error, 1)
	go func() {
		errCh <- RunArchiveIntakeWorker(ctx, ArchiveIntakeWorkerOptions{ArchiveRoot: archiveRoot, SourceRoots: map[string]string{"docs": sourceRoot}, Store: store, MaxJobs: 1}, time.Hour, func(result ArchiveIntakeWorkerResult, err error) {
			if err != nil {
				t.Errorf("worker callback error: %v", err)
			}
			progress <- result
			cancel()
		})
	}()

	select {
	case result := <-progress:
		if result.Archived != 1 || result.Remaining != 0 {
			t.Fatalf("unexpected progress result: %+v", result)
		}
	case <-time.After(2 * time.Second):
		t.Fatalf("timed out waiting for archive intake progress")
	}
	select {
	case err := <-errCh:
		if err != nil {
			t.Fatalf("worker returned error after context cancellation: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatalf("timed out waiting for archive intake worker to stop")
	}
}

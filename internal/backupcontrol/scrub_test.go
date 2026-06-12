package backupcontrol

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"testing"
	"time"

	"filesyncengine/internal/block"
	"filesyncengine/internal/config"
	"filesyncengine/internal/state"
)

func TestRunScrubReportsArchiveCheckpointAndRepairStatus(t *testing.T) {
	root := t.TempDir()
	folderPath := filepath.Join(root, "folder")
	archiveRoot := filepath.Join(root, "archive")
	checkpointRoot := filepath.Join(root, "checkpoints")
	if err := os.MkdirAll(folderPath, 0o700); err != nil {
		t.Fatal(err)
	}

	liveBytes := []byte("repairable")
	if err := os.WriteFile(filepath.Join(folderPath, "live.txt"), liveBytes, 0o600); err != nil {
		t.Fatal(err)
	}
	liveHash := sha256.Sum256(liveBytes)
	missingBytes := []byte("missing")
	missingHash := sha256.Sum256(missingBytes)

	store := state.NewJSONStore(filepath.Join(root, "state.json"))
	marker := state.SnapshotMarker{ID: "snap-001", FolderID: "docs", Cursor: 1, StateHash: "hash", CreatedAt: time.Now().UTC().Format(time.RFC3339Nano)}
	if err := store.SaveSnapshotMarker(marker); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveArchiveIntakeJobs(marker.ID, []state.ArchiveIntakeJob{
		{ID: "job-live", SnapshotID: marker.ID, FolderID: "docs", Path: "live.txt", Status: "archived", Block: block.Block{Offset: 0, Size: len(liveBytes), Hash: liveHash[:]}},
		{ID: "job-missing", SnapshotID: marker.ID, FolderID: "docs", Path: "missing.txt", Status: "archived", Block: block.Block{Offset: 0, Size: len(missingBytes), Hash: missingHash[:]}},
	}); err != nil {
		t.Fatal(err)
	}
	liveArchivePath := filepath.Join(archiveRoot, "blocks", hex.EncodeToString(liveHash[:])[:2], hex.EncodeToString(liveHash[:]))
	if err := os.MkdirAll(filepath.Dir(liveArchivePath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(liveArchivePath, liveBytes, 0o644); err != nil {
		t.Fatal(err)
	}

	result, err := RunScrub(config.Config{
		Backup:  config.BackupConfig{Enabled: true, Mode: config.BackupModeBlockArchiveOnly, ArchivePath: archiveRoot, CheckpointPath: checkpointRoot},
		Folders: []config.FolderConfig{{ID: "docs", Path: folderPath, Mode: config.ModeSendReceive}},
	}, filepath.Join(root, "config.json"), store)
	if err != nil {
		t.Fatalf("run backup scrub: %v", err)
	}
	if result.Archive.CheckedJobs != 2 || result.Archive.ProtectedBlocks != 1 || result.Archive.MissingBlocks != 1 {
		t.Fatalf("unexpected archive scrub result: %+v", result.Archive)
	}
	if result.Checkpoints.CheckedSnapshots != 1 || result.Checkpoints.MissingCheckpoints != 1 || result.Checkpoints.DegradedSnapshots != 1 {
		t.Fatalf("unexpected checkpoint scrub result: %+v", result.Checkpoints)
	}
	if result.RepairPlan.RepairableBlocks != 1 || result.RepairPlan.UnresolvedBlocks != 1 {
		t.Fatalf("unexpected repair plan result: %+v", result.RepairPlan)
	}
}

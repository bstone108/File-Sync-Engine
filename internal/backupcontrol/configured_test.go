package backupcontrol

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"filesyncengine/internal/block"
	"filesyncengine/internal/config"
	"filesyncengine/internal/state"
)

func TestRunConfiguredLoadsConfigAndConfiguredMetadataStore(t *testing.T) {
	root := t.TempDir()
	folderPath := filepath.Join(root, "folder")
	if err := os.MkdirAll(folderPath, 0o700); err != nil {
		t.Fatal(err)
	}

	configPath := filepath.Join(root, "config.json")
	metadataPath := filepath.Join(root, "metadata.json")
	archiveRoot := filepath.Join(root, "archive")
	checkpointRoot := filepath.Join(root, "checkpoints")
	cfg := config.Config{
		NodeName: "node-a",
		API:      config.APIConfig{Key: "test-key"},
		Backup:   config.BackupConfig{Enabled: true, Mode: config.BackupModeBlockArchiveOnly, ArchivePath: archiveRoot, CheckpointPath: checkpointRoot},
		Metadata: config.MetadataConfig{Backend: config.MetadataBackendJSON, Path: metadataPath},
		Folders:  []config.FolderConfig{{ID: "docs", Path: folderPath, Mode: config.ModeSendReceive}},
	}
	data, err := json.Marshal(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(configPath, data, 0o600); err != nil {
		t.Fatal(err)
	}

	store := state.NewJSONStore(metadataPath)
	marker := state.SnapshotMarker{ID: "snap-001", FolderID: "docs", Cursor: 1, StateHash: "hash", CreatedAt: time.Now().UTC().Format(time.RFC3339Nano)}
	if err := store.SaveSnapshotMarker(marker); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveArchiveIntakeJobs(marker.ID, []state.ArchiveIntakeJob{{ID: "job-missing", SnapshotID: marker.ID, FolderID: "docs", Path: "missing.txt", Status: "archived", Block: block.Block{Offset: 0, Size: 7, Hash: []byte("missing")}}}); err != nil {
		t.Fatal(err)
	}
	result, err := RunConfigured(configPath)
	if err != nil {
		t.Fatalf("run configured backup scrub: %v", err)
	}
	if result.Archive.CheckedJobs != 1 || result.Archive.MissingBlocks != 1 {
		t.Fatalf("unexpected archive scrub status from configured metadata store: %+v", result.Archive)
	}
	if result.Checkpoints.CheckedSnapshots != 1 || result.Checkpoints.MissingCheckpoints != 1 {
		t.Fatalf("unexpected checkpoint scrub status from configured metadata store: %+v", result.Checkpoints)
	}
}

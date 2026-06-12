package daemon

import (
	"os"
	"path/filepath"
	"testing"

	"filesyncengine/internal/block"
	"filesyncengine/internal/config"
	"filesyncengine/internal/state"
)

func TestRuntimeAppliesValidConfigReloadsWithoutStopping(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	first := `{"nodeName":"node-a","folders":[{"id":"docs","path":"./docs","mode":"sendrecv"}]}`
	second := `{"nodeName":"node-a","discovery":{"dht":true},"folders":[{"id":"docs","path":"./docs","mode":"sendonly"}]}`
	if err := os.WriteFile(path, []byte(first), 0o600); err != nil {
		t.Fatal(err)
	}
	mgr, err := config.NewManager(path)
	if err != nil {
		t.Fatal(err)
	}
	rt := NewRuntime(mgr)
	if rt.Generation() != 1 {
		t.Fatalf("initial generation = %d", rt.Generation())
	}
	if err := os.WriteFile(path, []byte(second), 0o600); err != nil {
		t.Fatal(err)
	}
	changed, err := rt.PollConfig()
	if err != nil {
		t.Fatalf("PollConfig: %v", err)
	}
	if !changed || rt.Generation() != 2 {
		t.Fatalf("reload not observed: changed=%v generation=%d", changed, rt.Generation())
	}
	if rt.Config().Folders[0].Mode != config.ModeSendOnly {
		t.Fatalf("runtime did not adopt new config")
	}
}

func TestReconcileReadySkippedDeletesAppliesOnlyAfterGatePrerequisites(t *testing.T) {
	root := t.TempDir()
	oldPath := filepath.Join(root, "old.txt")
	if err := os.WriteFile(oldPath, []byte("old"), 0o600); err != nil {
		t.Fatal(err)
	}

	store := state.NewJSONStore(filepath.Join(t.TempDir(), "state.json"))
	manifest := block.Manifest{Path: "new.txt", Size: 3, BlockSize: 3, Blocks: []block.Block{{Index: 0, Size: 3, Hash: []byte{0x01}}}}
	if err := store.SaveManifest("docs", "new.txt", manifest); err != nil {
		t.Fatalf("SaveManifest: %v", err)
	}
	summary, err := store.FolderSummary("docs")
	if err != nil {
		t.Fatalf("FolderSummary: %v", err)
	}
	if err := store.SavePendingWrite(state.PendingWrite{FolderID: "docs", Path: "new.txt", Manifest: manifest}); err != nil {
		t.Fatalf("SavePendingWrite: %v", err)
	}
	if err := store.SaveSkippedDelete(state.SkippedDelete{
		FolderID:                  "docs",
		Path:                      "old.txt",
		RequiredMetadataCursor:    summary.Cursor,
		RequiredMetadataStateHash: summary.StateHash,
		RequiredWrites:            []string{"new.txt"},
		Reason:                    "metadata_catchup_pending",
	}); err != nil {
		t.Fatalf("SaveSkippedDelete: %v", err)
	}

	blocked, err := ReconcileReadySkippedDeletes(root, "docs", store)
	if err != nil {
		t.Fatalf("ReconcileReadySkippedDeletes before write commit: %v", err)
	}
	if blocked.Deleted != 0 || blocked.Remaining != 1 {
		t.Fatalf("delete should remain gated before write commit: %+v", blocked)
	}
	if _, err := os.Stat(oldPath); err != nil {
		t.Fatalf("gated file was removed too early: %v", err)
	}

	if err := store.MarkPendingWriteCommitted("docs", "new.txt"); err != nil {
		t.Fatalf("MarkPendingWriteCommitted: %v", err)
	}
	applied, err := ReconcileReadySkippedDeletes(root, "docs", store)
	if err != nil {
		t.Fatalf("ReconcileReadySkippedDeletes after prerequisites: %v", err)
	}
	if applied.Deleted != 1 || applied.Remaining != 0 {
		t.Fatalf("ready skipped delete was not applied and cleared: %+v", applied)
	}
	if _, err := os.Stat(oldPath); !os.IsNotExist(err) {
		t.Fatalf("ready skipped delete did not remove stale file: %v", err)
	}

	again, err := ReconcileReadySkippedDeletes(root, "docs", store)
	if err != nil {
		t.Fatalf("ReconcileReadySkippedDeletes repeat: %v", err)
	}
	if again.Deleted != 0 || again.Remaining != 0 {
		t.Fatalf("cleared skipped delete should not be applied twice: %+v", again)
	}
}

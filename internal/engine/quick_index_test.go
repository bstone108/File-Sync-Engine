package engine

import (
	"os"
	"path/filepath"
	"testing"

	"filesyncengine/internal/block"
	"filesyncengine/internal/config"
	"filesyncengine/internal/scanner"
	"filesyncengine/internal/state"
)

func TestEngineQuickIndexStoresMetadataOnlyManifestsAndPrunesStaleEntries(t *testing.T) {
	root := t.TempDir()
	statePath := filepath.Join(t.TempDir(), "state.json")
	file := filepath.Join(root, "seed.txt")
	if err := os.WriteFile(file, []byte("seed-data"), 0o600); err != nil {
		t.Fatal(err)
	}
	store := state.NewJSONStore(statePath)
	eng := New(store)
	folder := config.FolderConfig{ID: "docs", Path: root, Mode: config.ModeSendReceive, BlockSize: 4096}

	result, err := eng.QuickIndex(folder)
	if err != nil {
		t.Fatalf("QuickIndex: %v", err)
	}
	if len(result.Changed) != 1 || result.Changed[0].Path != "seed.txt" || len(result.Changed[0].NeededBlocks) != 0 {
		t.Fatalf("quick index should report metadata change without needed blocks: %+v", result)
	}
	manifest, ok, err := store.LoadManifest("docs", "seed.txt")
	if err != nil || !ok {
		t.Fatalf("metadata manifest missing: ok=%v err=%v", ok, err)
	}
	if manifest.HashState != "unknown" || len(manifest.Blocks) != 0 || manifest.Size != int64(len("seed-data")) {
		t.Fatalf("stored manifest is not metadata-only: %+v", manifest)
	}

	if err := os.Remove(file); err != nil {
		t.Fatal(err)
	}
	second, err := eng.QuickIndex(folder)
	if err != nil {
		t.Fatalf("QuickIndex after delete: %v", err)
	}
	if len(second.Deleted) != 1 || second.Deleted[0] != "seed.txt" {
		t.Fatalf("expected stale metadata prune, got %+v", second.Deleted)
	}
}

func TestEngineQuickIndexPreservesVerifiedHashesWhenMetadataIsUnchanged(t *testing.T) {
	root := t.TempDir()
	statePath := filepath.Join(t.TempDir(), "state.json")
	file := filepath.Join(root, "seed.txt")
	if err := os.WriteFile(file, []byte("seed-data"), 0o600); err != nil {
		t.Fatal(err)
	}
	store := state.NewJSONStore(statePath)
	eng := New(store)
	folder := config.FolderConfig{ID: "docs", Path: root, Mode: config.ModeSendReceive, BlockSize: 4096}
	if _, err := eng.Scan(folder); err != nil {
		t.Fatalf("full scan: %v", err)
	}

	result, err := eng.QuickIndex(folder)
	if err != nil {
		t.Fatalf("quick index: %v", err)
	}
	if len(result.Changed) != 0 {
		t.Fatalf("unchanged verified file should not be reported changed: %+v", result.Changed)
	}
	manifest, ok, err := store.LoadManifest("docs", "seed.txt")
	if err != nil || !ok {
		t.Fatalf("stored manifest missing: ok=%v err=%v", ok, err)
	}
	if manifest.HashState != "complete" || len(manifest.Blocks) == 0 {
		t.Fatalf("quick index lost verified block hashes: %+v", manifest)
	}
}

func TestEngineQuickIndexPreservesPriorManifestWhenPathIsInaccessible(t *testing.T) {
	root := t.TempDir()
	statePath := filepath.Join(t.TempDir(), "state.json")
	store := state.NewJSONStore(statePath)
	eng := New(store)
	folder := config.FolderConfig{ID: "docs", Path: root, Mode: config.ModeSendReceive, BlockSize: 4096}
	previous := mustManifest(t, "seed-data")
	if err := store.SaveManifest("docs", "locked.txt", previous); err != nil {
		t.Fatalf("seed previous manifest: %v", err)
	}
	mustWriteSymlink(t, filepath.Join(root, "locked.txt"), "missing-target")
	if err := os.WriteFile(filepath.Join(root, "visible.txt"), []byte("visible"), 0o600); err != nil {
		t.Fatal(err)
	}

	result, err := eng.QuickIndex(folder)
	if err != nil {
		t.Fatalf("quick index: %v", err)
	}
	if len(result.Deleted) != 0 {
		t.Fatalf("inaccessible path should not be pruned as deleted: %+v", result.Deleted)
	}
	if len(result.Inaccessible) != 1 || result.Inaccessible[0].RelativePath != "locked.txt" {
		t.Fatalf("inaccessible paths = %+v", result.Inaccessible)
	}
	manifest, ok, err := store.LoadManifest("docs", "locked.txt")
	if err != nil || !ok {
		t.Fatalf("prior manifest missing after inaccessible scan: ok=%v err=%v", ok, err)
	}
	if manifest.Size != previous.Size || manifest.HashState != previous.HashState {
		t.Fatalf("prior manifest changed: got %+v want %+v", manifest, previous)
	}
}

func mustManifest(t *testing.T, content string) block.Manifest {
	t.Helper()
	path := filepath.Join(t.TempDir(), "manifest-source.txt")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	manifest, err := scanner.ScanFile(path, 4096)
	if err != nil {
		t.Fatalf("scan manifest source: %v", err)
	}
	return manifest
}

func mustWriteSymlink(t *testing.T, path string, target string) {
	t.Helper()
	if err := os.Symlink(target, path); err != nil {
		t.Skipf("symlink unavailable on this platform: %v", err)
	}
}

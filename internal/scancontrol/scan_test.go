package scancontrol

import (
	"os"
	"path/filepath"
	"testing"

	"filesyncengine/internal/config"
	"filesyncengine/internal/metadatastore"
	"filesyncengine/internal/state"
)

func TestRunQuickIndexScansSelectedFolderAndReportsStatePath(t *testing.T) {
	root := t.TempDir()
	folderPath := filepath.Join(root, "docs")
	if err := writeFile(filepath.Join(folderPath, "seed.txt"), []byte("seed-data")); err != nil {
		t.Fatal(err)
	}
	configPath := filepath.Join(root, "config.json")
	cfg := config.Config{
		Folders: []config.FolderConfig{{ID: "docs", Path: folderPath, Mode: config.ModeSendReceive, BlockSize: 4096}},
	}

	result, err := RunQuickIndex(cfg, configPath, "docs")
	if err != nil {
		t.Fatal(err)
	}
	if result.StatePath != metadatastore.DefaultStatePath(configPath) {
		t.Fatalf("state path = %q, want %q", result.StatePath, metadatastore.DefaultStatePath(configPath))
	}
	if len(result.Folders) != 1 || result.Folders[0].FolderID != "docs" || result.Folders[0].Changed != 1 || result.Folders[0].Deleted != 0 {
		t.Fatalf("unexpected scan result: %+v", result)
	}
	store := state.NewJSONStore(metadatastore.DefaultStatePath(configPath))
	manifest, ok, err := store.LoadManifest("docs", "seed.txt")
	if err != nil || !ok {
		t.Fatalf("stored manifest missing: ok=%v err=%v", ok, err)
	}
	if manifest.HashState != "unknown" || len(manifest.Blocks) != 0 {
		t.Fatalf("quick index should persist metadata-only manifest: %+v", manifest)
	}
}

func TestRunQuickIndexReturnsMissingFolderError(t *testing.T) {
	root := t.TempDir()
	cfg := config.Config{Folders: []config.FolderConfig{{ID: "docs", Path: filepath.Join(root, "docs"), Mode: config.ModeSendReceive, BlockSize: 4096}}}

	_, err := RunQuickIndex(cfg, filepath.Join(root, "config.json"), "missing")
	if err == nil || err.Error() != `folder "missing" not found` {
		t.Fatalf("error = %v, want missing folder error", err)
	}
}

func writeFile(path string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o600)
}

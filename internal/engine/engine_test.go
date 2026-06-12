package engine

import (
	"os"
	"path/filepath"
	"testing"

	"filesyncengine/internal/config"
	"filesyncengine/internal/state"
)

func TestEngineScanFolderRecordsNewAndChangedFiles(t *testing.T) {
	root := t.TempDir()
	statePath := filepath.Join(t.TempDir(), "state.json")
	file := filepath.Join(root, "alpha.txt")
	if err := os.WriteFile(file, []byte("abcdefgh"), 0o600); err != nil {
		t.Fatal(err)
	}
	eng := New(state.NewJSONStore(statePath))
	folder := config.FolderConfig{ID: "docs", Path: root, Mode: config.ModeSendReceive, BlockSize: 4}

	first, err := eng.Scan(folder)
	if err != nil {
		t.Fatalf("first scan: %v", err)
	}
	if len(first.Changed) != 1 || first.Changed[0].Path != "alpha.txt" || len(first.Changed[0].NeededBlocks) != 2 {
		t.Fatalf("unexpected first scan: %+v", first)
	}

	if err := os.WriteFile(file, []byte("abcdWXYZ"), 0o600); err != nil {
		t.Fatal(err)
	}
	second, err := eng.Scan(folder)
	if err != nil {
		t.Fatalf("second scan: %v", err)
	}
	if len(second.Changed) != 1 {
		t.Fatalf("changed files = %+v", second.Changed)
	}
	if len(second.Changed[0].NeededBlocks) != 1 || second.Changed[0].NeededBlocks[0].Index != 1 {
		t.Fatalf("expected only second block changed: %+v", second.Changed[0].NeededBlocks)
	}
}

func TestEngineScanReportsAndRemovesDeletedFilesFromStore(t *testing.T) {
	root := t.TempDir()
	statePath := filepath.Join(t.TempDir(), "state.json")
	file := filepath.Join(root, "alpha.txt")
	if err := os.WriteFile(file, []byte("abcdefgh"), 0o600); err != nil {
		t.Fatal(err)
	}
	store := state.NewJSONStore(statePath)
	eng := New(store)
	folder := config.FolderConfig{ID: "docs", Path: root, Mode: config.ModeSendReceive, BlockSize: 4}
	if _, err := eng.Scan(folder); err != nil {
		t.Fatalf("initial scan: %v", err)
	}
	if err := os.Remove(file); err != nil {
		t.Fatal(err)
	}

	second, err := eng.Scan(folder)
	if err != nil {
		t.Fatalf("delete scan: %v", err)
	}
	if len(second.Deleted) != 1 || second.Deleted[0] != "alpha.txt" {
		t.Fatalf("expected deleted alpha.txt, got %+v", second.Deleted)
	}
	if _, ok, err := store.LoadManifest("docs", "alpha.txt"); err != nil || ok {
		t.Fatalf("deleted manifest still in store: ok=%v err=%v", ok, err)
	}
}

package engine

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"filesyncengine/internal/config"
	"filesyncengine/internal/state"
)

func TestHashFileOnDemandCompletesUnknownManifestWhenMetadataStillMatches(t *testing.T) {
	root := t.TempDir()
	statePath := filepath.Join(t.TempDir(), "state.json")
	file := filepath.Join(root, "seed.txt")
	if err := os.WriteFile(file, []byte("seed-data"), 0o600); err != nil {
		t.Fatal(err)
	}
	store := state.NewJSONStore(statePath)
	eng := New(store)
	folder := config.FolderConfig{ID: "docs", Path: root, Mode: config.ModeSendReceive, BlockSize: 4}
	if _, err := eng.QuickIndex(folder); err != nil {
		t.Fatalf("quick index: %v", err)
	}

	manifest, err := eng.HashFileOnDemand(folder, "seed.txt")
	if err != nil {
		t.Fatalf("hash on demand: %v", err)
	}
	if manifest.HashState != "complete" || len(manifest.Blocks) != 3 {
		t.Fatalf("manifest was not fully hashed: %+v", manifest)
	}
	stored, ok, err := store.LoadManifest("docs", "seed.txt")
	if err != nil || !ok {
		t.Fatalf("stored manifest missing: ok=%v err=%v", ok, err)
	}
	if stored.HashState != "complete" || len(stored.Blocks) != 3 {
		t.Fatalf("stored manifest was not completed: %+v", stored)
	}
}

func TestHashFileOnDemandRefusesStaleBaselineAndKeepsUnknown(t *testing.T) {
	root := t.TempDir()
	statePath := filepath.Join(t.TempDir(), "state.json")
	file := filepath.Join(root, "seed.txt")
	if err := os.WriteFile(file, []byte("seed-data"), 0o600); err != nil {
		t.Fatal(err)
	}
	store := state.NewJSONStore(statePath)
	eng := New(store)
	folder := config.FolderConfig{ID: "docs", Path: root, Mode: config.ModeSendReceive, BlockSize: 4}
	if _, err := eng.QuickIndex(folder); err != nil {
		t.Fatalf("quick index: %v", err)
	}
	if err := os.WriteFile(file, []byte("changed-seed-data"), 0o600); err != nil {
		t.Fatal(err)
	}

	_, err := eng.HashFileOnDemand(folder, "seed.txt")
	if !errors.Is(err, ErrLazyHashBaselineChanged) {
		t.Fatalf("expected stale baseline error, got %v", err)
	}
	stored, ok, err := store.LoadManifest("docs", "seed.txt")
	if err != nil || !ok {
		t.Fatalf("stored manifest missing: ok=%v err=%v", ok, err)
	}
	if stored.HashState != "unknown" || len(stored.Blocks) != 0 || stored.Size != int64(len("changed-seed-data")) {
		t.Fatalf("stale baseline should be replaced with current unknown metadata, got %+v", stored)
	}
}

func TestHashNextUnknownProcessesUnknownFilesInStableOrder(t *testing.T) {
	root := t.TempDir()
	statePath := filepath.Join(t.TempDir(), "state.json")
	if err := os.WriteFile(filepath.Join(root, "b.txt"), []byte("bbbb"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "a.txt"), []byte("aaaa"), 0o600); err != nil {
		t.Fatal(err)
	}
	store := state.NewJSONStore(statePath)
	eng := New(store)
	folder := config.FolderConfig{ID: "docs", Path: root, Mode: config.ModeSendReceive, BlockSize: 2}
	if _, err := eng.QuickIndex(folder); err != nil {
		t.Fatalf("quick index: %v", err)
	}

	result, err := eng.HashNextUnknown(folder)
	if err != nil {
		t.Fatalf("hash next unknown: %v", err)
	}
	if !result.Hashed || result.Path != "a.txt" {
		t.Fatalf("expected stable first hash of a.txt, got %+v", result)
	}
	result, err = eng.HashNextUnknown(folder)
	if err != nil {
		t.Fatalf("hash next unknown second: %v", err)
	}
	if !result.Hashed || result.Path != "b.txt" {
		t.Fatalf("expected stable second hash of b.txt, got %+v", result)
	}
	result, err = eng.HashNextUnknown(folder)
	if err != nil {
		t.Fatalf("hash next unknown final: %v", err)
	}
	if result.Hashed || result.Path != "" {
		t.Fatalf("expected idle result after queue drained, got %+v", result)
	}
}

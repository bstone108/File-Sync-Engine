package engine

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"filesyncengine/internal/config"
	"filesyncengine/internal/scanner"
	"filesyncengine/internal/state"
)

func TestManualSeedAdoptionVerifiesUnchangedSeedAndAppliesAuthoritativeModTime(t *testing.T) {
	root := t.TempDir()
	statePath := filepath.Join(t.TempDir(), "state.json")
	file := filepath.Join(root, "seed.txt")
	if err := os.WriteFile(file, []byte("seed-data"), 0o600); err != nil {
		t.Fatal(err)
	}
	localTime := time.Unix(1700000000, 123)
	if err := os.Chtimes(file, localTime, localTime); err != nil {
		t.Fatal(err)
	}

	store := state.NewJSONStore(statePath)
	eng := New(store)
	folder := config.FolderConfig{ID: "docs", Path: root, Mode: config.ModeSendReceive, BlockSize: 4}
	if _, err := eng.QuickIndex(folder); err != nil {
		t.Fatalf("quick index: %v", err)
	}

	authoritative, err := scanner.ScanFile(file, folder.BlockSize)
	if err != nil {
		t.Fatalf("authoritative scan: %v", err)
	}
	authoritativeTime := time.Unix(1600000000, 456)
	authoritative.ModTimeUnixNano = authoritativeTime.UnixNano()
	authoritative.ChangeTimeUnixNano = 0

	result, err := eng.AdoptManualSeed(folder, map[string]scanner.File{
		"seed.txt": {RelativePath: "seed.txt", Manifest: authoritative},
	})
	if err != nil {
		t.Fatalf("adopt manual seed: %v", err)
	}
	if result.Adopted != 1 || result.Skipped != 0 {
		t.Fatalf("expected one adopted seed, got %+v", result)
	}
	stored, ok, err := store.LoadManifest("docs", "seed.txt")
	if err != nil || !ok {
		t.Fatalf("stored adopted manifest missing: ok=%v err=%v", ok, err)
	}
	if stored.HashState != "assumed-valid-unverified" || len(stored.Blocks) == 0 {
		t.Fatalf("expected unverified authoritative blocks, got %+v", stored)
	}
	if stored.SeedBaselineModTimeUnixNano != localTime.UnixNano() {
		t.Fatalf("expected local quick-index baseline to be preserved, got %+v", stored)
	}

	verified, err := eng.HashFileOnDemand(folder, "seed.txt")
	if err != nil {
		t.Fatalf("verify adopted seed: %v", err)
	}
	if verified.HashState != "complete" {
		t.Fatalf("expected verified complete manifest, got %+v", verified)
	}
	info, err := os.Stat(file)
	if err != nil {
		t.Fatal(err)
	}
	if info.ModTime().UnixNano() != authoritativeTime.UnixNano() {
		t.Fatalf("expected authoritative mod time %d, got %d", authoritativeTime.UnixNano(), info.ModTime().UnixNano())
	}
}

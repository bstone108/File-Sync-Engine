package metadataops

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"filesyncengine/internal/block"
	"filesyncengine/internal/config"
	"filesyncengine/internal/state"
)

func TestImportJSONCopiesStateIntoDurableStoreWithRollbackBackup(t *testing.T) {
	root := t.TempDir()
	sourcePath := filepath.Join(root, "source-state.json")
	sourceStore := state.NewJSONStore(sourcePath)
	if err := sourceStore.SaveManifest("docs", "imported.txt", block.Manifest{Path: "imported.txt", Size: 8, BlockSize: 4, HashState: "complete", Blocks: []block.Block{{Index: 0, Size: 4, Hash: []byte{1, 2, 3}}}}); err != nil {
		t.Fatal(err)
	}

	storePath := filepath.Join(root, "metadata.badger")
	existingStore, err := state.NewBadgerStore(storePath)
	if err != nil {
		t.Fatal(err)
	}
	if err := existingStore.SaveManifest("docs", "existing.txt", block.Manifest{Path: "existing.txt", Size: 3}); err != nil {
		t.Fatal(err)
	}
	if err := existingStore.Close(); err != nil {
		t.Fatal(err)
	}
	configPath := filepath.Join(root, "config.json")
	cfg := config.Config{
		NodeName: "test-node",
		API:      config.APIConfig{Listen: "127.0.0.1:0", Key: "test-key"},
		Metadata: config.MetadataConfig{Backend: config.MetadataBackendBadger, Path: storePath},
		Folders:  []config.FolderConfig{{ID: "docs", Path: filepath.Join(root, "docs"), Mode: config.ModeSendReceive, BlockSize: 4096}},
	}

	result, err := ImportJSON(ImportJSONOptions{SourcePath: sourcePath, Config: cfg, ConfigPath: configPath})
	if err != nil {
		t.Fatalf("ImportJSON: %v", err)
	}
	if result.ImportedManifests != 1 || result.BackupPath == "" || result.TargetPath != storePath || result.SourcePath != sourcePath {
		t.Fatalf("unexpected import result: %+v", result)
	}
	if _, err := os.Stat(result.BackupPath); err != nil {
		t.Fatalf("backup path missing: %v", err)
	}
	importedStore, err := state.NewBadgerStore(storePath)
	if err != nil {
		t.Fatal(err)
	}
	defer importedStore.Close()
	if _, ok, err := importedStore.LoadManifest("docs", "imported.txt"); err != nil || !ok {
		t.Fatalf("imported manifest missing: ok=%v err=%v", ok, err)
	}
	if _, ok, err := importedStore.LoadManifest("docs", "existing.txt"); err != nil || ok {
		t.Fatalf("old target state should be replaced after backup, ok=%v err=%v", ok, err)
	}
}

func TestSplitBadgerCopiesSingleStoreIntoPerFolderStores(t *testing.T) {
	root := t.TempDir()
	sourcePath := filepath.Join(root, "single.badger")
	sourceStore, err := state.NewBadgerStore(sourcePath)
	if err != nil {
		t.Fatal(err)
	}
	if err := sourceStore.SaveManifest("docs", "alpha.txt", block.Manifest{Path: "alpha.txt", Size: 5, BlockSize: 5, HashState: "complete", Blocks: []block.Block{{Index: 0, Size: 5, Hash: []byte{1, 2, 3}}}}); err != nil {
		t.Fatal(err)
	}
	if err := sourceStore.SaveManifest("media/lib", "song.flac", block.Manifest{Path: "song.flac", Size: 9, BlockSize: 9, HashState: "complete", Blocks: []block.Block{{Index: 0, Size: 9, Hash: []byte{4, 5, 6}}}}); err != nil {
		t.Fatal(err)
	}
	if err := sourceStore.Close(); err != nil {
		t.Fatal(err)
	}

	targetRoot := filepath.Join(root, "per-folder")
	existingStore, err := state.NewBadgerStore(filepath.Join(targetRoot, "docs.badger"))
	if err != nil {
		t.Fatal(err)
	}
	if err := existingStore.SaveManifest("docs", "old.txt", block.Manifest{Path: "old.txt", Size: 3}); err != nil {
		t.Fatal(err)
	}
	if err := existingStore.Close(); err != nil {
		t.Fatal(err)
	}
	cfg := config.Config{
		NodeName: "test-node",
		API:      config.APIConfig{Listen: "127.0.0.1:0", Key: "test-key"},
		Metadata: config.MetadataConfig{Backend: config.MetadataBackendBadger, Path: targetRoot, PerFolder: true},
		Folders: []config.FolderConfig{
			{ID: "docs", Path: filepath.Join(root, "docs"), Mode: config.ModeSendReceive, BlockSize: 4096},
			{ID: "media/lib", Path: filepath.Join(root, "media"), Mode: config.ModeSendReceive, BlockSize: 4096},
		},
	}
	configPath := filepath.Join(root, "config.json")

	result, err := SplitBadger(SplitBadgerOptions{SourcePath: sourcePath, Config: cfg, ConfigPath: configPath})
	if err != nil {
		t.Fatalf("SplitBadger: %v", err)
	}
	if result.ImportedManifests != 2 || result.Folders != 2 || result.BackupPath == "" {
		t.Fatalf("unexpected split result: %+v", result)
	}
	docsStore, err := state.NewBadgerStore(filepath.Join(targetRoot, "docs.badger"))
	if err != nil {
		t.Fatal(err)
	}
	defer docsStore.Close()
	if _, ok, err := docsStore.LoadManifest("docs", "alpha.txt"); err != nil || !ok {
		t.Fatalf("docs per-folder store missing migrated manifest: ok=%v err=%v", ok, err)
	}
	if _, ok, err := docsStore.LoadManifest("docs", "old.txt"); err != nil || ok {
		t.Fatalf("old target docs state should be replaced after backup, ok=%v err=%v", ok, err)
	}
	mediaStore, err := state.NewBadgerStore(filepath.Join(targetRoot, "media_lib.badger"))
	if err != nil {
		t.Fatal(err)
	}
	defer mediaStore.Close()
	if _, ok, err := mediaStore.LoadManifest("media/lib", "song.flac"); err != nil || !ok {
		t.Fatalf("media per-folder store missing migrated manifest: ok=%v err=%v", ok, err)
	}
	if _, ok, err := mediaStore.LoadManifest("docs", "alpha.txt"); err != nil || ok {
		t.Fatalf("media per-folder store should not contain docs manifest, ok=%v err=%v", ok, err)
	}
}

func TestSplitBadgerRejectsPerFolderTargetAsSource(t *testing.T) {
	root := t.TempDir()
	targetRoot := filepath.Join(root, "per-folder")
	cfg := config.Config{Metadata: config.MetadataConfig{Backend: config.MetadataBackendBadger, Path: targetRoot, PerFolder: true}}

	_, err := SplitBadger(SplitBadgerOptions{SourcePath: filepath.Clean(targetRoot), Config: cfg, ConfigPath: filepath.Join(root, "config.json")})
	if err == nil || err.Error() != fmt.Sprintf("metadata split-badger source must be a single Badger store, not the per-folder target root") {
		t.Fatalf("unexpected error: %v", err)
	}
}

package maintenancecontrol

import (
	"os"
	"path/filepath"
	"testing"

	"filesyncengine/internal/block"
	"filesyncengine/internal/config"
	"filesyncengine/internal/maintenance"
	"filesyncengine/internal/state"
)

func TestRunScrubSelectsFolderAndHonorsFolderMaintenanceOverrides(t *testing.T) {
	root := t.TempDir()
	folderPath := filepath.Join(root, "docs")
	if err := os.MkdirAll(folderPath, 0o700); err != nil {
		t.Fatal(err)
	}
	filePath := filepath.Join(folderPath, "data.txt")
	if err := os.WriteFile(filePath, []byte("expected-data"), 0o600); err != nil {
		t.Fatal(err)
	}
	manifest, err := block.BuildManifest(filePath, 4)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filePath, []byte("changed-data"), 0o600); err != nil {
		t.Fatal(err)
	}
	store := state.NewJSONStore(filepath.Join(root, "state.json"))
	if err := store.SaveManifest("docs", "data.txt", manifest); err != nil {
		t.Fatal(err)
	}
	cfg := config.Config{
		Maintenance: config.MaintenanceConfig{ScrubMode: config.MaintenanceScrubFullBlocks, MaxFilesPerRun: 9, MaxBytesPerRun: 9999},
		Folders: []config.FolderConfig{
			{ID: "docs", Path: folderPath, Mode: config.ModeSendReceive, Maintenance: config.MaintenanceConfig{ScrubMode: config.MaintenanceScrubLightMetadata, MaxFilesPerRun: 1, MaxBytesPerRun: 16}},
			{ID: "photos", Path: filepath.Join(root, "photos"), Mode: config.ModeSendReceive},
		},
	}

	results, err := RunScrub(cfg, store, filepath.Join(root, "state.json"), "docs")
	if err != nil {
		t.Fatalf("RunScrub: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected one selected folder result, got %+v", results)
	}
	result := results[0]
	if result.FolderID != "docs" || result.FilesScanned != 1 || result.Reported != 1 || result.Mode != maintenance.FileScrubLightMetadata || result.MaxFiles != 1 || result.MaxBytes != 16 {
		t.Fatalf("scrub did not honor selected folder config/mode/budget: %+v", result)
	}
	if len(result.Issues) != 1 || result.Issues[0].Kind != maintenance.FileScrubMetadataMismatch {
		t.Fatalf("manual scrub did not report bounded metadata issue: %+v", result.Issues)
	}
}

func TestRunScrubRejectsUnknownSelectedFolder(t *testing.T) {
	store := state.NewJSONStore(filepath.Join(t.TempDir(), "state.json"))
	_, err := RunScrub(config.Config{Folders: []config.FolderConfig{{ID: "docs", Path: t.TempDir()}}}, store, filepath.Join(t.TempDir(), "state.json"), "missing")
	if err == nil {
		t.Fatal("expected missing folder error")
	}
}

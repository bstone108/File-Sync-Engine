package snapshotcontrol

import (
	"path/filepath"
	"testing"
	"time"

	"filesyncengine/internal/backup"
	"filesyncengine/internal/block"
	"filesyncengine/internal/cli"
	"filesyncengine/internal/config"
	"filesyncengine/internal/state"
)

func TestArchiveAndCheckpointPathsResolveRelativeToConfig(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "config.jsonc")
	cfg := config.Config{Backup: config.BackupConfig{ArchivePath: "archive", CheckpointPath: "checkpoints"}}
	marker := state.SnapshotMarker{ID: "snap-001", FolderID: "docs"}

	if got, want := ArchivePath(cfg, configPath), filepath.Join(filepath.Dir(configPath), "archive"); got != want {
		t.Fatalf("ArchivePath() = %q, want %q", got, want)
	}
	if got, want := CheckpointRootPath(cfg, configPath), filepath.Join(filepath.Dir(configPath), "checkpoints"); got != want {
		t.Fatalf("CheckpointRootPath() = %q, want %q", got, want)
	}
	if got, want := CheckpointPath(cfg, marker, configPath), filepath.Join(filepath.Dir(configPath), "checkpoints", "docs", "snap-001.json"); got != want {
		t.Fatalf("CheckpointPath() = %q, want %q", got, want)
	}
}

func TestLoadAndUpdateSnapshotMarker(t *testing.T) {
	store := state.NewJSONStore(filepath.Join(t.TempDir(), "state.json"))
	marker := state.SnapshotMarker{ID: "snap-001", FolderID: "docs", Cursor: 7}
	if err := store.SaveSnapshotMarker(marker); err != nil {
		t.Fatalf("SaveSnapshotMarker: %v", err)
	}

	loaded, err := LoadMarker(store, "snap-001")
	if err != nil {
		t.Fatalf("LoadMarker: %v", err)
	}
	if loaded.Cursor != 7 {
		t.Fatalf("loaded cursor = %d, want 7", loaded.Cursor)
	}

	updated, err := UpdateMarker(store, "snap-001", func(marker *state.SnapshotMarker) { marker.Pinned = true })
	if err != nil {
		t.Fatalf("UpdateMarker: %v", err)
	}
	if !updated.Pinned {
		t.Fatalf("updated marker was not pinned")
	}
}

func TestRunMarkerActionHandlesLifecycleActions(t *testing.T) {
	root := t.TempDir()
	store := state.NewJSONStore(filepath.Join(root, "state.json"))
	cfg := config.Config{Folders: []config.FolderConfig{{ID: "docs", Path: filepath.Join(root, "docs"), Mode: config.ModeSendReceive}}}
	if err := store.SaveManifest("docs", "alpha.txt", block.Manifest{Path: "alpha.txt", Size: 1, BlockSize: 1, Blocks: []block.Block{{Index: 0, Size: 1}}}); err != nil {
		t.Fatalf("SaveManifest: %v", err)
	}

	created, err := RunMarkerAction(cli.ActionCreate, cfg, store, "docs", "nightly", filepath.Join(root, "config.jsonc"), time.Date(2026, 6, 3, 1, 2, 3, 4, time.UTC))
	if err != nil {
		t.Fatalf("RunMarkerAction create: %v", err)
	}
	if created.ID == "" || created.FolderID != "docs" || created.Description != "nightly" {
		t.Fatalf("created marker mismatch: %+v", created)
	}

	pinned, err := RunMarkerAction(cli.ActionPin, cfg, store, created.ID, "", filepath.Join(root, "config.jsonc"), time.Now())
	if err != nil {
		t.Fatalf("RunMarkerAction pin: %v", err)
	}
	if !pinned.Pinned {
		t.Fatalf("expected pinned marker, got %+v", pinned)
	}

	deleted, err := RunMarkerAction(cli.ActionDelete, cfg, store, created.ID, "", filepath.Join(root, "config.jsonc"), time.Now())
	if err != nil {
		t.Fatalf("RunMarkerAction delete: %v", err)
	}
	if deleted.ID != created.ID {
		t.Fatalf("deleted marker mismatch: %+v", deleted)
	}
	if _, ok, err := store.LoadSnapshotMarker(created.ID); err != nil || ok {
		t.Fatalf("marker still exists after delete: ok=%v err=%v", ok, err)
	}
}

func TestFolderPathLookup(t *testing.T) {
	cfg := config.Config{Folders: []config.FolderConfig{{ID: "docs", Path: "/shares/docs"}}}
	path, ok := FolderPath(cfg, "docs")
	if !ok || path != "/shares/docs" {
		t.Fatalf("FolderPath() = %q/%v, want /shares/docs/true", path, ok)
	}
	if FolderExists(cfg, "missing") {
		t.Fatalf("FolderExists() returned true for missing folder")
	}
}

func TestConfiguredSnapshotListLoadsConfigAndConfiguredStore(t *testing.T) {
	root := t.TempDir()
	cfg := config.Config{NodeName: "node-a"}
	store := state.NewJSONStore(filepath.Join(root, "state.json"))
	if err := store.SaveSnapshotMarker(state.SnapshotMarker{ID: "snap-docs", FolderID: "docs"}); err != nil {
		t.Fatalf("SaveSnapshotMarker docs: %v", err)
	}
	if err := store.SaveSnapshotMarker(state.SnapshotMarker{ID: "snap-photos", FolderID: "photos"}); err != nil {
		t.Fatalf("SaveSnapshotMarker photos: %v", err)
	}
	loadedConfigPath := ""
	openedConfigPath := ""
	openedCfg := config.Config{}

	markers, err := ListConfigured(ConfiguredOptions{
		ConfigPath: "config.jsonc",
		FolderID:   "docs",
		LoadConfig: func(path string) (config.Config, error) {
			loadedConfigPath = path
			return cfg, nil
		},
		OpenStore: func(openCfg config.Config, path string) (state.JSONStore, string, error) {
			openedCfg = openCfg
			openedConfigPath = path
			return store, filepath.Join(root, "state.json"), nil
		},
	})
	if err != nil {
		t.Fatalf("ListConfigured: %v", err)
	}
	if loadedConfigPath != "config.jsonc" || openedConfigPath != "config.jsonc" || openedCfg.NodeName != "node-a" {
		t.Fatalf("configured load/open mismatch: loaded=%q opened=%q cfg=%+v", loadedConfigPath, openedConfigPath, openedCfg)
	}
	if len(markers) != 1 || markers[0].ID != "snap-docs" {
		t.Fatalf("ListConfigured markers = %+v, want only docs marker", markers)
	}
}

func TestConfiguredSnapshotMarkerActionLoadsConfigAndConfiguredStore(t *testing.T) {
	root := t.TempDir()
	cfg := config.Config{Folders: []config.FolderConfig{{ID: "docs", Path: filepath.Join(root, "docs"), Mode: config.ModeSendReceive}}}
	store := state.NewJSONStore(filepath.Join(root, "state.json"))
	if err := store.SaveManifest("docs", "alpha.txt", block.Manifest{Path: "alpha.txt", Size: 1, BlockSize: 1, Blocks: []block.Block{{Index: 0, Size: 1}}}); err != nil {
		t.Fatalf("SaveManifest: %v", err)
	}
	loadedConfigPath := ""
	openedConfigPath := ""
	createdAt := time.Date(2026, 6, 4, 1, 2, 3, 4, time.UTC)

	marker, err := MarkerConfigured(MarkerConfiguredOptions{
		ConfigPath: "config.jsonc",
		Action:     cli.ActionCreate,
		ID:         "docs",
		Mode:       "nightly",
		Now:        func() time.Time { return createdAt },
		LoadConfig: func(path string) (config.Config, error) {
			loadedConfigPath = path
			return cfg, nil
		},
		OpenStore: func(openCfg config.Config, path string) (state.JSONStore, string, error) {
			if len(openCfg.Folders) != 1 || openCfg.Folders[0].ID != "docs" {
				t.Fatalf("OpenStore cfg = %+v, want loaded docs config", openCfg)
			}
			openedConfigPath = path
			return store, filepath.Join(root, "state.json"), nil
		},
	})
	if err != nil {
		t.Fatalf("MarkerConfigured: %v", err)
	}
	if loadedConfigPath != "config.jsonc" || openedConfigPath != "config.jsonc" {
		t.Fatalf("configured marker path mismatch: loaded=%q opened=%q", loadedConfigPath, openedConfigPath)
	}
	if marker.FolderID != "docs" || marker.Description != "nightly" || marker.CreatedAt != createdAt.Format(time.RFC3339Nano) {
		t.Fatalf("marker action inputs not preserved: %+v", marker)
	}
}

func TestConfiguredSnapshotRestorePlanLoadsConfigAndConfiguredStore(t *testing.T) {
	root := t.TempDir()
	cfg := config.Config{NodeName: "node-a"}
	store := state.NewJSONStore(filepath.Join(root, "state.json"))
	loadedConfigPath := ""
	openedConfigPath := ""
	plannedConfigPath := ""
	plannedSnapshotID := ""
	plannedPaths := []string{}
	plannedDestination := ""
	plannedAlternate := ""

	plan, err := PlanRestoreConfigured(RestoreConfiguredOptions{
		ConfigPath:      "config.jsonc",
		SnapshotID:      "snap-1",
		Paths:           []string{"docs/a.txt"},
		DestinationRoot: filepath.Join(root, "restore"),
		AlternatePath:   "restored-a.txt",
		LoadConfig: func(path string) (config.Config, error) {
			loadedConfigPath = path
			return cfg, nil
		},
		OpenStore: func(openCfg config.Config, path string) (state.JSONStore, string, error) {
			if openCfg.NodeName != "node-a" {
				t.Fatalf("OpenStore cfg = %+v, want loaded config", openCfg)
			}
			openedConfigPath = path
			return store, filepath.Join(root, "state.json"), nil
		},
		PlanRestore: func(planCfg config.Config, planStore state.JSONStore, configPath string, snapshotID string, paths []string, destinationRoot string, alternatePath string) (backup.RestorePlan, error) {
			if planCfg.NodeName != "node-a" {
				t.Fatalf("PlanRestore cfg = %+v, want loaded config", planCfg)
			}
			plannedConfigPath = configPath
			plannedSnapshotID = snapshotID
			plannedPaths = append(plannedPaths, paths...)
			plannedDestination = destinationRoot
			plannedAlternate = alternatePath
			return backup.RestorePlan{SnapshotID: snapshotID}, nil
		},
	})
	if err != nil {
		t.Fatalf("PlanRestoreConfigured: %v", err)
	}
	if loadedConfigPath != "config.jsonc" || openedConfigPath != "config.jsonc" || plannedConfigPath != "config.jsonc" {
		t.Fatalf("configured path mismatch: loaded=%q opened=%q planned=%q", loadedConfigPath, openedConfigPath, plannedConfigPath)
	}
	if plan.SnapshotID != "snap-1" || plannedSnapshotID != "snap-1" || len(plannedPaths) != 1 || plannedPaths[0] != "docs/a.txt" || plannedDestination == "" || plannedAlternate != "restored-a.txt" {
		t.Fatalf("restore plan inputs not preserved: plan=%+v snapshot=%q paths=%v destination=%q alternate=%q", plan, plannedSnapshotID, plannedPaths, plannedDestination, plannedAlternate)
	}
}

func TestConfiguredSnapshotRetentionUsesArchiveRootFromConfig(t *testing.T) {
	root := t.TempDir()
	archiveRoot := filepath.Join(root, "archive")
	cfg := config.Config{Backup: config.BackupConfig{ArchivePath: archiveRoot}}
	store := state.NewJSONStore(filepath.Join(root, "state.json"))
	plannedArchiveRoot := ""
	plannedKeepLast := 0

	plan, err := RetentionConfigured(RetentionConfiguredOptions{
		ConfigPath: "config.jsonc",
		KeepLast:   3,
		LoadConfig: func(path string) (config.Config, error) {
			return cfg, nil
		},
		OpenStore: func(openCfg config.Config, path string) (state.JSONStore, string, error) {
			return store, filepath.Join(root, "state.json"), nil
		},
		ExecuteRetention: func(opts backup.SnapshotRetentionOptions) (backup.SnapshotRetentionPlan, error) {
			plannedArchiveRoot = opts.ArchiveRoot
			plannedKeepLast = opts.KeepLast
			return backup.SnapshotRetentionPlan{KeepLast: opts.KeepLast}, nil
		},
	})
	if err != nil {
		t.Fatalf("RetentionConfigured: %v", err)
	}
	if plan.KeepLast != 3 || plannedKeepLast != 3 || plannedArchiveRoot != archiveRoot {
		t.Fatalf("retention inputs not preserved: plan=%+v keep=%d archive=%q", plan, plannedKeepLast, plannedArchiveRoot)
	}
}

func TestCreateMarkerPersistsBackupArchiveJobs(t *testing.T) {
	root := t.TempDir()
	folderPath := filepath.Join(root, "folder")
	configPath := filepath.Join(root, "config.json")
	store := state.NewJSONStore(filepath.Join(root, "state.json"))
	manifest := block.Manifest{
		Path:      "alpha.txt",
		Size:      2,
		BlockSize: 1,
		HashState: "complete",
		Blocks: []block.Block{
			{Index: 0, Offset: 0, Size: 1, Hash: []byte("a")},
			{Index: 1, Offset: 1, Size: 1, Hash: []byte("b")},
		},
	}
	if err := store.SaveManifest("docs", "alpha.txt", manifest); err != nil {
		t.Fatalf("SaveManifest: %v", err)
	}
	cfg := config.Config{
		Backup: config.BackupConfig{Enabled: true, Mode: config.BackupModeBlockArchiveOnly},
		Folders: []config.FolderConfig{
			{ID: "docs", Path: folderPath, Mode: config.ModeSendReceive, BlockSize: 4096},
		},
	}
	createdAt := time.Date(2026, 6, 2, 3, 4, 5, 6, time.UTC)

	marker, err := CreateMarker(cfg, store, "docs", "nightly", configPath, createdAt)
	if err != nil {
		t.Fatalf("CreateMarker: %v", err)
	}

	if marker.FolderID != "docs" || marker.Description != "nightly" || marker.CreatedAt != createdAt.Format(time.RFC3339Nano) {
		t.Fatalf("marker metadata mismatch: %+v", marker)
	}
	jobs, err := store.ListArchiveIntakeJobs(marker.ID)
	if err != nil {
		t.Fatalf("ListArchiveIntakeJobs: %v", err)
	}
	if len(jobs) != 2 {
		t.Fatalf("expected one archive job per snapshot block, got %+v", jobs)
	}
	if jobs[0].SnapshotID != marker.ID || jobs[0].FolderID != "docs" || jobs[0].Path != "alpha.txt" || string(jobs[0].Block.Hash) != "a" || jobs[0].Status != "pending" || jobs[0].CreatedAt != createdAt.Format(time.RFC3339Nano) {
		t.Fatalf("archive job missing deterministic snapshot/block metadata: %+v", jobs[0])
	}
}

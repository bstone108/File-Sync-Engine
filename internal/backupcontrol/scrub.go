package backupcontrol

import (
	"path/filepath"
	"time"

	"filesyncengine/internal/api"
	"filesyncengine/internal/apicontrol"
	"filesyncengine/internal/backup"
	"filesyncengine/internal/config"
	"filesyncengine/internal/metadatastore"
	"filesyncengine/internal/state"
)

// RunConfigured loads the daemon config, opens the configured metadata store,
// runs the backup scrub, and closes the store. CLI/API boundaries should use
// this rather than reimplementing config/store orchestration in cmd/fse.
func RunConfigured(configPath string) (api.BackupScrubResponse, error) {
	cfg, err := config.LoadFile(configPath)
	if err != nil {
		return api.BackupScrubResponse{}, err
	}
	store, _, err := metadatastore.Open(cfg, configPath)
	if err != nil {
		return api.BackupScrubResponse{}, err
	}
	defer store.Close()
	return RunScrub(cfg, configPath, store)
}

// RunScrub runs the configured report-only backup scrub and returns the compact
// API/CLI status shape. It centralizes backup scrub control flow outside the
// cmd/fse entrypoint so daemon handlers and CLI wrappers share one tested path.
func RunScrub(cfg config.Config, configPath string, store state.JSONStore) (api.BackupScrubResponse, error) {
	started := time.Now().UTC()
	archiveRoot := archivePath(cfg, configPath)
	checkpointRoot := checkpointPath(cfg, configPath)
	archiveResult := backup.BackupArchiveScrubResult{}
	if archiveRoot != "" {
		var err error
		archiveResult, err = backup.ScrubBackupArchive(backup.BackupArchiveScrubOptions{ArchiveRoot: archiveRoot, Store: store})
		if err != nil {
			return api.BackupScrubResponse{}, err
		}
	}
	checkpointResult := backup.BackupCheckpointScrubResult{}
	if checkpointRoot != "" {
		var err error
		checkpointResult, err = backup.ScrubBackupCheckpoints(backup.BackupCheckpointScrubOptions{ArchiveRoot: archiveRoot, CheckpointRoot: checkpointRoot, Store: store})
		if err != nil {
			return api.BackupScrubResponse{}, err
		}
	}
	repairPlan, err := backup.PlanBackupArchiveRepair(backup.BackupArchiveRepairPlanOptions{
		SourceRoots: sourceRoots(cfg),
		Store:       store,
	})
	if err != nil {
		return api.BackupScrubResponse{}, err
	}
	return api.BackupScrubResponse{
		StartedAt:   started,
		FinishedAt:  time.Now().UTC(),
		Archive:     apicontrol.BackupArchiveScrubState(archiveResult),
		Checkpoints: apicontrol.BackupCheckpointScrubState(checkpointResult),
		RepairPlan:  apicontrol.BackupRepairPlanState(repairPlan),
	}, nil
}

func sourceRoots(cfg config.Config) map[string]string {
	roots := map[string]string{}
	for _, folder := range cfg.Folders {
		roots[folder.ID] = folder.Path
	}
	return roots
}

func archivePath(cfg config.Config, configPath string) string {
	return configRelativePath(cfg.Backup.ArchivePath, configPath)
}

func checkpointPath(cfg config.Config, configPath string) string {
	return configRelativePath(cfg.Backup.CheckpointPath, configPath)
}

func configRelativePath(path string, configPath string) string {
	if path != "" && !filepath.IsAbs(path) {
		return filepath.Join(filepath.Dir(configPath), path)
	}
	return path
}

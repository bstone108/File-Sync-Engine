package maintenancecontrol

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"filesyncengine/internal/config"
	"filesyncengine/internal/maintenance"
	"filesyncengine/internal/state"
)

type ScrubResult struct {
	FolderID     string
	Mode         maintenance.FileScrubVerifyMode
	MaxFiles     int
	MaxBytes     int64
	Issues       []maintenance.FileScrubIssue
	Run          maintenance.RunResult
	FilesScanned int
	BytesScanned int64
	Reported     int
	Quarantined  int
	Complete     bool
	Yielded      bool
	Cursor       maintenance.Cursor
}

func RunScrub(cfg config.Config, store state.JSONStore, storePath string, folderID string) ([]ScrubResult, error) {
	results := []ScrubResult{}
	matched := 0
	for _, folder := range cfg.Folders {
		if folderID != "" && folder.ID != folderID {
			continue
		}
		matched++
		maintenanceCfg := EffectiveFolderMaintenance(cfg.Maintenance, folder.Maintenance)
		mode := ScrubMode(maintenanceCfg.ScrubMode)
		issues := []maintenance.FileScrubIssue{}
		crawler := maintenance.FileScrubCrawler{
			Store:              store,
			FolderIDs:          []string{folder.ID},
			Roots:              map[string]string{folder.ID: folder.Path},
			VerifyMode:         mode,
			SampleEveryNBlocks: maintenanceCfg.SampleEveryNBlocks,
			Report: func(issue maintenance.FileScrubIssue) {
				issues = append(issues, issue)
			},
		}
		runResult, err := maintenance.RunOnce(context.Background(), maintenance.RunOptions{
			Crawler:    crawler,
			Checkpoint: maintenance.FileCheckpoint{Path: ScrubCheckpointPath(storePath, folder.ID)},
			MaxFiles:   maintenanceCfg.MaxFilesPerRun,
			MaxBytes:   maintenanceCfg.MaxBytesPerRun,
		})
		if err != nil {
			return nil, fmt.Errorf("maintenance scrub %s: %w", folder.ID, err)
		}
		results = append(results, ScrubResult{
			FolderID:     folder.ID,
			Mode:         mode,
			MaxFiles:     maintenanceCfg.MaxFilesPerRun,
			MaxBytes:     maintenanceCfg.MaxBytesPerRun,
			Issues:       issues,
			Run:          runResult,
			FilesScanned: runResult.FilesScanned,
			BytesScanned: runResult.BytesScanned,
			Reported:     runResult.Reported,
			Quarantined:  runResult.Quarantined,
			Complete:     runResult.Complete,
			Yielded:      runResult.Yielded,
			Cursor:       runResult.Cursor,
		})
	}
	if folderID != "" && matched == 0 {
		return nil, fmt.Errorf("folder %q not found", folderID)
	}
	return results, nil
}

func EffectiveFolderMaintenance(global config.MaintenanceConfig, folder config.MaintenanceConfig) config.MaintenanceConfig {
	merged := global
	if folder.Enabled {
		merged.Enabled = true
	}
	if folder.Frequency != "" {
		merged.Frequency = folder.Frequency
	}
	if folder.IdleOnly {
		merged.IdleOnly = true
	}
	if folder.MaxFilesPerRun > 0 {
		merged.MaxFilesPerRun = folder.MaxFilesPerRun
	}
	if folder.MaxBytesPerRun > 0 {
		merged.MaxBytesPerRun = folder.MaxBytesPerRun
	}
	if folder.MaxFilesPerDay > 0 {
		merged.MaxFilesPerDay = folder.MaxFilesPerDay
	}
	if folder.MaxBytesPerDay > 0 {
		merged.MaxBytesPerDay = folder.MaxBytesPerDay
	}
	if folder.ScrubMode != "" {
		merged.ScrubMode = folder.ScrubMode
	}
	if folder.SampleEveryNBlocks > 0 {
		merged.SampleEveryNBlocks = folder.SampleEveryNBlocks
	}
	if folder.AutoRepair {
		merged.AutoRepair = true
	}
	return merged
}

func ScrubMode(mode config.MaintenanceScrubMode) maintenance.FileScrubVerifyMode {
	switch mode {
	case config.MaintenanceScrubLightMetadata:
		return maintenance.FileScrubLightMetadata
	case config.MaintenanceScrubSampledBlocks:
		return maintenance.FileScrubSampledBlocks
	default:
		return maintenance.FileScrubFullBlocks
	}
}

func ScrubCheckpointPath(storePath string, folderID string) string {
	safeFolder := strings.NewReplacer(string(os.PathSeparator), "_", "/", "_", "\\", "_", ":", "_").Replace(folderID)
	return filepath.Join(storePath+".maintenance", "scrub-"+safeFolder+".cursor.json")
}

package snapshotcontrol

import (
	"fmt"
	"path/filepath"
	"time"

	"filesyncengine/internal/backup"
	"filesyncengine/internal/block"
	"filesyncengine/internal/cli"
	"filesyncengine/internal/config"
	"filesyncengine/internal/state"
)

// FolderPath returns the configured root path for a folder ID.
func FolderPath(cfg config.Config, folderID string) (string, bool) {
	for _, folder := range cfg.Folders {
		if folder.ID == folderID {
			return folder.Path, true
		}
	}
	return "", false
}

// FolderExists reports whether a folder ID exists in the active config.
func FolderExists(cfg config.Config, folderID string) bool {
	_, ok := FolderPath(cfg, folderID)
	return ok
}

// LoadMarker loads a snapshot marker and returns a concise not-found error.
func LoadMarker(store state.JSONStore, id string) (state.SnapshotMarker, error) {
	marker, ok, err := store.LoadSnapshotMarker(id)
	if err != nil {
		return state.SnapshotMarker{}, err
	}
	if !ok {
		return state.SnapshotMarker{}, fmt.Errorf("snapshot %q not found", id)
	}
	return marker, nil
}

// UpdateMarker loads, mutates, and persists one snapshot marker.
func UpdateMarker(store state.JSONStore, id string, update func(*state.SnapshotMarker)) (state.SnapshotMarker, error) {
	marker, err := LoadMarker(store, id)
	if err != nil {
		return state.SnapshotMarker{}, err
	}
	update(&marker)
	return marker, store.SaveSnapshotMarker(marker)
}

// CheckpointRootPath resolves the configured checkpoint root, preserving an empty disabled path.
func CheckpointRootPath(cfg config.Config, configPath string) string {
	root := cfg.Backup.CheckpointPath
	if root != "" && !filepath.IsAbs(root) {
		root = filepath.Join(filepath.Dir(configPath), root)
	}
	return root
}

// CheckpointPath resolves the configured snapshot checkpoint path.
func CheckpointPath(cfg config.Config, marker state.SnapshotMarker, configPath string) string {
	return filepath.Join(CheckpointRootPath(cfg, configPath), marker.FolderID, marker.ID+".json")
}

// ArchivePath resolves the configured archive root, preserving an empty disabled path.
func ArchivePath(cfg config.Config, configPath string) string {
	root := cfg.Backup.ArchivePath
	if root != "" && !filepath.IsAbs(root) {
		root = filepath.Join(filepath.Dir(configPath), root)
	}
	return root
}

// CreateMarker creates a snapshot marker and runs the configured backup-side marker work.
func CreateMarker(cfg config.Config, store state.JSONStore, folderID string, description string, configPath string, createdAt time.Time) (state.SnapshotMarker, error) {
	if !FolderExists(cfg, folderID) {
		return state.SnapshotMarker{}, fmt.Errorf("folder %q not found", folderID)
	}
	summary, err := store.FolderSummary(folderID)
	if err != nil {
		return state.SnapshotMarker{}, err
	}
	now := createdAt.UTC()
	marker := state.SnapshotMarker{ID: fmt.Sprintf("snap-%s", now.Format("20060102T150405.000000000Z")), FolderID: folderID, Cursor: summary.Cursor, StateHash: summary.StateHash, CreatedAt: now.Format(time.RFC3339Nano), Description: description}
	if err := store.SaveSnapshotMarker(marker); err != nil {
		return state.SnapshotMarker{}, err
	}
	if cfg.Backup.Enabled {
		if err := PersistArchiveIntakeJobs(cfg, store, marker, now, configPath); err != nil {
			return state.SnapshotMarker{}, err
		}
		if cfg.Backup.CheckpointPath != "" {
			if err := store.ExportCheckpoint(CheckpointPath(cfg, marker, configPath)); err != nil {
				return state.SnapshotMarker{}, err
			}
		}
	}
	return marker, nil
}

// RunMarkerAction executes the snapshot marker lifecycle actions shared by CLI and API callers.
func RunMarkerAction(action cli.Action, cfg config.Config, store state.JSONStore, id string, description string, configPath string, now time.Time) (state.SnapshotMarker, error) {
	switch action {
	case cli.ActionCreate:
		return CreateMarker(cfg, store, id, description, configPath, now)
	case cli.ActionShow:
		return LoadMarker(store, id)
	case cli.ActionPin:
		return UpdateMarker(store, id, func(marker *state.SnapshotMarker) { marker.Pinned = true })
	case cli.ActionDeprecate:
		return UpdateMarker(store, id, func(marker *state.SnapshotMarker) { marker.Deprecated = true })
	case cli.ActionDelete:
		marker, err := LoadMarker(store, id)
		if err != nil {
			return state.SnapshotMarker{}, err
		}
		return marker, store.DeleteSnapshotMarker(id)
	default:
		return state.SnapshotMarker{}, fmt.Errorf("snapshot action %s not implemented", action)
	}
}

// PersistArchiveIntakeJobs plans and persists backup archive/mirror work for a snapshot marker.
func PersistArchiveIntakeJobs(cfg config.Config, store state.JSONStore, marker state.SnapshotMarker, createdAt time.Time, configPath string) error {
	snapshotManifestsByPath, err := store.SnapshotManifests(marker.ID)
	if err != nil {
		return err
	}
	currentManifests, err := store.ListManifests(marker.FolderID)
	if err != nil {
		return err
	}
	snapshotManifests := make([]block.Manifest, 0, len(snapshotManifestsByPath))
	for _, manifest := range snapshotManifestsByPath {
		snapshotManifests = append(snapshotManifests, manifest)
	}
	mode := cfg.Backup.Mode
	if mode == "" {
		mode = config.BackupModeBlockArchiveOnly
	}
	plan, err := backup.PlanDestinationMode(mode, snapshotManifests, currentManifests)
	if err != nil {
		return err
	}
	if cfg.Backup.MirrorPath != "" && len(plan.MirrorFiles) > 0 {
		folderPath, ok := FolderPath(cfg, marker.FolderID)
		if !ok {
			return fmt.Errorf("folder %q not found", marker.FolderID)
		}
		if _, err := backup.ExecuteMirrorUpdate(folderPath, filepath.Join(cfg.Backup.MirrorPath, marker.FolderID), plan); err != nil {
			return err
		}
	}
	jobs := make([]state.ArchiveIntakeJob, 0, len(plan.ArchiveBlocks))
	created := createdAt.UTC().Format(time.RFC3339Nano)
	for _, archiveBlock := range plan.ArchiveBlocks {
		jobs = append(jobs, state.ArchiveIntakeJob{
			ID:         fmt.Sprintf("%s-%06d", marker.ID, len(jobs)),
			SnapshotID: marker.ID,
			FolderID:   marker.FolderID,
			Path:       archiveBlock.Path,
			Block:      archiveBlock.Block,
			Status:     "pending",
			CreatedAt:  created,
		})
	}
	if err := store.SaveArchiveIntakeJobs(marker.ID, jobs); err != nil {
		return err
	}
	archiveRoot := ArchivePath(cfg, configPath)
	if archiveRoot == "" || len(jobs) == 0 {
		return nil
	}
	folderPath, ok := FolderPath(cfg, marker.FolderID)
	if !ok {
		return fmt.Errorf("folder %q not found", marker.FolderID)
	}
	_, err = backup.ProcessArchiveIntakeJobs(folderPath, archiveRoot, store, marker.ID)
	return err
}

// PlanRestore builds a dry-run snapshot restore plan from configured backup paths.
func PlanRestore(cfg config.Config, store state.JSONStore, configPath string, snapshotID string, paths []string, destinationRoot string, alternatePath string) (backup.RestorePlan, error) {
	if snapshotID == "" {
		return backup.RestorePlan{}, fmt.Errorf("snapshot id is required")
	}
	archiveRoot := ArchivePath(cfg, configPath)
	if archiveRoot == "" {
		return backup.RestorePlan{}, fmt.Errorf("backup archivePath is required for restore planning")
	}
	options, err := restoreOptions(cfg, store, configPath, snapshotID, paths, destinationRoot, alternatePath, archiveRoot, true)
	if err != nil {
		return backup.RestorePlan{}, err
	}
	return backup.PlanSnapshotRestore(options)
}

// ExecuteRestore restores files from verified archive blocks using configured backup paths.
func ExecuteRestore(cfg config.Config, store state.JSONStore, configPath string, snapshotID string, paths []string, destinationRoot string, alternatePath string) (backup.RestoreResult, error) {
	if snapshotID == "" {
		return backup.RestoreResult{}, fmt.Errorf("snapshot id is required")
	}
	archiveRoot := ArchivePath(cfg, configPath)
	if archiveRoot == "" {
		return backup.RestoreResult{}, fmt.Errorf("backup archivePath is required for restore")
	}
	options, err := restoreOptions(cfg, store, configPath, snapshotID, paths, destinationRoot, alternatePath, archiveRoot, false)
	if err != nil {
		return backup.RestoreResult{}, err
	}
	return backup.ExecuteSnapshotRestore(options)
}

func restoreOptions(cfg config.Config, store state.JSONStore, configPath string, snapshotID string, paths []string, destinationRoot string, alternatePath string, archiveRoot string, dryRun bool) (backup.RestorePlanOptions, error) {
	originalRoot := ""
	if marker, ok, err := store.LoadSnapshotMarker(snapshotID); err != nil {
		return backup.RestorePlanOptions{}, err
	} else if ok {
		if folderPath, found := FolderPath(cfg, marker.FolderID); found {
			originalRoot = folderPath
		}
	}
	return backup.RestorePlanOptions{Store: store, ArchiveRoot: archiveRoot, SnapshotID: snapshotID, Paths: paths, DestinationRoot: destinationRoot, OriginalRoot: originalRoot, AlternatePath: alternatePath, DryRun: dryRun}, nil
}

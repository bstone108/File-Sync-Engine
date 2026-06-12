package metadataops

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"filesyncengine/internal/config"
	"filesyncengine/internal/metadatastore"
	"filesyncengine/internal/state"
)

type Result struct {
	SourcePath        string
	TargetPath        string
	BackupPath        string
	Folders           int
	ImportedManifests int
}

type ImportJSONOptions struct {
	SourcePath string
	Config     config.Config
	ConfigPath string
}

func ImportJSON(opts ImportJSONOptions) (Result, error) {
	if opts.SourcePath == "" {
		return Result{}, fmt.Errorf("metadata import-json --source is required")
	}
	backend := metadatastore.EffectiveBackend(opts.Config)
	if backend == config.MetadataBackendJSON {
		return Result{}, fmt.Errorf("metadata import-json requires a durable target backend, got %q", backend)
	}
	targetPath := metadatastore.ConfiguredStorePath(opts.Config, opts.ConfigPath)
	backupPath, err := BackupExistingStore(targetPath)
	if err != nil {
		return Result{}, err
	}
	store, _, err := metadatastore.Open(opts.Config, opts.ConfigPath)
	if err != nil {
		RestoreBackup(targetPath, backupPath)
		return Result{}, err
	}
	importResult, importErr := state.ImportJSONSnapshot(opts.SourcePath, store)
	closeErr := store.Close()
	if importErr != nil {
		RestoreBackup(targetPath, backupPath)
		return Result{}, importErr
	}
	if closeErr != nil {
		RestoreBackup(targetPath, backupPath)
		return Result{}, closeErr
	}
	return Result{SourcePath: opts.SourcePath, TargetPath: targetPath, BackupPath: backupPath, Folders: importResult.Folders, ImportedManifests: importResult.ImportedManifests}, nil
}

type SplitBadgerOptions struct {
	SourcePath string
	Config     config.Config
	ConfigPath string
}

func SplitBadger(opts SplitBadgerOptions) (Result, error) {
	if opts.SourcePath == "" {
		return Result{}, fmt.Errorf("metadata split-badger --source is required")
	}
	if metadatastore.EffectiveBackend(opts.Config) != config.MetadataBackendBadger || !opts.Config.Metadata.PerFolder {
		return Result{}, fmt.Errorf("metadata split-badger requires metadata.backend %q with metadata.perFolder true", config.MetadataBackendBadger)
	}
	targetPath := metadatastore.ConfiguredStorePath(opts.Config, opts.ConfigPath)
	if filepath.Clean(opts.SourcePath) == filepath.Clean(targetPath) {
		return Result{}, fmt.Errorf("metadata split-badger source must be a single Badger store, not the per-folder target root")
	}
	backupPath, err := BackupExistingStore(targetPath)
	if err != nil {
		return Result{}, err
	}
	source, err := state.NewBadgerStore(opts.SourcePath)
	if err != nil {
		RestoreBackup(targetPath, backupPath)
		return Result{}, err
	}
	defer source.Close()
	destination, _, err := metadatastore.Open(opts.Config, opts.ConfigPath)
	if err != nil {
		RestoreBackup(targetPath, backupPath)
		return Result{}, err
	}
	importResult, importErr := state.ImportStoreSnapshot(source, destination)
	closeErr := destination.Close()
	if importErr != nil {
		RestoreBackup(targetPath, backupPath)
		return Result{}, importErr
	}
	if closeErr != nil {
		RestoreBackup(targetPath, backupPath)
		return Result{}, closeErr
	}
	return Result{SourcePath: opts.SourcePath, TargetPath: targetPath, BackupPath: backupPath, Folders: importResult.Folders, ImportedManifests: importResult.ImportedManifests}, nil
}

func BackupExistingStore(path string) (string, error) {
	if _, err := os.Stat(path); os.IsNotExist(err) {
		return "", nil
	} else if err != nil {
		return "", err
	}
	for i := 0; ; i++ {
		candidate := fmt.Sprintf("%s.backup-%s", path, time.Now().UTC().Format("20060102T150405Z"))
		if i > 0 {
			candidate = fmt.Sprintf("%s.%d", candidate, i)
		}
		if _, err := os.Stat(candidate); os.IsNotExist(err) {
			if err := os.Rename(path, candidate); err != nil {
				return "", err
			}
			return candidate, nil
		} else if err != nil {
			return "", err
		}
	}
}

func RestoreBackup(path string, backupPath string) {
	if backupPath == "" {
		_ = os.RemoveAll(path)
		return
	}
	_ = os.RemoveAll(path)
	_ = os.Rename(backupPath, path)
}

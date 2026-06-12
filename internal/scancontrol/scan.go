package scancontrol

import (
	"fmt"

	"filesyncengine/internal/config"
	"filesyncengine/internal/engine"
	"filesyncengine/internal/metadatastore"
)

type FolderResult struct {
	FolderID  string
	Changed   int
	Deleted   int
	StatePath string
}

type Result struct {
	Folders   []FolderResult
	StatePath string
}

func RunQuickIndex(cfg config.Config, configPath string, folderID string) (Result, error) {
	if cfg.Metadata.PerFolder {
		return runQuickIndexPerFolder(cfg, configPath, folderID)
	}
	store, statePath, err := metadatastore.Open(cfg, configPath)
	if err != nil {
		return Result{}, err
	}
	defer store.Close()
	result := Result{StatePath: statePath}
	eng := engine.New(store)
	for _, folder := range cfg.Folders {
		if folderID != "" && folder.ID != folderID {
			continue
		}
		scan, err := eng.QuickIndex(folder)
		if err != nil {
			return Result{}, fmt.Errorf("scan %s: %w", folder.ID, err)
		}
		result.Folders = append(result.Folders, FolderResult{FolderID: scan.FolderID, Changed: len(scan.Changed), Deleted: len(scan.Deleted), StatePath: statePath})
	}
	if folderID != "" && len(result.Folders) == 0 {
		return Result{}, fmt.Errorf("folder %q not found", folderID)
	}
	return result, nil
}

func runQuickIndexPerFolder(cfg config.Config, configPath string, folderID string) (Result, error) {
	result := Result{StatePath: metadatastore.ConfiguredStorePath(cfg, configPath)}
	for _, folder := range cfg.Folders {
		if folderID != "" && folder.ID != folderID {
			continue
		}
		store, statePath, err := metadatastore.OpenFolder(cfg, configPath, folder.ID)
		if err != nil {
			return Result{}, err
		}
		eng := engine.New(store)
		scan, scanErr := eng.QuickIndex(folder)
		closeErr := store.Close()
		if scanErr != nil {
			return Result{}, fmt.Errorf("scan %s: %w", folder.ID, scanErr)
		}
		if closeErr != nil {
			return Result{}, fmt.Errorf("close metadata store %s: %w", statePath, closeErr)
		}
		result.Folders = append(result.Folders, FolderResult{FolderID: scan.FolderID, Changed: len(scan.Changed), Deleted: len(scan.Deleted), StatePath: statePath})
	}
	if folderID != "" && len(result.Folders) == 0 {
		return Result{}, fmt.Errorf("folder %q not found", folderID)
	}
	return result, nil
}

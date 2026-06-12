package foldersync

import (
	"fmt"

	"filesyncengine/internal/config"
	"filesyncengine/internal/localsync"
)

type Folder struct {
	ID             string
	Path           string
	SyncGroup      string
	Mode           config.FolderMode
	BlockSize      int
	IgnoreSuffixes []string
	Permissions    config.PermissionPolicy
}

type Result struct {
	Writes       int
	Deletes      int
	Moves        int
	ReusedBlocks int
	Conflicts    int
	Targets      int
	Inaccessible []InaccessibleWarning
}

type InaccessibleWarning struct {
	FolderID string
	Role     string
	Path     string
	Error    string
}

type Runner struct {
	folders map[string]Folder
}

func New(folders []Folder) *Runner {
	byID := make(map[string]Folder, len(folders))
	for _, folder := range folders {
		if folder.SyncGroup == "" {
			folder.SyncGroup = folder.ID
		}
		if folder.Mode == "" {
			folder.Mode = config.ModeSendReceive
		}
		byID[folder.ID] = folder
	}
	return &Runner{folders: byID}
}

func (r *Runner) ScanDue(folderID string) (Result, error) {
	source, ok := r.folders[folderID]
	if !ok {
		return Result{}, fmt.Errorf("unknown folder %q", folderID)
	}
	if !canSend(source.Mode) {
		return Result{}, nil
	}
	result := Result{}
	for _, target := range r.folders {
		if target.ID == source.ID || target.SyncGroup != source.SyncGroup || !canReceive(target.Mode) {
			continue
		}
		syncResult, err := localsync.SyncOneWay(source.Path, target.Path, localsync.Options{
			BlockSize:               source.BlockSize,
			IgnoreSuffixes:          source.IgnoreSuffixes,
			PreserveTargetConflicts: source.Mode == config.ModeSendReceive && target.Mode == config.ModeSendReceive,
			ConflictSuffix:          ".sync-conflict-" + target.ID,
			Permissions:             target.Permissions,
		})
		if err != nil {
			return result, err
		}
		result.Writes += syncResult.Writes
		result.Deletes += syncResult.Deletes
		result.Moves += syncResult.Moves
		result.ReusedBlocks += syncResult.ReusedBlocks
		result.Conflicts += syncResult.Conflicts
		for _, warning := range syncResult.SourceInaccessible {
			result.Inaccessible = append(result.Inaccessible, InaccessibleWarning{FolderID: source.ID, Role: "source", Path: warning.RelativePath, Error: warning.Error})
		}
		for _, warning := range syncResult.TargetInaccessible {
			result.Inaccessible = append(result.Inaccessible, InaccessibleWarning{FolderID: target.ID, Role: "target", Path: warning.RelativePath, Error: warning.Error})
		}
		result.Targets++
	}
	return result, nil
}

func canSend(mode config.FolderMode) bool {
	return mode == config.ModeSendOnly || mode == config.ModeSendReceive
}

func canReceive(mode config.FolderMode) bool {
	return mode == config.ModeReceiveOnly || mode == config.ModeSendReceive
}

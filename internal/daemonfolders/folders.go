package daemonfolders

import (
	"filesyncengine/internal/config"
	"filesyncengine/internal/foldersync"
)

func SyncFolders(cfg config.Config) []foldersync.Folder {
	folders := make([]foldersync.Folder, 0, len(cfg.Folders))
	for _, folder := range cfg.Folders {
		folders = append(folders, foldersync.Folder{
			ID:             folder.ID,
			Path:           folder.Path,
			SyncGroup:      folder.SyncGroup,
			Mode:           folder.Mode,
			BlockSize:      folder.BlockSize,
			IgnoreSuffixes: folder.Ignore,
			Permissions:    folder.Permissions,
		})
	}
	return folders
}

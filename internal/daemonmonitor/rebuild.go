package daemonmonitor

import (
	"filesyncengine/internal/config"
	"filesyncengine/internal/monitor"
)

// Closable is the narrow lifecycle contract required by the daemon's folder monitor rebuild path.
type Closable interface {
	Close() error
}

// FoldersFromConfig projects sync folder config into monitor roots.
func FoldersFromConfig(cfg config.Config) []monitor.Folder {
	folders := make([]monitor.Folder, 0, len(cfg.Folders))
	for _, folder := range cfg.Folders {
		folders = append(folders, monitor.Folder{ID: folder.ID, Path: folder.Path})
	}
	return folders
}

// Rebuild starts a replacement monitor before closing the current one so failed rebuilds do not
// drop active filesystem watching. If closing the current monitor fails, the replacement is closed
// and the current monitor remains active from the caller's perspective.
func Rebuild(current Closable, active []monitor.Folder, cfg config.Config, start func([]monitor.Folder) (Closable, error)) (Closable, bool, error) {
	nextFolders := FoldersFromConfig(cfg)
	if sameFolders(active, nextFolders) {
		return current, false, nil
	}
	nextMonitor, err := start(nextFolders)
	if err != nil {
		return current, false, err
	}
	if current != nil {
		if err := current.Close(); err != nil {
			_ = nextMonitor.Close()
			return current, false, err
		}
	}
	return nextMonitor, true, nil
}

func sameFolders(a, b []monitor.Folder) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

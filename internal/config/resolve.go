package config

import (
	"fmt"
	"os"
)

func ResolvePath(explicit string, common []string) (string, error) {
	if explicit != "" {
		return explicit, nil
	}
	for _, candidate := range common {
		if _, err := os.Stat(candidate); err == nil {
			return candidate, nil
		}
	}
	return "", fmt.Errorf("config not found; pass explicit path or create one of the common config files")
}

func CommonPaths(binaryDir string) []string {
	paths := []string{}
	if binaryDir != "" {
		paths = append(paths, binaryDir+string(os.PathSeparator)+"file-sync-engine.json")
	}
	paths = append(paths,
		"/etc/file-sync-engine/config.json",
		"/etc/filesyncengine/config.json",
	)
	return paths
}

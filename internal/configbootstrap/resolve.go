package configbootstrap

import (
	"path/filepath"

	"filesyncengine/internal/config"
)

// ResolveOrCreate returns the explicit config path when provided, otherwise it
// searches common config locations next to the executable and creates a
// skeleton at the selected path when needed.
func ResolveOrCreate(explicit, executablePath string) (string, error) {
	if explicit != "" {
		_, _, err := config.EnsureFile(explicit)
		return explicit, err
	}
	common := config.CommonPaths(filepath.Dir(executablePath))
	if path, err := config.ResolvePath("", common); err == nil {
		_, _, err := config.EnsureFile(path)
		return path, err
	}
	path := common[0]
	_, _, err := config.EnsureFile(path)
	return path, err
}

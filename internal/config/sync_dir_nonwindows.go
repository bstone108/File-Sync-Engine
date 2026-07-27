//go:build !windows

package config

import "os"

// syncDir persists the directory entry after an atomic replacement on platforms
// that support syncing directories.
func syncDir(dir string) error {
	f, err := os.Open(dir)
	if err != nil {
		return nil
	}
	defer f.Close()
	return f.Sync()
}

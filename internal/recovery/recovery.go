package recovery

import (
	"os"
	"path/filepath"
	"strings"
)

type Result struct {
	Removed int
}

// CleanFolder removes temporary files left behind by interrupted staged writes.
// It only removes temp names produced by this prototype's apply/peer/stream
// writers; normal user files are left untouched.
func CleanFolder(root string) (Result, error) {
	result := Result{}
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			if os.IsNotExist(walkErr) {
				return nil
			}
			return walkErr
		}
		if entry.IsDir() {
			return nil
		}
		if !IsInterruptedTempName(entry.Name()) {
			return nil
		}
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			return err
		}
		result.Removed++
		return nil
	})
	if os.IsNotExist(err) {
		return result, nil
	}
	return result, err
}

func IsInterruptedTempName(name string) bool {
	return isApplyStagingName(name) || strings.HasSuffix(name, ".fse-peer-tmp") || strings.HasSuffix(name, ".fse-stream-tmp")
}

func isApplyStagingName(name string) bool {
	if !strings.HasPrefix(name, ".") || !strings.HasSuffix(name, ".staging") {
		return false
	}
	trimmed := strings.TrimSuffix(name, ".staging")
	lastDot := strings.LastIndex(trimmed, ".")
	if lastDot < 1 || lastDot == len(trimmed)-1 {
		return false
	}
	for _, r := range trimmed[lastDot+1:] {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

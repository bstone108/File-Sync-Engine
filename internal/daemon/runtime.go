package daemon

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"filesyncengine/internal/config"
	"filesyncengine/internal/state"
)

type Runtime struct {
	manager    *config.Manager
	generation uint64
}

func NewRuntime(manager *config.Manager) *Runtime {
	return &Runtime{manager: manager, generation: 1}
}

func (r *Runtime) PollConfig() (bool, error) {
	changed, err := r.manager.ReloadIfChanged()
	if err != nil {
		return false, err
	}
	if changed {
		r.generation++
	}
	return changed, nil
}

func (r *Runtime) Generation() uint64 {
	return r.generation
}

func (r *Runtime) Config() config.Config {
	return r.manager.Current()
}

type skippedDeleteReconciliationStore interface {
	FolderSummary(folderID string) (state.FolderSummary, error)
	SkippedDeletes(folderID string) ([]state.SkippedDelete, error)
	ReadySkippedDeletes(folderID string, current state.FolderSummary) ([]state.SkippedDelete, error)
	RemoveSkippedDelete(folderID string, path string) error
}

type SkippedDeleteReconcileResult struct {
	Deleted   int
	Remaining int
}

func ReconcileReadySkippedDeletes(root string, folderID string, store skippedDeleteReconciliationStore) (SkippedDeleteReconcileResult, error) {
	current, err := store.FolderSummary(folderID)
	if err != nil {
		return SkippedDeleteReconcileResult{}, err
	}
	ready, err := store.ReadySkippedDeletes(folderID, current)
	if err != nil {
		return SkippedDeleteReconcileResult{}, err
	}
	result := SkippedDeleteReconcileResult{}
	for _, delete := range ready {
		path, err := safeRelativePath(root, delete.Path)
		if err != nil {
			return result, err
		}
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			return result, err
		}
		if err := store.RemoveSkippedDelete(folderID, delete.Path); err != nil {
			return result, err
		}
		result.Deleted++
	}
	remaining, err := store.SkippedDeletes(folderID)
	if err != nil {
		return result, err
	}
	result.Remaining = len(remaining)
	return result, nil
}

func safeRelativePath(root string, rel string) (string, error) {
	if rel == "" || filepath.IsAbs(rel) {
		return "", fmt.Errorf("unsafe path %q", rel)
	}
	clean := filepath.Clean(filepath.FromSlash(rel))
	if clean == "." || clean == ".." || strings.HasPrefix(clean, ".."+string(os.PathSeparator)) {
		return "", fmt.Errorf("unsafe path %q", rel)
	}
	rootAbs, err := filepath.Abs(root)
	if err != nil {
		return "", err
	}
	fullAbs, err := filepath.Abs(filepath.Join(rootAbs, clean))
	if err != nil {
		return "", err
	}
	relToRoot, err := filepath.Rel(rootAbs, fullAbs)
	if err != nil {
		return "", err
	}
	if relToRoot == ".." || strings.HasPrefix(relToRoot, ".."+string(os.PathSeparator)) {
		return "", fmt.Errorf("unsafe path %q", rel)
	}
	return fullAbs, nil
}

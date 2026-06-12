package backup

import (
	"fmt"
	"io"
	"os"
	pathpkg "path"
	"path/filepath"
	"sort"
	"strings"
)

type MirrorUpdateResult struct {
	Copied  int
	Deleted int
}

func ExecuteMirrorUpdate(sourceRoot string, mirrorRoot string, plan SnapshotProtectionPlan) (MirrorUpdateResult, error) {
	if len(plan.MirrorFiles) == 0 {
		return MirrorUpdateResult{}, nil
	}
	if err := validateMirrorRoots(sourceRoot, mirrorRoot); err != nil {
		return MirrorUpdateResult{}, err
	}
	wanted := map[string]struct{}{}
	paths := append([]string(nil), plan.MirrorFiles...)
	sort.Strings(paths)
	result := MirrorUpdateResult{}
	for _, rel := range paths {
		clean, err := cleanMirrorRel(rel)
		if err != nil {
			return result, err
		}
		wanted[clean] = struct{}{}
		if err := copyMirrorFile(sourceRoot, mirrorRoot, clean); err != nil {
			return result, err
		}
		result.Copied++
	}
	deleted, err := deleteStaleMirrorFiles(mirrorRoot, wanted)
	if err != nil {
		return result, err
	}
	result.Deleted = deleted
	return result, nil
}

func validateMirrorRoots(sourceRoot string, mirrorRoot string) error {
	sourceAbs, err := filepath.Abs(sourceRoot)
	if err != nil {
		return err
	}
	mirrorAbs, err := filepath.Abs(mirrorRoot)
	if err != nil {
		return err
	}
	sourceAbs = filepath.Clean(sourceAbs)
	mirrorAbs = filepath.Clean(mirrorAbs)
	if sourceAbs == mirrorAbs {
		return fmt.Errorf("mirror root must be separate from source root")
	}
	rel, err := filepath.Rel(sourceAbs, mirrorAbs)
	if err != nil {
		return err
	}
	if rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) && rel != "." {
		return fmt.Errorf("mirror root %q must not be inside source root %q", mirrorRoot, sourceRoot)
	}
	return nil
}

func cleanMirrorRel(rel string) (string, error) {
	if rel == "" || strings.HasPrefix(rel, "/") || strings.Contains(rel, "\\") {
		return "", fmt.Errorf("mirror path %q is not a safe relative path", rel)
	}
	clean := pathpkg.Clean(rel)
	if clean == "." || strings.HasPrefix(clean, "../") || clean == ".." {
		return "", fmt.Errorf("mirror path %q is not a safe relative path", rel)
	}
	if clean == ".sync" || strings.HasPrefix(clean, ".sync/") {
		return "", fmt.Errorf("mirror path %q targets engine metadata", rel)
	}
	return clean, nil
}

func copyMirrorFile(sourceRoot string, mirrorRoot string, rel string) error {
	sourcePath := filepath.Join(sourceRoot, filepath.FromSlash(rel))
	targetPath := filepath.Join(mirrorRoot, filepath.FromSlash(rel))
	in, err := os.Open(sourcePath)
	if err != nil {
		return err
	}
	defer in.Close()
	info, err := in.Stat()
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("mirror source %q is not a regular file", rel)
	}
	if err := os.MkdirAll(filepath.Dir(targetPath), 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(targetPath), "."+filepath.Base(targetPath)+".*.mirror-staging")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	cleanup := true
	defer func() {
		if cleanup {
			_ = os.Remove(tmpName)
		}
	}()
	if _, err := io.Copy(tmp, in); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Chmod(tmpName, info.Mode().Perm()); err != nil {
		return err
	}
	if err := os.Rename(tmpName, targetPath); err != nil {
		return err
	}
	cleanup = false
	return nil
}

func deleteStaleMirrorFiles(mirrorRoot string, wanted map[string]struct{}) (int, error) {
	deleted := 0
	if _, err := os.Stat(mirrorRoot); os.IsNotExist(err) {
		return 0, nil
	}
	err := filepath.WalkDir(mirrorRoot, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if path == mirrorRoot {
			return nil
		}
		rel, err := filepath.Rel(mirrorRoot, path)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		if rel == ".sync" || strings.HasPrefix(rel, ".sync/") {
			if entry.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if entry.IsDir() {
			return nil
		}
		if _, ok := wanted[rel]; ok {
			return nil
		}
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			return err
		}
		deleted++
		return nil
	})
	return deleted, err
}

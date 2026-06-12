package localsync

import (
	"bytes"
	"encoding/hex"
	"io"
	"os"
	pathpkg "path"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"filesyncengine/internal/apply"
	"filesyncengine/internal/backup"
	"filesyncengine/internal/block"
	"filesyncengine/internal/config"
	"filesyncengine/internal/recovery"
	"filesyncengine/internal/scanner"
)

type Options struct {
	BlockSize               int
	IgnoreSuffixes          []string
	PreserveTargetConflicts bool
	ConflictSuffix          string
	Permissions             config.PermissionPolicy
}

type StepKind string

const (
	StepWrite  StepKind = "write"
	StepDelete StepKind = "delete"
	StepMove   StepKind = "move"
)

type Step struct {
	Kind StepKind
	Path string
}

type Result struct {
	Writes             int
	Deletes            int
	Moves              int
	ReusedBlocks       int
	Conflicts          int
	SourceInaccessible []scanner.InaccessibleFile
	TargetInaccessible []scanner.InaccessibleFile
	Steps              []Step
}

func SyncOneWay(sourceRoot, targetRoot string, opts Options) (Result, error) {
	if opts.BlockSize <= 0 {
		opts.BlockSize = 128 * 1024
	}
	if _, err := recovery.CleanFolder(sourceRoot); err != nil {
		return Result{}, err
	}
	if _, err := recovery.CleanFolder(targetRoot); err != nil {
		return Result{}, err
	}
	scanOpts := scanner.Options{BlockSize: opts.BlockSize, IgnoreSuffixes: opts.IgnoreSuffixes}
	sourceScan, err := scanner.ScanFolder(sourceRoot, scanOpts)
	if err != nil {
		return Result{}, err
	}
	targetScan, err := scanner.ScanFolder(targetRoot, scanOpts)
	if err != nil {
		if os.IsNotExist(err) {
			if err := os.MkdirAll(targetRoot, 0o755); err != nil {
				return Result{}, err
			}
			targetScan = scanner.Result{Root: targetRoot}
		} else {
			return Result{}, err
		}
	}
	result := Result{
		SourceInaccessible: append([]scanner.InaccessibleFile(nil), sourceScan.Inaccessible...),
		TargetInaccessible: append([]scanner.InaccessibleFile(nil), targetScan.Inaccessible...),
	}
	targetIgnoreMatcher, err := scanner.LoadSyncIgnoreMatcher(targetRoot)
	if err != nil {
		return Result{}, err
	}

	sourceByPath := filesByPath(sourceScan.Files)
	targetByPath := filesByPath(targetScan.Files)
	sourceManifests := manifests(sourceScan.Files)
	targetManifests := manifests(targetScan.Files)
	assemblySources := append(append([]block.Manifest{}, targetManifests...), sourceManifests...)

	protectedDeletes := map[string]bool{}
	moveCandidates := staleMoveCandidates(sourceByPath, targetByPath)
	writePaths := sortedKeys(sourceByPath)
	for _, rel := range writePaths {
		if ignoredForTarget(targetIgnoreMatcher, rel) {
			continue
		}
		sourceFile := sourceByPath[rel]
		if targetFile, ok := targetByPath[rel]; ok && manifestsEqual(sourceFile.Manifest, targetFile.Manifest) {
			continue
		} else if !ok {
			if moveRel, moved := takeMoveCandidate(moveCandidates, sourceFile.Manifest); moved {
				targetPath := filepath.Join(targetRoot, filepath.FromSlash(rel))
				if err := prepareTargetDirectory(targetRoot, filepath.Dir(targetPath), opts.Permissions); err != nil {
					return result, err
				}
				if err := os.Rename(filepath.Join(targetRoot, filepath.FromSlash(moveRel)), targetPath); err != nil {
					return result, err
				}
				if err := applyPermissionPolicy(targetPath, filepath.Join(sourceRoot, filepath.FromSlash(rel)), opts.Permissions); err != nil {
					return result, err
				}
				protectedDeletes[moveRel] = true
				result.Moves++
				result.Steps = append(result.Steps, Step{Kind: StepMove, Path: rel})
				continue
			}
		} else if ok && opts.PreserveTargetConflicts {
			conflictRel := nextConflictRel(targetRoot, rel, opts.ConflictSuffix)
			conflictPath := filepath.Join(targetRoot, filepath.FromSlash(conflictRel))
			if err := copyFileAtomic(filepath.Join(targetRoot, filepath.FromSlash(rel)), conflictPath); err != nil {
				return result, err
			}
			protectedDeletes[conflictRel] = true
			result.Conflicts++
		}
		targetPath := filepath.Join(targetRoot, filepath.FromSlash(rel))
		if err := prepareTargetDirectory(targetRoot, filepath.Dir(targetPath), opts.Permissions); err != nil {
			return result, err
		}
		plan := block.PlanContentDelta(targetManifests, sourceFile.Manifest)
		result.ReusedBlocks += len(plan.Reused)
		if err := apply.AssembleFromLocalBlocksBeforeRename(targetPath, sourceFile.Manifest, assemblySources, func() error {
			_, _, err := backup.RetainExistingBackupIntakeFile(targetRoot, rel, time.Time{})
			return err
		}); err != nil {
			return result, err
		}
		if err := applyPermissionPolicy(targetPath, filepath.Join(sourceRoot, filepath.FromSlash(rel)), opts.Permissions); err != nil {
			return result, err
		}
		result.Writes++
		result.Steps = append(result.Steps, Step{Kind: StepWrite, Path: rel})
	}

	deletePaths := stalePaths(sourceByPath, targetByPath)
	for _, rel := range deletePaths {
		if protectedDeletes[rel] || (opts.PreserveTargetConflicts && isConflictRel(rel, opts.ConflictSuffix)) {
			continue
		}
		if _, _, err := backup.RetainExistingBackupIntakeFile(targetRoot, rel, time.Time{}); err != nil {
			return result, err
		}
		if err := os.Remove(filepath.Join(targetRoot, filepath.FromSlash(rel))); err != nil && !os.IsNotExist(err) {
			return result, err
		}
		result.Deletes++
		result.Steps = append(result.Steps, Step{Kind: StepDelete, Path: rel})
	}
	return result, nil
}

func nextConflictRel(targetRoot, rel, suffix string) string {
	if suffix == "" {
		suffix = ".sync-conflict"
	}
	ext := pathpkg.Ext(rel)
	base := strings.TrimSuffix(rel, ext)
	for i := 0; ; i++ {
		candidateSuffix := suffix
		if i > 0 {
			candidateSuffix = suffix + "-" + strconv.Itoa(i)
		}
		candidate := base + candidateSuffix + ext
		if _, err := os.Stat(filepath.Join(targetRoot, filepath.FromSlash(candidate))); os.IsNotExist(err) {
			return candidate
		}
	}
}

func isConflictRel(rel, suffix string) bool {
	if suffix == "" {
		suffix = ".sync-conflict"
	}
	return strings.Contains(pathpkg.Base(rel), suffix)
}

func copyFileAtomic(sourcePath, targetPath string) error {
	if err := os.MkdirAll(filepath.Dir(targetPath), 0o755); err != nil {
		return err
	}
	in, err := os.Open(sourcePath)
	if err != nil {
		return err
	}
	defer in.Close()
	tmp, err := os.CreateTemp(filepath.Dir(targetPath), "."+filepath.Base(targetPath)+".*.staging")
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
	if err := os.Rename(tmpName, targetPath); err != nil {
		return err
	}
	cleanup = false
	return nil
}

func prepareTargetDirectory(root, dir string, policy config.PermissionPolicy) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	mode, ok, err := directoryMode(policy)
	if err != nil || !ok {
		return err
	}
	for current := dir; ; current = filepath.Dir(current) {
		if err := os.Chmod(current, mode); err != nil {
			return err
		}
		if current == root || current == filepath.Dir(current) {
			break
		}
	}
	return nil
}

func applyPermissionPolicy(targetPath, sourcePath string, policy config.PermissionPolicy) error {
	mode, ok, err := fileMode(policy, sourcePath)
	if err != nil || !ok {
		return err
	}
	return os.Chmod(targetPath, mode)
}

func fileMode(policy config.PermissionPolicy, sourcePath string) (os.FileMode, bool, error) {
	switch policy.Mode {
	case config.PermissionSync:
		info, err := os.Stat(sourcePath)
		if err != nil {
			return 0, false, err
		}
		return info.Mode().Perm(), true, nil
	case config.PermissionDefault, config.PermissionFixed:
		return parseOptionalMode(policy.FileMode)
	default:
		return 0, false, nil
	}
}

func directoryMode(policy config.PermissionPolicy) (os.FileMode, bool, error) {
	switch policy.Mode {
	case config.PermissionDefault, config.PermissionFixed:
		return parseOptionalMode(policy.DirMode)
	default:
		return 0, false, nil
	}
}

func parseOptionalMode(value string) (os.FileMode, bool, error) {
	if value == "" {
		return 0, false, nil
	}
	parsed, err := strconv.ParseUint(value, 8, 32)
	if err != nil {
		return 0, false, err
	}
	return os.FileMode(parsed).Perm(), true, nil
}

func filesByPath(files []scanner.File) map[string]scanner.File {
	out := make(map[string]scanner.File, len(files))
	for _, file := range files {
		out[file.RelativePath] = file
	}
	return out
}

func manifests(files []scanner.File) []block.Manifest {
	out := make([]block.Manifest, 0, len(files))
	for _, file := range files {
		out = append(out, file.Manifest)
	}
	return out
}

func sortedKeys(files map[string]scanner.File) []string {
	keys := make([]string, 0, len(files))
	for rel := range files {
		keys = append(keys, rel)
	}
	sort.Strings(keys)
	return keys
}

func stalePaths(source, target map[string]scanner.File) []string {
	paths := make([]string, 0)
	for rel := range target {
		if _, ok := source[rel]; !ok {
			paths = append(paths, rel)
		}
	}
	sort.Strings(paths)
	return paths
}

func staleMoveCandidates(source, target map[string]scanner.File) map[string][]string {
	candidates := map[string][]string{}
	for _, rel := range stalePaths(source, target) {
		file := target[rel]
		key := manifestKey(file.Manifest)
		if key == "" {
			continue
		}
		candidates[key] = append(candidates[key], rel)
	}
	for key := range candidates {
		sort.Strings(candidates[key])
	}
	return candidates
}

func takeMoveCandidate(candidates map[string][]string, manifest block.Manifest) (string, bool) {
	key := manifestKey(manifest)
	paths := candidates[key]
	if len(paths) == 0 {
		return "", false
	}
	path := paths[0]
	if len(paths) == 1 {
		delete(candidates, key)
	} else {
		candidates[key] = paths[1:]
	}
	return path, true
}

func manifestKey(manifest block.Manifest) string {
	var builder strings.Builder
	builder.WriteString(strconv.FormatInt(manifest.Size, 10))
	for _, b := range manifest.Blocks {
		builder.WriteByte('|')
		builder.WriteString(strconv.Itoa(b.Size))
		builder.WriteByte(':')
		builder.WriteString(hex.EncodeToString(b.Hash))
	}
	return builder.String()
}

func ignoredForTarget(matcher scanner.SyncIgnoreMatcher, rel string) bool {
	rel = filepath.ToSlash(rel)
	return rel == ".sync" || strings.HasPrefix(rel, ".sync/") || matcher.IsIgnored(rel)
}

func manifestsEqual(a, b block.Manifest) bool {
	if a.Size != b.Size || len(a.Blocks) != len(b.Blocks) {
		return false
	}
	for i := range a.Blocks {
		if a.Blocks[i].Size != b.Blocks[i].Size || !bytes.Equal(a.Blocks[i].Hash, b.Blocks[i].Hash) {
			return false
		}
	}
	return true
}

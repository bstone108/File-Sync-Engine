package maintenance

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"filesyncengine/internal/block"
)

type RepairReplacementPlan struct {
	Root            string
	RelativePath    string
	ReplacementPath string
	DesiredManifest block.Manifest
	Now             func() time.Time
}

type QuarantineRecord struct {
	OriginalPath    string
	QuarantinePath  string
	ReplacementPath string
}

func ApplyVerifiedReplacementWithQuarantine(plan RepairReplacementPlan) (QuarantineRecord, error) {
	target, ok := containedPath(plan.Root, plan.RelativePath)
	if !ok {
		return QuarantineRecord{}, fmt.Errorf("unsafe repair target path %q", plan.RelativePath)
	}
	if plan.ReplacementPath == "" {
		return QuarantineRecord{}, errors.New("replacement path is required")
	}
	if !pathUnderRoot(plan.Root, plan.ReplacementPath) {
		return QuarantineRecord{}, fmt.Errorf("replacement path %q is outside folder root", plan.ReplacementPath)
	}
	if err := verifyReplacementManifest(plan.ReplacementPath, plan.DesiredManifest); err != nil {
		return QuarantineRecord{}, err
	}
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return QuarantineRecord{}, err
	}
	record := QuarantineRecord{OriginalPath: filepath.ToSlash(filepath.Clean(plan.RelativePath)), ReplacementPath: plan.ReplacementPath}
	quarantined := false
	if _, err := os.Stat(target); err == nil {
		quarantinePath, rel, err := allocateQuarantinePath(plan.Root, plan.RelativePath, plan.Now)
		if err != nil {
			return QuarantineRecord{}, err
		}
		if err := os.MkdirAll(filepath.Dir(quarantinePath), 0o755); err != nil {
			return QuarantineRecord{}, err
		}
		if err := os.Rename(target, quarantinePath); err != nil {
			return QuarantineRecord{}, err
		}
		record.QuarantinePath = rel
		quarantined = true
	} else if !errors.Is(err, os.ErrNotExist) {
		return QuarantineRecord{}, err
	}
	if err := os.Rename(plan.ReplacementPath, target); err != nil {
		if quarantined {
			_ = os.Rename(filepath.Join(plan.Root, filepath.FromSlash(record.QuarantinePath)), target)
		}
		return QuarantineRecord{}, err
	}
	return record, nil
}

func verifyReplacementManifest(path string, desired block.Manifest) error {
	if !isVerifiableManifest(desired) {
		return errors.New("desired repair manifest is not complete/verifiable")
	}
	actual, err := block.BuildManifest(path, desired.BlockSize)
	if err != nil {
		return err
	}
	if !manifestBytesMatch(desired, actual) {
		return errors.New("replacement bytes do not match trusted manifest")
	}
	return nil
}

func allocateQuarantinePath(root string, rel string, now func() time.Time) (string, string, error) {
	cleanRel := filepath.ToSlash(filepath.Clean(rel))
	if strings.HasPrefix(cleanRel, "../") || cleanRel == ".." || filepath.IsAbs(cleanRel) {
		return "", "", fmt.Errorf("unsafe quarantine path %q", rel)
	}
	clock := now
	if clock == nil {
		clock = time.Now
	}
	stamp := clock().UTC().Format("20060102T150405Z")
	base := filepath.Join(root, ".sync", "quarantine", filepath.FromSlash(cleanRel)) + "." + stamp + ".damaged"
	for i := 0; ; i++ {
		candidate := base
		if i > 0 {
			candidate = fmt.Sprintf("%s.%d", base, i)
		}
		if _, err := os.Stat(candidate); errors.Is(err, os.ErrNotExist) {
			relToRoot, err := filepath.Rel(root, candidate)
			if err != nil {
				return "", "", err
			}
			return candidate, filepath.ToSlash(relToRoot), nil
		} else if err != nil {
			return "", "", err
		}
	}
}

func pathUnderRoot(root string, path string) bool {
	rootAbs, err := filepath.Abs(root)
	if err != nil {
		return false
	}
	pathAbs, err := filepath.Abs(path)
	if err != nil {
		return false
	}
	rel, err := filepath.Rel(rootAbs, pathAbs)
	if err != nil || rel == ".." || filepath.IsAbs(rel) || hasParentSegment(rel) {
		return false
	}
	return true
}

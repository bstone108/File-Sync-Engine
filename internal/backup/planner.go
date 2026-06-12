package backup

import (
	"fmt"
	"sort"

	"filesyncengine/internal/block"
	"filesyncengine/internal/config"
)

type SnapshotProtectionPlan struct {
	Mode          config.BackupMode
	MirrorFiles   []string
	ArchiveBlocks []ArchiveBlock
}

type ArchiveBlock struct {
	Path  string
	Block block.Block
}

func PlanDestinationMode(mode config.BackupMode, snapshotManifests []block.Manifest, currentManifests map[string]block.Manifest) (SnapshotProtectionPlan, error) {
	plan := SnapshotProtectionPlan{Mode: mode}
	sortedSnapshot := append([]block.Manifest(nil), snapshotManifests...)
	sort.Slice(sortedSnapshot, func(i, j int) bool { return sortedSnapshot[i].Path < sortedSnapshot[j].Path })

	switch mode {
	case config.BackupModeBlockArchiveOnly:
		plan.ArchiveBlocks = archiveAllBlocks(sortedSnapshot)
	case config.BackupModeMirrorPlusArchive:
		plan.MirrorFiles = mirrorPaths(currentManifests, sortedSnapshot)
		plan.ArchiveBlocks = archiveBlocksNotSatisfiedByMirror(sortedSnapshot, currentManifests)
	case config.BackupModeMirrorPlusFullArchive:
		plan.MirrorFiles = mirrorPaths(currentManifests, sortedSnapshot)
		plan.ArchiveBlocks = archiveAllBlocks(sortedSnapshot)
	default:
		return SnapshotProtectionPlan{}, fmt.Errorf("backup mode %q is not supported", mode)
	}
	return plan, nil
}

func mirrorPaths(currentManifests map[string]block.Manifest, fallback []block.Manifest) []string {
	if len(currentManifests) == 0 {
		paths := make([]string, 0, len(fallback))
		for _, manifest := range fallback {
			paths = append(paths, manifest.Path)
		}
		return paths
	}
	paths := make([]string, 0, len(currentManifests))
	for path := range currentManifests {
		paths = append(paths, path)
	}
	sort.Strings(paths)
	return paths
}

func archiveAllBlocks(manifests []block.Manifest) []ArchiveBlock {
	blocks := make([]ArchiveBlock, 0)
	seen := map[string]struct{}{}
	for _, manifest := range manifests {
		for _, b := range manifest.Blocks {
			key := archiveBlockKey(b)
			if _, ok := seen[key]; ok {
				continue
			}
			seen[key] = struct{}{}
			blocks = append(blocks, ArchiveBlock{Path: manifest.Path, Block: b})
		}
	}
	return blocks
}

func archiveBlocksNotSatisfiedByMirror(snapshotManifests []block.Manifest, currentManifests map[string]block.Manifest) []ArchiveBlock {
	currentBlocks := map[string]struct{}{}
	for _, manifest := range currentManifests {
		for _, b := range manifest.Blocks {
			currentBlocks[archiveBlockKey(b)] = struct{}{}
		}
	}
	blocks := make([]ArchiveBlock, 0)
	seen := map[string]struct{}{}
	for _, manifest := range snapshotManifests {
		for _, b := range manifest.Blocks {
			key := archiveBlockKey(b)
			if _, satisfied := currentBlocks[key]; satisfied {
				continue
			}
			if _, ok := seen[key]; ok {
				continue
			}
			seen[key] = struct{}{}
			blocks = append(blocks, ArchiveBlock{Path: manifest.Path, Block: b})
		}
	}
	return blocks
}

func archiveBlockKey(b block.Block) string {
	return fmt.Sprintf("%d:%x", b.Size, b.Hash)
}

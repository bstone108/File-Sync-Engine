package backup

import (
	"testing"

	"filesyncengine/internal/block"
	"filesyncengine/internal/config"
)

func TestPlanDestinationModeBlockArchiveOnlyArchivesBlocksWithoutMirror(t *testing.T) {
	manifest := manifestWithBlocks("docs/report.txt", blockWithHash(0, "alpha"), blockWithHash(1, "beta"))

	plan, err := PlanDestinationMode(config.BackupModeBlockArchiveOnly, []block.Manifest{manifest}, nil)
	if err != nil {
		t.Fatalf("plan destination mode: %v", err)
	}

	if len(plan.MirrorFiles) != 0 {
		t.Fatalf("block-archive-only should not mirror current files: %+v", plan.MirrorFiles)
	}
	if len(plan.ArchiveBlocks) != 2 {
		t.Fatalf("block-archive-only should archive every snapshot block, got %+v", plan.ArchiveBlocks)
	}
}

func TestPlanDestinationModeMirrorPlusArchiveUsesMirrorForCurrentBlocks(t *testing.T) {
	snapshot := manifestWithBlocks("docs/report.txt", blockWithHash(0, "current"), blockWithHash(1, "old"))
	current := manifestWithBlocks("docs/report.txt", blockWithHash(0, "current"), blockWithHash(1, "new"))

	plan, err := PlanDestinationMode(config.BackupModeMirrorPlusArchive, []block.Manifest{snapshot}, map[string]block.Manifest{"docs/report.txt": current})
	if err != nil {
		t.Fatalf("plan destination mode: %v", err)
	}

	if len(plan.MirrorFiles) != 1 || plan.MirrorFiles[0] != "docs/report.txt" {
		t.Fatalf("mirror-plus-archive should mirror the live file: %+v", plan.MirrorFiles)
	}
	if len(plan.ArchiveBlocks) != 1 || string(plan.ArchiveBlocks[0].Block.Hash) != "old" {
		t.Fatalf("mirror-plus-archive should archive only snapshot blocks not satisfied by the mirror: %+v", plan.ArchiveBlocks)
	}
}

func TestPlanDestinationModeMirrorPlusFullArchiveDuplicatesCurrentBlocks(t *testing.T) {
	snapshot := manifestWithBlocks("docs/report.txt", blockWithHash(0, "current"), blockWithHash(1, "old"))
	current := manifestWithBlocks("docs/report.txt", blockWithHash(0, "current"), blockWithHash(1, "new"))

	plan, err := PlanDestinationMode(config.BackupModeMirrorPlusFullArchive, []block.Manifest{snapshot}, map[string]block.Manifest{"docs/report.txt": current})
	if err != nil {
		t.Fatalf("plan destination mode: %v", err)
	}

	if len(plan.MirrorFiles) != 1 || plan.MirrorFiles[0] != "docs/report.txt" {
		t.Fatalf("mirror-plus-full-archive should mirror the live file: %+v", plan.MirrorFiles)
	}
	if len(plan.ArchiveBlocks) != 2 {
		t.Fatalf("mirror-plus-full-archive should archive every snapshot block for extra durability, got %+v", plan.ArchiveBlocks)
	}
}

func TestPlanDestinationModeRejectsUnknownMode(t *testing.T) {
	if _, err := PlanDestinationMode(config.BackupMode("unknown"), nil, nil); err == nil {
		t.Fatalf("expected unknown backup mode to be rejected")
	}
}

func manifestWithBlocks(path string, blocks ...block.Block) block.Manifest {
	return block.Manifest{Path: path, Size: int64(len(blocks)), BlockSize: 1, Blocks: blocks, HashState: "complete"}
}

func blockWithHash(index int, hash string) block.Block {
	return block.Block{Index: index, Offset: int64(index), Size: 1, Hash: []byte(hash)}
}

package maintenance

import (
	"context"
	"testing"

	"filesyncengine/internal/block"
	"filesyncengine/internal/state"
)

type fakeBlockIndexStore struct {
	refs      map[string][]state.BlockRef
	manifests map[string]map[string]block.Manifest
}

func (f fakeBlockIndexStore) ListBlockRefs(folderID string) ([]state.BlockRef, error) {
	return append([]state.BlockRef(nil), f.refs[folderID]...), nil
}

func (f fakeBlockIndexStore) LoadManifest(folderID string, relativePath string) (block.Manifest, bool, error) {
	manifest, ok := f.manifests[folderID][relativePath]
	return manifest, ok, nil
}

func TestBlockIndexCrawlerReportsRefsWhoseManifestOwnerIsMissing(t *testing.T) {
	crawler := BlockIndexCrawler{
		Store: fakeBlockIndexStore{
			refs: map[string][]state.BlockRef{"docs": {
				{FolderID: "docs", RelativePath: "live.bin", Block: block.Block{Index: 0, Offset: 0, Size: 4, Hash: []byte{0xaa}}},
				{FolderID: "docs", RelativePath: "missing.bin", Block: block.Block{Index: 0, Offset: 0, Size: 4, Hash: []byte{0xbb}}},
			}},
			manifests: map[string]map[string]block.Manifest{"docs": {
				"live.bin": {Path: "live.bin", Blocks: []block.Block{{Index: 0, Offset: 0, Size: 4, Hash: []byte{0xaa}}}},
			}},
		},
		FolderIDs: []string{"docs"},
	}

	result, err := RunOnce(context.Background(), RunOptions{
		Crawler:    crawler,
		Checkpoint: &memoryCheckpoint{},
		MaxFiles:   10,
	})
	if err != nil {
		t.Fatalf("RunOnce: %v", err)
	}
	if result.FilesScanned != 2 || result.Reported != 1 || result.Pruned != 0 || !result.Complete {
		t.Fatalf("result=%+v, want one missing-owner block index ref reported without pruning", result)
	}
}

func TestBlockIndexCrawlerReportsRefsWhoseManifestNoLongerContainsBlock(t *testing.T) {
	crawler := BlockIndexCrawler{
		Store: fakeBlockIndexStore{
			refs: map[string][]state.BlockRef{"docs": {
				{FolderID: "docs", RelativePath: "changed.bin", Block: block.Block{Index: 1, Offset: 4, Size: 4, Hash: []byte{0xcc}}},
			}},
			manifests: map[string]map[string]block.Manifest{"docs": {
				"changed.bin": {Path: "changed.bin", Blocks: []block.Block{{Index: 0, Offset: 0, Size: 4, Hash: []byte{0xdd}}}},
			}},
		},
		FolderIDs: []string{"docs"},
	}

	result, err := RunOnce(context.Background(), RunOptions{
		Crawler:    crawler,
		Checkpoint: &memoryCheckpoint{},
		MaxFiles:   10,
	})
	if err != nil {
		t.Fatalf("RunOnce: %v", err)
	}
	if result.FilesScanned != 1 || result.Reported != 1 || result.Pruned != 0 || !result.Complete {
		t.Fatalf("result=%+v, want stale block index ref reported without pruning", result)
	}
}

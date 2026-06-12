package maintenance

import (
	"bytes"
	"context"
	"sort"

	"filesyncengine/internal/block"
	"filesyncengine/internal/state"
)

type BlockIndexStore interface {
	ListBlockRefs(folderID string) ([]state.BlockRef, error)
	LoadManifest(folderID string, relativePath string) (block.Manifest, bool, error)
}

type BlockIndexCrawler struct {
	Store     BlockIndexStore
	FolderIDs []string
}

type blockIndexRecord struct {
	ref state.BlockRef
}

func (c BlockIndexCrawler) Step(ctx context.Context, cursor Cursor) (StepResult, error) {
	if err := ctx.Err(); err != nil {
		return StepResult{}, err
	}
	records, err := c.records()
	if err != nil {
		return StepResult{}, err
	}
	idx := int(cursor.Position)
	if idx >= len(records) {
		return StepResult{Cursor: Cursor{}, Complete: true}, nil
	}
	record := records[idx]
	result := StepResult{
		Cursor:       Cursor{Position: uint64(idx + 1), FolderID: record.ref.FolderID, Path: record.ref.RelativePath},
		FilesScanned: 1,
		BytesScanned: int64(record.ref.Block.Size),
		Complete:     idx+1 >= len(records),
	}
	manifest, ok, err := c.Store.LoadManifest(record.ref.FolderID, record.ref.RelativePath)
	if err != nil {
		return StepResult{}, err
	}
	if !ok || !manifestContainsBlock(manifest, record.ref.Block) {
		result.Reported = 1
	}
	return result, nil
}

func (c BlockIndexCrawler) records() ([]blockIndexRecord, error) {
	folderIDs := append([]string(nil), c.FolderIDs...)
	sort.Strings(folderIDs)
	records := make([]blockIndexRecord, 0)
	for _, folderID := range folderIDs {
		refs, err := c.Store.ListBlockRefs(folderID)
		if err != nil {
			return nil, err
		}
		for _, ref := range refs {
			records = append(records, blockIndexRecord{ref: ref})
		}
	}
	sort.Slice(records, func(i, j int) bool {
		left := records[i].ref
		right := records[j].ref
		if left.FolderID != right.FolderID {
			return left.FolderID < right.FolderID
		}
		if left.RelativePath != right.RelativePath {
			return left.RelativePath < right.RelativePath
		}
		if left.Block.Index != right.Block.Index {
			return left.Block.Index < right.Block.Index
		}
		if left.Block.Offset != right.Block.Offset {
			return left.Block.Offset < right.Block.Offset
		}
		if left.Block.Size != right.Block.Size {
			return left.Block.Size < right.Block.Size
		}
		return bytes.Compare(left.Block.Hash, right.Block.Hash) < 0
	})
	return records, nil
}

func manifestContainsBlock(manifest block.Manifest, target block.Block) bool {
	for _, candidate := range manifest.Blocks {
		if candidate.Index == target.Index && candidate.Offset == target.Offset && candidate.Size == target.Size && bytes.Equal(candidate.Hash, target.Hash) {
			return true
		}
	}
	return false
}

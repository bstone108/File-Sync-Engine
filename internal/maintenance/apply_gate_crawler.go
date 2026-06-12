package maintenance

import (
	"context"
	"sort"

	"filesyncengine/internal/state"
)

type ApplyGateStore interface {
	FolderSummary(folderID string) (state.FolderSummary, error)
	PendingWrites(folderID string) ([]state.PendingWrite, error)
	SkippedDeletes(folderID string) ([]state.SkippedDelete, error)
	RemovePendingWrite(folderID string, path string) error
	RemoveSkippedDelete(folderID string, path string) error
}

type ApplyGateCrawler struct {
	Store     ApplyGateStore
	FolderIDs []string
}

type applyGateRecord struct {
	folderID string
	path     string
	write    *state.PendingWrite
	delete   *state.SkippedDelete
}

func (c ApplyGateCrawler) Step(ctx context.Context, cursor Cursor) (StepResult, error) {
	if err := ctx.Err(); err != nil {
		return StepResult{}, err
	}
	records, requiredBySkippedDelete, pendingWrites, summaries, err := c.records()
	if err != nil {
		return StepResult{}, err
	}
	idx := int(cursor.Position)
	if idx >= len(records) {
		return StepResult{Cursor: Cursor{}, Complete: true}, nil
	}
	record := records[idx]
	result := StepResult{
		Cursor:       Cursor{Position: uint64(idx + 1), FolderID: record.folderID, Path: record.path},
		FilesScanned: 1,
		Complete:     idx+1 >= len(records),
	}
	if record.write != nil && record.write.Committed && !requiredBySkippedDelete[record.folderID][record.path] {
		if err := c.Store.RemovePendingWrite(record.folderID, record.path); err != nil {
			return StepResult{}, err
		}
		result.Pruned = 1
	}
	if record.delete != nil && missingRequiredWrite(record.delete.RequiredWrites, pendingWrites[record.folderID]) {
		if err := c.Store.RemoveSkippedDelete(record.folderID, record.path); err != nil {
			return StepResult{}, err
		}
		result.Pruned = 1
	} else if record.delete != nil && metadataPrerequisitesUnsatisfiable(*record.delete, summaries[record.folderID]) {
		result.Reported = 1
	}
	return result, nil
}

func (c ApplyGateCrawler) records() ([]applyGateRecord, map[string]map[string]bool, map[string]map[string]bool, map[string]state.FolderSummary, error) {
	folderIDs := append([]string(nil), c.FolderIDs...)
	sort.Strings(folderIDs)
	requiredBySkippedDelete := map[string]map[string]bool{}
	pendingWrites := map[string]map[string]bool{}
	summaries := map[string]state.FolderSummary{}
	records := make([]applyGateRecord, 0)
	for _, folderID := range folderIDs {
		summary, err := c.Store.FolderSummary(folderID)
		if err != nil {
			return nil, nil, nil, nil, err
		}
		summaries[folderID] = summary
		writes, err := c.Store.PendingWrites(folderID)
		if err != nil {
			return nil, nil, nil, nil, err
		}
		sort.Slice(writes, func(i, j int) bool { return writes[i].Path < writes[j].Path })
		if pendingWrites[folderID] == nil {
			pendingWrites[folderID] = map[string]bool{}
		}
		for _, write := range writes {
			write := write
			pendingWrites[folderID][write.Path] = true
			records = append(records, applyGateRecord{folderID: folderID, path: write.Path, write: &write})
		}

		deletes, err := c.Store.SkippedDeletes(folderID)
		if err != nil {
			return nil, nil, nil, nil, err
		}
		sort.Slice(deletes, func(i, j int) bool { return deletes[i].Path < deletes[j].Path })
		if requiredBySkippedDelete[folderID] == nil {
			requiredBySkippedDelete[folderID] = map[string]bool{}
		}
		for _, skipped := range deletes {
			skipped := skipped
			for _, path := range skipped.RequiredWrites {
				requiredBySkippedDelete[folderID][path] = true
			}
			records = append(records, applyGateRecord{folderID: folderID, path: skipped.Path, delete: &skipped})
		}
	}
	sort.Slice(records, func(i, j int) bool {
		if records[i].folderID != records[j].folderID {
			return records[i].folderID < records[j].folderID
		}
		if records[i].path != records[j].path {
			return records[i].path < records[j].path
		}
		return records[i].write != nil && records[j].delete != nil
	})
	return records, requiredBySkippedDelete, pendingWrites, summaries, nil
}

func missingRequiredWrite(requiredWrites []string, pendingWrites map[string]bool) bool {
	for _, path := range requiredWrites {
		if !pendingWrites[path] {
			return true
		}
	}
	return false
}

func metadataPrerequisitesUnsatisfiable(delete state.SkippedDelete, current state.FolderSummary) bool {
	if delete.RequiredMetadataCursor == 0 || delete.RequiredMetadataStateHash == "" {
		return false
	}
	return current.Cursor >= delete.RequiredMetadataCursor && current.StateHash != delete.RequiredMetadataStateHash
}

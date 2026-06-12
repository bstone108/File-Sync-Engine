package maintenance

import (
	"context"
	"path/filepath"
	"testing"

	"filesyncengine/internal/block"
	"filesyncengine/internal/state"
)

func TestApplyGateCrawlerPrunesCommittedPendingWritesOnlyWhenNoSkippedDeleteRequiresThem(t *testing.T) {
	store := state.NewJSONStore(filepath.Join(t.TempDir(), "state.json"))
	manifest := block.Manifest{Path: "unused.txt", Size: 4}
	for _, write := range []state.PendingWrite{
		{FolderID: "docs", Path: "unused.txt", Manifest: manifest, Committed: true},
		{FolderID: "docs", Path: "needed.txt", Manifest: block.Manifest{Path: "needed.txt", Size: 6}, Committed: true},
		{FolderID: "docs", Path: "active.txt", Manifest: block.Manifest{Path: "active.txt", Size: 6}},
	} {
		if err := store.SavePendingWrite(write); err != nil {
			t.Fatalf("SavePendingWrite(%s): %v", write.Path, err)
		}
	}
	if err := store.SaveSkippedDelete(state.SkippedDelete{FolderID: "docs", Path: "old.txt", RequiredWrites: []string{"needed.txt"}, Reason: "waiting for committed write gate"}); err != nil {
		t.Fatalf("SaveSkippedDelete: %v", err)
	}

	result, err := RunOnce(context.Background(), RunOptions{
		Crawler:    ApplyGateCrawler{Store: store, FolderIDs: []string{"docs"}},
		Checkpoint: &memoryCheckpoint{},
		MaxFiles:   10,
	})
	if err != nil {
		t.Fatalf("RunOnce: %v", err)
	}
	if result.Pruned != 1 || !result.Complete {
		t.Fatalf("result=%+v, want one stale committed pending write pruned and complete", result)
	}
	if _, ok, err := store.PendingWrite("docs", "unused.txt"); err != nil || ok {
		t.Fatalf("unused committed write still present ok=%v err=%v", ok, err)
	}
	for _, path := range []string{"needed.txt", "active.txt"} {
		if _, ok, err := store.PendingWrite("docs", path); err != nil || !ok {
			t.Fatalf("pending write %s should remain ok=%v err=%v", path, ok, err)
		}
	}
}

func TestApplyGateCrawlerPrunesSkippedDeletesWithMissingRequiredWrites(t *testing.T) {
	store := state.NewJSONStore(filepath.Join(t.TempDir(), "state.json"))
	for _, delete := range []state.SkippedDelete{
		{FolderID: "docs", Path: "orphaned.txt", RequiredWrites: []string{"missing-write.txt"}, Reason: "required write disappeared"},
		{FolderID: "docs", Path: "blocked.txt", RequiredWrites: []string{"pending.txt"}, Reason: "required write still pending"},
		{FolderID: "docs", Path: "metadata-only.txt", RequiredMetadataCursor: 2, RequiredMetadataStateHash: "abc", Reason: "metadata catchup still pending"},
	} {
		if err := store.SaveSkippedDelete(delete); err != nil {
			t.Fatalf("SaveSkippedDelete(%s): %v", delete.Path, err)
		}
	}
	if err := store.SavePendingWrite(state.PendingWrite{FolderID: "docs", Path: "pending.txt", Manifest: block.Manifest{Path: "pending.txt", Size: 7}}); err != nil {
		t.Fatalf("SavePendingWrite: %v", err)
	}

	result, err := RunOnce(context.Background(), RunOptions{
		Crawler:    ApplyGateCrawler{Store: store, FolderIDs: []string{"docs"}},
		Checkpoint: &memoryCheckpoint{},
		MaxFiles:   10,
	})
	if err != nil {
		t.Fatalf("RunOnce: %v", err)
	}
	if result.Pruned != 1 || !result.Complete {
		t.Fatalf("result=%+v, want one orphaned skipped delete pruned and complete", result)
	}
	deletes, err := store.SkippedDeletes("docs")
	if err != nil {
		t.Fatalf("SkippedDeletes: %v", err)
	}
	paths := make(map[string]bool, len(deletes))
	for _, delete := range deletes {
		paths[delete.Path] = true
	}
	if paths["orphaned.txt"] {
		t.Fatalf("orphaned skipped delete should have been pruned: %+v", deletes)
	}
	for _, path := range []string{"blocked.txt", "metadata-only.txt"} {
		if !paths[path] {
			t.Fatalf("skipped delete %s should remain: %+v", path, deletes)
		}
	}
}

func TestApplyGateCrawlerReportsSkippedDeletesWithUnsatisfiableMetadataPrerequisites(t *testing.T) {
	store := state.NewJSONStore(filepath.Join(t.TempDir(), "state.json"))
	if err := store.SaveManifest("docs", "current.txt", block.Manifest{Path: "current.txt", Size: 4}); err != nil {
		t.Fatalf("SaveManifest: %v", err)
	}
	summary, err := store.FolderSummary("docs")
	if err != nil {
		t.Fatalf("FolderSummary: %v", err)
	}
	if err := store.SaveSkippedDelete(state.SkippedDelete{FolderID: "docs", Path: "stale.txt", RequiredMetadataCursor: summary.Cursor, RequiredMetadataStateHash: "old-state-hash", Reason: "metadata prerequisite cannot match current state"}); err != nil {
		t.Fatalf("SaveSkippedDelete stale: %v", err)
	}
	if err := store.SaveSkippedDelete(state.SkippedDelete{FolderID: "docs", Path: "waiting.txt", RequiredMetadataCursor: summary.Cursor + 1, RequiredMetadataStateHash: "future-state-hash", Reason: "future metadata may still arrive"}); err != nil {
		t.Fatalf("SaveSkippedDelete waiting: %v", err)
	}

	result, err := RunOnce(context.Background(), RunOptions{
		Crawler:    ApplyGateCrawler{Store: store, FolderIDs: []string{"docs"}},
		Checkpoint: &memoryCheckpoint{},
		MaxFiles:   10,
	})
	if err != nil {
		t.Fatalf("RunOnce: %v", err)
	}
	if result.Reported != 1 || result.Pruned != 0 || !result.Complete {
		t.Fatalf("result=%+v, want one unsatisfiable metadata gate reported without pruning", result)
	}
	for _, path := range []string{"stale.txt", "waiting.txt"} {
		if _, ok := skippedDeleteByPath(t, store, "docs", path); !ok {
			t.Fatalf("skipped delete %s should remain for conservative operator review", path)
		}
	}
}

func skippedDeleteByPath(t *testing.T, store state.JSONStore, folderID string, path string) (state.SkippedDelete, bool) {
	t.Helper()
	deletes, err := store.SkippedDeletes(folderID)
	if err != nil {
		t.Fatalf("SkippedDeletes: %v", err)
	}
	for _, delete := range deletes {
		if delete.Path == path {
			return delete, true
		}
	}
	return state.SkippedDelete{}, false
}

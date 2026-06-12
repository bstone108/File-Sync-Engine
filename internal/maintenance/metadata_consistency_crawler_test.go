package maintenance

import (
	"context"
	"testing"

	"filesyncengine/internal/block"
	"filesyncengine/internal/state"
)

type fakeMetadataConsistencyStore struct {
	manifests  map[string]map[string]block.Manifest
	revisions  map[string]map[string]uint64
	tombstones map[string]map[string]uint64
	summaries  map[string]state.FolderSummary
}

func (f fakeMetadataConsistencyStore) ListManifests(folderID string) (map[string]block.Manifest, error) {
	out := map[string]block.Manifest{}
	for path, manifest := range f.manifests[folderID] {
		out[path] = manifest
	}
	return out, nil
}

func (f fakeMetadataConsistencyStore) ListManifestRevisions(folderID string) (map[string]uint64, error) {
	out := map[string]uint64{}
	for path, revision := range f.revisions[folderID] {
		out[path] = revision
	}
	return out, nil
}

func (f fakeMetadataConsistencyStore) ListTombstones(folderID string) (map[string]uint64, error) {
	out := map[string]uint64{}
	for path, revision := range f.tombstones[folderID] {
		out[path] = revision
	}
	return out, nil
}

func (f fakeMetadataConsistencyStore) FolderSummary(folderID string) (state.FolderSummary, error) {
	return f.summaries[folderID], nil
}

func TestMetadataConsistencyCrawlerReportsRevisionTombstoneCursorInconsistenciesWithoutPruning(t *testing.T) {
	store := fakeMetadataConsistencyStore{
		manifests: map[string]map[string]block.Manifest{"docs": {
			"missing-revision.txt": {Path: "missing-revision.txt", Size: 1},
			"live-and-deleted.txt": {Path: "live-and-deleted.txt", Size: 1},
		}},
		revisions: map[string]map[string]uint64{"docs": {
			"orphan-revision.txt":  6,
			"live-and-deleted.txt": 7,
		}},
		tombstones: map[string]map[string]uint64{"docs": {
			"live-and-deleted.txt": 8,
			"future-delete.txt":    10,
		}},
		summaries: map[string]state.FolderSummary{"docs": {FolderID: "docs", Cursor: 7}},
	}
	crawler := MetadataConsistencyCrawler{Store: store, FolderIDs: []string{"docs"}}

	result, err := RunOnce(context.Background(), RunOptions{
		Crawler:    crawler,
		Checkpoint: &memoryCheckpoint{},
		MaxFiles:   10,
	})
	if err != nil {
		t.Fatalf("RunOnce: %v", err)
	}
	if result.FilesScanned != 5 || result.Reported != 5 || result.Pruned != 0 || !result.Complete {
		t.Fatalf("result=%+v, want five metadata inconsistencies reported without pruning", result)
	}
}

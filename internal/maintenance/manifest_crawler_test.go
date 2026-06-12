package maintenance

import (
	"context"
	"testing"

	"filesyncengine/internal/block"
)

type fakeManifestStore struct {
	folders   map[string]map[string]block.Manifest
	revisions map[string]map[string]uint64
}

func (f fakeManifestStore) ListManifests(folderID string) (map[string]block.Manifest, error) {
	manifests := map[string]block.Manifest{}
	for path, manifest := range f.folders[folderID] {
		manifests[path] = manifest
	}
	return manifests, nil
}

func (f fakeManifestStore) ListManifestRevisions(folderID string) (map[string]uint64, error) {
	revisions := map[string]uint64{}
	for path, revision := range f.revisions[folderID] {
		revisions[path] = revision
	}
	return revisions, nil
}

func TestManifestCrawlerWalksConfiguredFoldersInStableOrder(t *testing.T) {
	crawler := ManifestCrawler{
		Store: fakeManifestStore{folders: map[string]map[string]block.Manifest{
			"b": {"two.txt": {Size: 2}},
			"a": {"one.txt": {Size: 1}},
		}},
		FolderIDs: []string{"b", "a"},
	}

	first, err := crawler.Step(context.Background(), Cursor{})
	if err != nil {
		t.Fatalf("first Step: %v", err)
	}
	if first.FilesScanned != 1 || first.BytesScanned != 1 || first.Cursor.Position != 1 || first.Cursor.FolderID != "a" || first.Cursor.Path != "one.txt" || first.Complete {
		t.Fatalf("first=%+v, want first sorted manifest a/one.txt", first)
	}

	second, err := crawler.Step(context.Background(), first.Cursor)
	if err != nil {
		t.Fatalf("second Step: %v", err)
	}
	if second.FilesScanned != 1 || second.BytesScanned != 2 || !second.Complete {
		t.Fatalf("second=%+v, want second sorted manifest b/two.txt and complete", second)
	}
}

func TestManifestCrawlerResumesAfterPersistedFileVersionMarkerWhenEarlierRecordsAppear(t *testing.T) {
	store := fakeManifestStore{folders: map[string]map[string]block.Manifest{
		"docs": {
			"b.txt": {Size: 2},
			"c.txt": {Size: 3},
		},
	}}
	store.revisions = map[string]map[string]uint64{"docs": {"b.txt": 7, "c.txt": 8}}
	crawler := ManifestCrawler{Store: store, FolderIDs: []string{"docs"}}

	first, err := crawler.Step(context.Background(), Cursor{})
	if err != nil {
		t.Fatalf("first Step: %v", err)
	}
	if first.Cursor.FolderID != "docs" || first.Cursor.Path != "b.txt" || first.Cursor.Revision != 7 {
		t.Fatalf("first cursor=%+v, want persisted marker for docs/b.txt revision 7", first.Cursor)
	}

	store.folders["docs"]["a.txt"] = block.Manifest{Size: 1}
	store.revisions["docs"]["a.txt"] = 9
	crawler.Store = store

	second, err := crawler.Step(context.Background(), first.Cursor)
	if err != nil {
		t.Fatalf("second Step: %v", err)
	}
	if second.BytesScanned != 3 || second.Cursor.Path != "c.txt" || second.Cursor.Revision != 8 {
		t.Fatalf("second=%+v, want resume after persisted b.txt marker and scan c.txt", second)
	}
}
